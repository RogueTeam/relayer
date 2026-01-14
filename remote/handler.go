package remote

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RogueTeam/relayer/internal/ioutils"
	"github.com/RogueTeam/relayer/internal/p2p/peers"
	"github.com/RogueTeam/relayer/internal/ringqueue"
	"github.com/RogueTeam/relayer/internal/set"
	"github.com/RogueTeam/relayer/internal/utils"
	"github.com/RogueTeam/relayer/proxy"
	"github.com/ipfs/go-cid"
	"github.com/klauspost/compress/zstd"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

type Remote struct {
	// Timeout when GC active protocol connections
	StreamTimeout time.Duration
	// Timeout for the refresh process
	RefreshTimeout time.Duration
	// Interval between refresh processes
	RefreshInterval time.Duration
	// Protocol of the remote service
	// This should be the same as the one advertised by remote
	Protocol protocol.ID
	// Name used for handling the service with the configured proxy
	Host string
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

type Handler struct {
	logger *slog.Logger
	host   host.Host
	dht    *dht.IpfsDHT
	proxy  *proxy.Proxy

	running      atomic.Bool
	l            manet.Listener
	peersQueue   *ringqueue.Queue[peer.ID]
	remote       *Remote
	allowedPeers set.Set[peer.ID]
	cid          cid.Cid
}

type Config struct {
	Logger *slog.Logger
	Host   host.Host
	DHT    *dht.IpfsDHT
	Proxy  *proxy.Proxy
}

// Setups a remote connection handler
// the passed context is used only during configuration
func New(ctx context.Context, cfg *Config, remote *Remote) (h *Handler, err error) {
	h = &Handler{
		logger: cfg.Logger.
			WithGroup("Remote").
			With(
				"name", remote.Protocol,
				"id", cfg.Host.ID(),
				"interval", remote.RefreshInterval,
			),
		host:         cfg.Host,
		dht:          cfg.DHT,
		proxy:        cfg.Proxy,
		peersQueue:   ringqueue.New[peer.ID](),
		remote:       remote,
		allowedPeers: set.New(remote.AllowedPeers...),
		cid:          peers.IdentityCidFromData(remote.Protocol),
	}
	err = h.bind(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to bind handler: %w", err)
	}
	return h, nil
}

func (h *Handler) bindWithRemoteAddress(ctx context.Context, logger *slog.Logger, addr multiaddr.Multiaddr) (id peer.ID, err error) {
	logger = logger.With("address", addr)
	logger.Info("Connecting to peer")

	id, err = peer.IDFromP2PAddr(addr)
	if err != nil {
		return "", fmt.Errorf("failed to get peer id from remote address: %v: %w", addr, err)
	}

	err = h.host.Connect(ctx, peer.AddrInfo{ID: id, Addrs: []multiaddr.Multiaddr{addr}})
	if err != nil {
		logger.Error("failed to conntect", "error-msg", err.Error())
		return "", fmt.Errorf("failed to connect to remote address: %w", err)
	}
	return id, nil
}

func (h *Handler) bindWithRemoteAddresses(ctx context.Context) (err error) {
	logger := h.logger.WithGroup("Addresses").With("addresses", h.remote.Addresses)
	logger.Info("using remote peers")

	peers := make([]peer.ID, 0, len(h.remote.Addresses))
	for _, addr := range h.remote.Addresses {
		id, err := h.bindWithRemoteAddress(ctx, logger, addr)
		if err != nil {
			return fmt.Errorf("failed to add remote peer by addr: %s: %w", addr.String(), err)
		}
		peers = append(peers, id)
	}

	err = h.peersQueue.Set(peers)
	if err != nil {
		return fmt.Errorf("failed to set ring queue from remote addresses: %w", err)
	}
	return nil
}

func (h *Handler) doBindWithDhtWork(logger *slog.Logger) (err error) {
	var refreshTimeout = h.remote.RefreshTimeout
	if refreshTimeout == 0 {
		refreshTimeout = utils.DefaultTimeout
	}

	logger.Debug("Pulling providers")
	ctx, cancel := utils.NewContextWithTimeout(refreshTimeout)
	defer cancel()
	providers, err := h.dht.FindProviders(ctx, h.cid)
	if err != nil {
		logger.Error("failed to find providers", "error-msg", err.Error())
		return
	}

	streamTimeout := h.remote.StreamTimeout
	if streamTimeout == 0 {
		streamTimeout = utils.DefaultTimeout
	}

	timeAgo := time.Now().Add(-streamTimeout)
	var wg sync.WaitGroup
	for _, addrInfo := range providers {
		if strings.Contains(addrInfo.String(), "quic-v1") {
			continue // Specifically for quic we can do a quic level ping
		}
		wg.Go(func() {
			conns := h.host.Network().ConnsToPeer(addrInfo.ID)
			for _, conn := range conns {
				logger := logger.With("connection", conn.RemoteMultiaddr())
				for _, stream := range conn.GetStreams() {
					if stream.Protocol() != h.remote.Protocol {
						continue
					}
					stat := stream.Stat()
					if stat.Opened.Before(timeAgo) || stat.Opened.Equal(timeAgo) {
						logger.Debug("Stream closed")
						stream.ResetWithError(network.StreamGarbageCollected)
						stream.Close()
					}
				}

				if conn.Stat().NumStreams == 0 {
					logger.Debug("Connection closed")
					conn.CloseWithError(network.ConnGarbageCollected)
					conn.Close()
				}
			}

			logger := logger.With("addr-info", addrInfo.String())
			logger.Debug("Found service provider")
			logger.Debug("Reconnecting")
			err = h.host.Connect(ctx, addrInfo)
			if err != nil {
				logger.Error("failed to connect to provider", "error-msg", err.Error())
				return
			}
		})
	}

	wg.Wait()

	peersIds := peer.AddrInfosToIDs(providers)

	if len(h.allowedPeers) > 0 {
		peersIds = slices.DeleteFunc(peersIds, func(e peer.ID) bool {
			return h.allowedPeers.Has(e)
		})
	}

	if len(peersIds) == 0 {
		logger.Info("no peers providers received")
		return
	}

	// At this point the peers ids list is not empty
	err = h.peersQueue.Set(peersIds)
	if err != nil {
		logger.Error("failed to prepare peers queue", "error-msg", err)
		return
	}
	return nil
}

func (h *Handler) bindWithDHT(ctx context.Context) (err error) {
	logger := h.logger.WithGroup("DHT")
	logger.Info("Using DHT truth of source")

	// Spawn worker that retrieves providers of the requested service.
	go func() {
		logger.Info("Pulling providers worker")
		defer logger.Info("Stopped pulling providers worker")

		var interval = h.remote.RefreshInterval
		if interval == 0 {
			interval = time.Minute
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for h.running.Load() {
			err = h.doBindWithDhtWork(logger)
			if err != nil {
				logger.Error("failed to do work", "error-msg", err.Error())
			}
			<-ticker.C
		}
	}()
	return nil
}

func (h *Handler) processConnection(conn manet.Conn) (err error) {
	if h.peersQueue.Empty() {
		return errors.New("no peers available")
	}

	target := h.peersQueue.Next()

	logger := h.logger.With("target", target)
	logger.Info("Connecting")

	s, err := h.host.NewStream(context.TODO(), target, h.remote.Protocol)
	if err != nil {
		return fmt.Errorf("failed to create new stream: %w", err)
	}
	defer s.Close()

	logger.Info("Connected")

	go ioutils.CopyToZstd(s, conn, zstd.SpeedBestCompression)
	_, err = ioutils.CopyFromZstd(conn, s)
	if err != nil {
		return fmt.Errorf("failed to copy from stream to local connection: %w", err)
	}
	return nil
}

func (h *Handler) serve() (err error) {
	for {
		conn, err := h.l.Accept()
		if err != nil {
			return fmt.Errorf("failed to accept connection: %w", err)
		}

		go func() {
			defer conn.Close()
			err := h.processConnection(conn)
			if err != nil {
				h.logger.Error("failed to process connection", "error-msg", err.Error())
			}
		}()
	}
}

func (h *Handler) Protocol() (protocol protocol.ID) {
	return h.remote.Protocol
}

func (h *Handler) Close() (err error) {
	h.logger.Info("Closing")
	if !h.running.Swap(false) {
		return nil
	}

	h.l.Close()
	return nil
}

func (h *Handler) bind(ctx context.Context) (err error) {
	h.running.Store(true)

	if len(h.remote.Addresses) > 0 {
		err = h.bindWithRemoteAddresses(ctx)
		if err != nil {
			return fmt.Errorf("failed to bind with remote addresses: %w", err)
		}
	} else if h.dht != nil {
		err = h.bindWithDHT(ctx)
		if err != nil {
			return fmt.Errorf("failed to bind with dht: %w", err)
		}
	} else {
		return errors.New("can't resolve remote without a DHT")
	}

	if h.remote.ListenAddress != nil {
		h.logger.Info("Binding")
		h.l, err = manet.Listen(h.remote.ListenAddress)
		if err != nil {
			return fmt.Errorf("failed to listen: %w", err)
		}
		defer func() {
			if err != nil {
				h.l.Close()
			}
		}()
		go func() {
			err := h.serve()
			if err != nil {
				h.logger.Error("error during serve", "error-msg", err.Error())
			}
		}()
	}

	if h.proxy != nil && h.remote.Host != "" {
		h.logger.Info("Registering proxy host", "host", h.host)
		h.proxy.Register(h.remote.Host, func(ctx context.Context, network, addr string) (conn net.Conn, err error) {
			if h.peersQueue.Empty() {
				return nil, errors.New("no peers available")
			}

			target := h.peersQueue.Next()

			logger := h.logger.With("target", target)
			logger.Info("Proxying")

			s, err := h.host.NewStream(ctx, target, h.remote.Protocol)
			if err != nil {
				return nil, fmt.Errorf("failed to create new stream: %w", err)
			}
			return &ioutils.ZstdConn{Conn: &ioutils.TCPStreamConn{Stream: s}}, nil
		})
	}

	return nil
}
