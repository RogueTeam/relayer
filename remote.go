package relayer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"sync"
	"time"

	"github.com/RogueTeam/relayer/internal/p2p/peers"
	"github.com/RogueTeam/relayer/internal/ringqueue"
	"github.com/RogueTeam/relayer/internal/set"
	"github.com/RogueTeam/relayer/internal/utils"
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
			r.logger.Printf("[REMOTE] [%s] Pulling providers worker", remote.Name)
			defer r.logger.Printf("[REMOTE] [%s] Stopped pulling providers worker", remote.Name)
			allowedPeers := set.New(remote.AllowedPeers...)

			ticker := time.NewTicker(time.Minute)
			defer ticker.Stop()
			for r.running {
				func() {
					if peersQueue == nil {
						peersQueueMutex.Lock()
						defer peersQueueMutex.Unlock()
					}

					r.logger.Printf("[REMOTE] [%s] Pulling providers", remote.Name)
					ctx, cancel := utils.NewContext()
					defer cancel()
					providers, err := r.dht.FindProviders(ctx, cid)
					if err != nil {
						r.logger.Printf("[REMOTE] [%s] failed to find providers: %v", remote.Name, err)
						return
					}

					for _, addrInfo := range providers {
						r.logger.Printf("[REMOTE] [%s] Found service provider: %v", remote.Name, addrInfo)
						go func() {
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

					if len(allowedPeers) > 0 {
						peersIds = slices.DeleteFunc(peersIds, func(e peer.ID) bool {
							return allowedPeers.Has(e)
						})
					}

					if len(peersIds) == 0 {
						r.logger.Printf("[REMOTE] [%s] no peers providers received", remote.Name)
						return
					}

					if peersQueue != nil {
						peersQueueMutex.Lock()
						defer peersQueueMutex.Unlock()
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
					peersQueueMutex.Unlock()
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
