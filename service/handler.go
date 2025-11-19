package service

import (
	"fmt"
	"io"
	"log/slog"
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

type Service struct {
	Logger *slog.Logger
	Host   host.Host
	DHT    *dht.IpfsDHT
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

func Register(svc *Service) (h *Handler, err error) {
	h = &Handler{
		logger: svc.Logger.With(
			"kind", "service",
			"name", svc.Name,
			"id", svc.Host.ID(),
			"interval", svc.AdvertiseInterval,
		),
		host:              svc.Host,
		dht:               svc.DHT,
		name:              svc.Name,
		addresses:         svc.Addresses,
		allowedPeers:      svc.AllowedPeers,
		advertise:         svc.Advertise,
		advertiseInterval: svc.AdvertiseInterval,
	}
	err = h.registerService()
	if err != nil {
		return nil, fmt.Errorf("failed to start handler: %w", err)
	}
	return h, nil
}

type Handler struct {
	logger *slog.Logger
	host   host.Host
	dht    *dht.IpfsDHT

	name              string
	addresses         []multiaddr.Multiaddr
	allowedPeers      []peer.ID
	advertise         bool
	advertiseInterval time.Duration
}

func (h *Handler) advertiseAttempt() (err error) {
	key := peers.IdentityCidFromData(h.name)
	ctx, cancel := utils.NewContext()
	defer cancel()
	err = h.dht.Provide(ctx, key, true)
	if err != nil {
		return fmt.Errorf("failed to advertise over DHT: %w", err)
	}
	return nil
}

func (h *Handler) registerService() (err error) {
	h.logger.Info("Registering service")

	pid := protocol.ID(h.name)

	q := ringqueue.New[multiaddr.Multiaddr]()
	err = q.Set(h.addresses)
	if err != nil {
		return fmt.Errorf("failed to create ring queue from addresses: %w", err)
	}

	allowedPeers := set.New(h.allowedPeers...)

	h.host.SetStreamHandler(pid, func(s network.Stream) {
		logger := h.logger.With("peer-id", s.Conn().RemotePeer(), "peer-addr", s.Conn().RemoteMultiaddr())
		logger.Info("Received connection")
		defer s.Close()

		if len(allowedPeers) > 0 && !allowedPeers.Has(s.Conn().RemotePeer()) {
			logger.Error("Peer not allowed")
			return
		}

		remote := q.Next()
		logger = logger.With("remote-addr", remote)
		logger.Debug("Connecting")

		conn, err := manet.Dial(remote)
		if err != nil {
			logger.Error("failed to dial to remote", "error-msg", err.Error())
			return
		}
		defer conn.Close()

		logger.Debug("Serving")
		go io.Copy(s, conn)
		io.Copy(conn, s)
	})

	if h.advertise {
		h.logger.Info("Advertising enabled")
		if h.dht == nil {
			h.logger.Warn("Skiping advertise: No DHT provided")
		} else {
			go func() {
				var interval = h.advertiseInterval
				if interval == 0 {
					interval = time.Minute
				}
				ticker := time.NewTicker(interval)
				defer ticker.Stop()
				for {
					err := h.advertiseAttempt()
					if err != nil {
						h.logger.Error("Failed to advertise", "error-msg", err)
					}
					h.logger.Debug("service advertised")
					<-ticker.C
				}
			}()
		}
	}
	return nil
}

func (h *Handler) Name() (name string) {
	return h.name
}

// Unregisters the handler for the host
func (h *Handler) Close() (err error) {
	h.logger.Info("Unregistering service from HOST")
	h.host.RemoveStreamHandler(protocol.ID(h.name))
	return nil
}
