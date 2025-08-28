package relayer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"slices"
	"sync"
	"time"

	"github.com/RogueTeam/relayer/internal/p2p/peers"
	"github.com/RogueTeam/relayer/internal/ringqueue"
	"github.com/RogueTeam/relayer/internal/set"
	"github.com/RogueTeam/relayer/internal/utils"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

type Remote struct {
	// Name of the remote service
	// This should be the same as the one advertised by remote
	Name string
	// Listen address for incoming requests
	ListenAddress multiaddr.Multiaddr
	// Optional. Remote peers address from which consume the remote service
	// If a DHT is passed the service could be advertised locally and the P2P
	// will resolve the address automatically
	Addresses []multiaddr.Multiaddr
	// Optional. Allowed peers to request the service from. This will prevent consuming
	// the service from untrusted peers
	AllowedPeers []peer.ID
}

type Service struct {
	// Name of the service to advertise
	Name string
	// Addresses to advertise the service. If more than one address is used it will work as a load balancer.
	Addresses []multiaddr.Multiaddr
	// Optional. Allowed peers to permit access to consume this service
	AllowedPeers []peer.ID
	// Optional. If a DHT is available the service will be advertised within it.
	Advertise bool
}

type Relayer struct {
	logger   *log.Logger
	host     host.Host
	dht      *dht.IpfsDHT
	remote   []Remote
	services []Service

	running         bool
	remoteListeners map[string]manet.Listener
}

func (r *Relayer) bindRemote(remote *Remote) (err error) {
	r.logger.Printf("[CONFIG] [REMOTE] [%s] Binding remote", remote.Name)
	r.logger.Printf("[CONFIG] [REMOTE] [%s] Listening at: %v", remote.Name, remote.ListenAddress)
	l, err := manet.Listen(remote.ListenAddress)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}
	r.remoteListeners[remote.Name] = l
	defer func() {
		if err != nil {
			delete(r.remoteListeners, remote.Name)
			l.Close()
		}
	}()

	var peersQueueMutex = new(sync.Mutex)
	var peersQueue *ringqueue.Queue[peer.ID]
	if len(remote.Addresses) > 0 {
		r.logger.Printf("[CONFIG] [REMOTE] [%s] Using remote peers", remote.Name)
		peers := make([]peer.ID, 0, len(remote.Addresses))
		for _, addr := range remote.Addresses {
			r.logger.Printf("[CONFIG] [REMOTE] [%s] Connecting to peer: %v", remote.Name, addr)

			id, err := peer.IDFromP2PAddr(addr)
			if err != nil {
				return fmt.Errorf("failed to get peer id from remote address: %v: %w", addr, err)
			}
			peers = append(peers, id)

			err = func() (err error) {
				ctx, cancel := utils.NewContext()
				defer cancel()
				return r.host.Connect(ctx, peer.AddrInfo{
					ID:    id,
					Addrs: []multiaddr.Multiaddr{addr},
				})
			}()
			if err != nil {
				return fmt.Errorf("failed to connect to remote address: %w", err)
			}
		}
		peersQueue, err = ringqueue.New(peers)
		if err != nil {
			return fmt.Errorf("failed to create ring queue from remote addresses: %w", err)
		}
	} else if r.dht != nil {
		r.logger.Printf("[CONFIG] [REMOTE] [%s] Using DHT truth of source", remote.Name)

		cid := peers.IdentityCidFromData(remote.Name)

		// Spawn worker that retrieves providers of the requested service.
		go func() {
			allowedPeers := set.New(remote.AllowedPeers...)

			ticker := time.NewTicker(time.Minute)
			defer ticker.Stop()
			for r.running {
				func() {
					peersQueueMutex.Lock()
					defer peersQueueMutex.Unlock()

					r.logger.Printf("[REMOTE] [%s] Pulling providers", remote.Name)
					ctx, cancel := utils.NewContext()
					defer cancel()
					providers, err := r.dht.FindProviders(ctx, cid)
					if err != nil {
						r.logger.Printf("[REMOTE] [%s] failed to find providers: %v", remote.Name, err)
						return
					}

					for _, addrInfo := range providers {
						func() {
							ctx, cancel := utils.NewContext()
							defer cancel()
							err := r.host.Connect(ctx, addrInfo)
							if err != nil {
								r.logger.Printf("[REMOTE] [%s] failed to connect to provider %v: %v", remote.Name, addrInfo, err)
								return
							}
						}()
					}
					peersIds := peer.AddrInfosToIDs(providers)
					if len(peersIds) == 0 {
						r.logger.Printf("[REMOTE] [%s] no peers providers received", remote.Name)
						return
					}

					if len(allowedPeers) > 0 {
						peersIds = slices.DeleteFunc(peersIds, func(e peer.ID) bool {
							return allowedPeers.Has(e)
						})
					}

					peersQueue, err = ringqueue.New(peersIds)
					if err != nil {
						r.logger.Printf("[REMOTE] [%s] failed to prepare peers queue: %v", remote.Name, err)
						return
					}
				}()
				<-ticker.C
			}
		}()
	} else {
		return errors.New("can't resolve remote without a DHT")
	}

	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				r.logger.Printf("[REMOTE] [%s] Failed to accept connection: %v", remote.Name, err)
				break
			}
			go func() {
				defer conn.Close()

				peersQueueMutex.Lock()
				if peersQueue == nil {
					r.logger.Printf("[REMOTE] [%s] Failed to accept connection no peers available", remote.Name)
					return
				}
				target := peersQueue.Next()
				peersQueueMutex.Unlock()

				r.logger.Printf("[REMOTE] [%s] Connecting to: %v", remote.Name, target)

				pid := protocol.ID(remote.Name)
				s, err := r.host.NewStream(context.TODO(), target, pid)
				if err != nil {
					r.logger.Printf("[REMOTE] [%s] Failed to connect to: %v: %v", remote.Name, target, err)
					return
				}
				defer s.Close()

				go io.Copy(s, conn)
				io.Copy(conn, s)
			}()
		}
	}()

	return nil
}

func (r *Relayer) registerService(svc *Service) (err error) {
	r.logger.Printf("[CONFIG] [SERVICE] [%s] Registering service", svc.Name)

	pid := protocol.ID(svc.Name)

	q, err := ringqueue.New(svc.Addresses)
	if err != nil {
		return fmt.Errorf("failed to create ring queue from addresses: %w", err)
	}

	allowedPeers := set.New(svc.AllowedPeers...)

	r.host.SetStreamHandler(pid, func(s network.Stream) {
		r.logger.Printf("[SERVICE] [%s] [%s] Received connection from: %v", svc.Name, s.Conn().RemotePeer(), s.Conn().RemoteMultiaddr())
		defer s.Close()

		if len(allowedPeers) > 0 && !allowedPeers.Has(s.Conn().RemotePeer()) {
			r.logger.Printf("[SERVICE] [%s] [%s] Peer not allowed", svc.Name, s.Conn().RemotePeer())
			return
		}

		remote := q.Next()
		r.logger.Printf("[SERVICE] [%s] [%s] Connecting to: %v", svc.Name, s.Conn().RemotePeer(), remote)

		conn, err := manet.Dial(remote)
		if err != nil {
			r.logger.Printf("[SERVICE] [%s] [%s] Failed to dial to %v: %v", svc.Name, s.Conn().RemotePeer(), remote, err)
			return
		}
		defer conn.Close()

		r.logger.Printf("[SERVICE] [%s] [%s] Serving: %v", svc.Name, s.Conn().RemotePeer(), remote)
		go io.Copy(s, conn)
		io.Copy(conn, s)
	})

	if svc.Advertise {
		r.logger.Printf("[CONFIG] [SERVICE] [%s] Registering service", svc.Name)
		if r.dht == nil {
			r.logger.Printf("[CONFIG] [SERVICE] [%s] Skiping advertise: No DHT provided", svc.Name)
		} else {
			r.logger.Printf("[CONFIG] [SERVICE] [%s] Advertising over DHT", svc.Name)
			key := peers.IdentityCidFromData(svc.Name)

			ctx, cancel := utils.NewContext()
			defer cancel()
			err = r.dht.Provide(ctx, key, true)
			if err != nil {
				return fmt.Errorf("failed to advertise over DHT: %w", err)
			}
		}
	}
	return nil
}

func (r *Relayer) unregisterService(svc *Service) (err error) {
	r.logger.Printf("[CONFIG] [SERVICE] [%s] Unregistering service from HOST", svc.Name)
	r.host.RemoveStreamHandler(protocol.ID(svc.Name))
	return nil
}

// Binds remotes and handle connection for them.
// This function returns immediatly.
// Make sure to call Close
func (r *Relayer) Serve() (err error) {
	r.running = true
	// Bind remote services locally
	for _, remote := range r.remote {
		err = r.bindRemote(&remote)
		if err != nil {
			return fmt.Errorf("failed to bind remote: %s: %w", remote.Name, err)
		}
	}
	// Spawn host handlers for services
	for _, svc := range r.services {
		err = r.registerService(&svc)
		if err != nil {
			return fmt.Errorf("failed to register service: %s: %w", svc.Name, err)
		}
	}
	return nil
}

// Closes the relayer. Relayer should no be used after calling it.
// This function is responsible of unregistering services and stoping
// remote binds. DHT and Host is maintained open
func (r *Relayer) Close() (err error) {
	r.running = false

	for name, remote := range r.remoteListeners {
		r.logger.Printf("[CONFIG] [REMOTE] [%s] Stopping", name)
		remote.Close()
	}

	for _, svc := range r.services {
		err = r.unregisterService(&svc)
		if err != nil {
			r.logger.Printf("[CONFIG] [SERVICE] [%s] Failed to stop: %v", svc.Name, err)
		}
	}
	return nil
}

type Config struct {
	// Logger to use
	Logger *log.Logger
	// Host responsible from managing
	Host host.Host
	// Optional DHT for advertising services
	DHT *dht.IpfsDHT
	// Remote servers to be binded locally
	Remote []Remote
	// Local services to be promoted
	Services []Service
}

func (c *Config) Validate() (err error) {
	if c.Logger == nil {
		return errors.New("no logger provided")
	}
	if c.Host == nil {
		return errors.New("no host passed")
	}
	if len(c.Remote) == 0 && len(c.Services) == 0 {
		return errors.New("no remotes to bind or services to advertise")
	}

	if len(c.Remote) > 0 {
		var visited = make(map[string]struct{}, len(c.Remote))
		for _, remote := range c.Remote {
			_, found := visited[remote.Name]
			if found {
				return fmt.Errorf("remote name collision: %s", remote.Name)
			}
			visited[remote.Name] = struct{}{}
		}
	}
	if len(c.Services) > 0 {
		var visited = make(map[string]struct{}, len(c.Services))
		for _, svc := range c.Services {
			_, found := visited[svc.Name]
			if found {
				return fmt.Errorf("service name collision: %s", svc.Name)
			}
			visited[svc.Name] = struct{}{}
		}
	}
	return nil
}

// Create a new instance of the relayer
func New(cfg *Config) (r *Relayer, err error) {
	err = cfg.Validate()
	if err != nil {
		return nil, fmt.Errorf("failed to validate configuration: %w", err)
	}
	r = &Relayer{
		logger:   cfg.Logger,
		host:     cfg.Host,
		dht:      cfg.DHT,
		remote:   cfg.Remote,
		services: cfg.Services,

		remoteListeners: make(map[string]manet.Listener),
	}
	return r, nil
}
