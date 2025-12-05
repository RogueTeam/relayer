package service

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/RogueTeam/relayer/internal/ioutils"
	"github.com/RogueTeam/relayer/internal/p2p/peers"
	"github.com/RogueTeam/relayer/internal/ringqueue"
	"github.com/RogueTeam/relayer/internal/set"
	"github.com/RogueTeam/relayer/internal/utils"
	"github.com/klauspost/compress/zstd"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

type Config struct {
	Logger *slog.Logger
	Host   host.Host
	DHT    *dht.IpfsDHT
}

type Service struct {
	// Protocol of the service to advertise
	Protocol protocol.ID
	// Addresses to advertise the service. If more than one address is used it will work as a load balancer.
	Addresses []multiaddr.Multiaddr
	// Optional. Allowed peers to permit access to consume this service
	AllowedPeers []peer.ID
	// Optional. If a DHT is available the service will be advertised within it.
	Advertise bool
	// Optional interval to use for advertising
	AdvertiseInterval time.Duration
}

func Register(cfg *Config, svc *Service) (h *Handler, err error) {
	h = &Handler{
		logger: cfg.Logger.With(
			"kind", "service",
			"name", svc.Protocol,
			"id", cfg.Host.ID(),
			"interval", svc.AdvertiseInterval,
		),
		host:              cfg.Host,
		dht:               cfg.DHT,
		protocol:          svc.Protocol,
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

	protocol          protocol.ID
	addresses         []multiaddr.Multiaddr
	allowedPeers      []peer.ID
	advertise         bool
	advertiseInterval time.Duration
}

func (h *Handler) advertiseAttempt() (err error) {
	key := peers.IdentityCidFromData(h.protocol)
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

	q := ringqueue.New[multiaddr.Multiaddr]()
	err = q.Set(h.addresses)
	if err != nil {
		return fmt.Errorf("failed to create ring queue from addresses: %w", err)
	}

	allowedPeers := set.New(h.allowedPeers...)

	h.host.SetStreamHandler(h.protocol, func(s network.Stream) {
		logger := h.logger.With("peer-id", s.Conn().RemotePeer(), "peer-addr", s.Conn().RemoteMultiaddr())
		logger.Info("Received connection")
		defer s.Close()

		if len(allowedPeers) > 0 && !allowedPeers.Has(s.Conn().RemotePeer()) {
			logger.Error("Peer not allowed",
				"remote-peer", s.Conn().RemotePeer(),
				"allowed-peers", allowedPeers.Slice(),
			)
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
		go ioutils.CopyToZstd(s, conn, zstd.SpeedBestCompression)
		_, err = ioutils.CopyFromZstd(conn, s)
		if err != nil {
			logger.Error("failed to copy from stream to service", "error-msg", err.Error())
		}
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

func (h *Handler) Protocol() (protocol protocol.ID) {
	return h.protocol
}

// Unregisters the handler for the host
func (h *Handler) Close() (err error) {
	h.logger.Info("Unregistering service from HOST")
	h.host.RemoveStreamHandler(h.protocol)
	return nil
}
