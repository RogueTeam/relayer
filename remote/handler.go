package remote

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RogueTeam/relayer/internal/ioutils"
	"github.com/RogueTeam/relayer/internal/p2p/peers"
	"github.com/RogueTeam/relayer/internal/ringqueue"
	"github.com/RogueTeam/relayer/internal/set"
	"github.com/RogueTeam/relayer/internal/utils"
	"github.com/ipfs/go-cid"
	"github.com/klauspost/compress/zstd"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

type Remote struct {
	RefreshTimeout  time.Duration
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

type Handler struct {
	logger *slog.Logger
	host   host.Host
	dht    *dht.IpfsDHT

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
}

// Setups a remote connection handler
// the passed context is used only during configuration
func New(ctx context.Context, cfg *Config, remote *Remote) (h *Handler, err error) {
	h = &Handler{
		logger: cfg.Logger.With(
			"kind", "remote",
			"name", remote.Name,
			"id", cfg.Host.ID(),
			"interval", remote.RefreshInterval,
		),
		host:         cfg.Host,
		dht:          cfg.DHT,
		peersQueue:   ringqueue.New[peer.ID](),
		remote:       remote,
		allowedPeers: set.New(remote.AllowedPeers...),
		cid:          peers.IdentityCidFromData(remote.Name),
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
	logger := h.logger.With("mode", "addresses", "addresses", h.remote.Addresses)
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

func (h *Handler) doWork(logger *slog.Logger) (err error) {
	var timeout = h.remote.RefreshTimeout
	if timeout == 0 {
		timeout = utils.DefaultTimeout
	}

	logger.Debug("Pulling providers")
	ctx, cancel := utils.NewContextWithTimeout(timeout)
	defer cancel()
	providers, err := h.dht.FindProviders(ctx, h.cid)
	if err != nil {
		logger.Error("failed to find providers", "error-msg", err.Error())
		return
	}

	var wg sync.WaitGroup
	for _, addrInfo := range providers {
		wg.Go(func() {
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
	logger := h.logger.With("mode", "dht")
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
			err = h.doWork(logger)
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

	pid := protocol.ID(h.remote.Name)
	s, err := h.host.NewStream(context.TODO(), target, pid)
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

func (h *Handler) Name() (name string) {
	return h.remote.Name
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

	go func() {
		err := h.serve()
		if err != nil {
			h.logger.Error("error during serve", "error-msg", err.Error())
		}
	}()

	return nil
}
