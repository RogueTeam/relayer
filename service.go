package relayer

import (
	"fmt"
	"io"
	"time"

	"github.com/RogueTeam/relayer/internal/p2p/peers"
	"github.com/RogueTeam/relayer/internal/ringqueue"
	"github.com/RogueTeam/relayer/internal/set"
	"github.com/RogueTeam/relayer/internal/utils"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

type Service struct {
	// Name of the service to advertise
	Name string
	// Addresses to advertise the service. If more than one address is used it will work as a load balancer.
	Addresses []multiaddr.Multiaddr
	// Optional. Allowed peers to permit access to consume this service
	AllowedPeers []peer.ID
	// Optional. If a DHT is available the service will be advertised within it.
	Advertise bool
	// Optional interval to use for advertising
	AdvertiseInterval time.Duration
}

func (r *Relayer) advertiseAttempt(svc *Service) (err error) {
	r.logger.Printf("[CONFIG] [SERVICE] [%s] Advertising over DHT", svc.Name)
	key := peers.IdentityCidFromData(svc.Name)

	ctx, cancel := utils.NewContext()
	defer cancel()
	err = r.dht.Provide(ctx, key, true)
	if err != nil {
		return fmt.Errorf("failed to advertise over DHT: %w", err)
	}

	r.logger.Printf("[CONFIG] [SERVICE] [%s] Service advertised", svc.Name)
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
			go func() {
				var interval = svc.AdvertiseInterval
				if interval == 0 {
					interval = time.Minute
				}
				ticker := time.NewTicker(interval)
				defer ticker.Stop()
				for {
					err := r.advertiseAttempt(svc)
					if err != nil {
						r.logger.Printf("[CONFIG] [SERVICE] [%s] Failed to advertise: %v", svc.Name, err)
					}
					<-ticker.C
				}
			}()
		}
	}
	return nil
}

func (r *Relayer) unregisterService(svc *Service) (err error) {
	r.logger.Printf("[CONFIG] [SERVICE] [%s] Unregistering service from HOST", svc.Name)
	r.host.RemoveStreamHandler(protocol.ID(svc.Name))
	return nil
}
