package relayer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
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
	RefreshInterval time.Duration
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
	logger := r.logger.With("kind", "remote", "name", remote.Name, "listen-addr", remote.ListenAddress)
	logger.Info("Binding")
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

	var peersQueue = ringqueue.New[peer.ID]()

	if len(remote.Addresses) > 0 {
		logger := logger.With("mode", "addresses", "addresses", remote.Addresses)
		logger.Info("using remote peers")
		peers := make([]peer.ID, 0, len(remote.Addresses))
		for _, addr := range remote.Addresses {
			logger := logger.With("address", addr)
			logger.Info("Connecting to peer")

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
				logger.Error("failed to conntect", "error-msg", err.Error())
				return fmt.Errorf("failed to connect to remote address: %w", err)
			}
		}

		err = peersQueue.Set(peers)
		if err != nil {
			return fmt.Errorf("failed to set ring queue from remote addresses: %w", err)
		}
	} else if r.dht != nil {
		logger := logger.With("mode", "dht")
		logger.Info("Using DHT truth of source")

		cid := peers.IdentityCidFromData(remote.Name)

		// Spawn worker that retrieves providers of the requested service.
		go func() {
			logger.Info("Pulling providers worker")
			defer logger.Info("Stopped pulling providers worker")
			allowedPeers := set.New(remote.AllowedPeers...)

			var interval = remote.RefreshInterval
			if interval == 0 {
				interval = time.Minute
			}
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for r.running {
				func() {
					logger.Debug("Pulling providers")
					ctx, cancel := utils.NewContext()
					defer cancel()
					providers, err := r.dht.FindProviders(ctx, cid)
					if err != nil {
						logger.Error("failed to find providers", "error-msg", err.Error())
						return
					}

					for _, addrInfo := range providers {
						logger := logger.With("addr-info", addrInfo.String())
						logger.Debug("Found service provider")
						go func() {

							logger.Debug("Reconnecting")
							ctx, cancel := utils.NewContext()
							defer cancel()
							err = r.host.Connect(ctx, addrInfo)
							if err != nil {
								logger.Error("failed to connect to provider", "error-msg", err.Error())
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
						logger.Info("no peers providers received")
						return
					}

					// At this point the peers ids list is not empty
					err = peersQueue.Set(peersIds)
					if err != nil {
						logger.Error("failed to prepare peers queue", "error-msg", err)
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
				logger.Error("Failed to accept connection", "error-msg", err)
				break
			}
			go func() {
				defer conn.Close()

				if peersQueue.Empty() {
					logger.Error("Failed to accept connection no peers available")
					return
				}
				target := peersQueue.Next()

				logger := logger.With("target", target)
				logger.Info("Connecting")

				pid := protocol.ID(remote.Name)
				s, err := r.host.NewStream(context.TODO(), target, pid)
				if err != nil {
					logger.Error("failed to connect", "error-msg", err.Error())
					return
				}
				logger.Info("connected")
				defer s.Close()

				go io.Copy(s, conn)
				io.Copy(conn, s)
			}()
		}
	}()

	return nil
}
