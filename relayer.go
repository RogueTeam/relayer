package relayer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/RogueTeam/relayer/proxy"
	"github.com/RogueTeam/relayer/remote"
	"github.com/RogueTeam/relayer/service"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/protocol"
)

type Relayer struct {
	logger *slog.Logger
	host   host.Host
	dht    *dht.IpfsDHT
	proxy  *proxy.Proxy

	remotes  []*remote.Handler
	services []*service.Handler
}

// Binds remotes and handle connection for them.
// This function returns immediatly.
// Make sure to call Close
func (r *Relayer) Serve() (err error) {
	return nil
}

// Closes the relayer. Relayer should no be used after calling it.
// This function is responsible of unregistering services and stoping
// remote binds. DHT and Host is maintained open
func (r *Relayer) Close() (err error) {
	for _, remote := range r.remotes {
		r.logger.Info("Stopping remote", "name", remote.Protocol())
		err = remote.Close()
		if err != nil {
			r.logger.Error("Failed to stop remote", "name", remote.Protocol(), "error-msg", err.Error())
		}
	}

	for _, handler := range r.services {
		r.logger.Info("Stopping service", "name", handler.Protocol())
		err = handler.Close()
		if err != nil {
			r.logger.Error("Failed to stop service", "name", handler.Protocol(), "error-msg", err.Error())
		}
	}
	return nil
}

type Config struct {
	// Logger to use
	Logger *slog.Logger
	// Host responsible from managing
	Host host.Host
	// Optional DHT for advertising services
	DHT *dht.IpfsDHT
	// Remote servers to be binded locally
	Remote []*remote.Remote
	// Local services to be promoted
	Services []*service.Service
	// Optional proxy for service remotes
	Proxy *proxy.Proxy
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
		var visited = make(map[protocol.ID]struct{}, len(c.Remote))
		for _, remote := range c.Remote {
			_, found := visited[remote.Protocol]
			if found {
				return fmt.Errorf("remote name collision: %s", remote.Protocol)
			}
			visited[remote.Protocol] = struct{}{}
		}
	}
	if len(c.Services) > 0 {
		var visited = make(map[protocol.ID]struct{}, len(c.Services))
		for _, svc := range c.Services {
			_, found := visited[svc.Protocol]
			if found {
				return fmt.Errorf("service name collision: %s", svc.Protocol)
			}
			visited[svc.Protocol] = struct{}{}
		}
	}
	return nil
}

// Create a new instance of the relayer
func New(ctx context.Context, cfg *Config) (r *Relayer, err error) {
	err = cfg.Validate()
	if err != nil {
		return nil, fmt.Errorf("failed to validate configuration: %w", err)
	}
	relayer := &Relayer{
		logger: cfg.Logger,
		host:   cfg.Host,
		dht:    cfg.DHT,
		proxy:  cfg.Proxy,
	}

	defer func() {
		if err == nil {
			return
		}

		relayer.Close()
	}()

	defer func() {
		if err == nil {
			return
		}

		for _, rmt := range relayer.remotes {
			rmt.Close()
		}
		for _, svc := range relayer.services {
			svc.Close()
		}
	}()
	var remoteCfg = &remote.Config{
		Logger: relayer.logger,
		Host:   relayer.host,
		DHT:    relayer.dht,
		Proxy:  relayer.proxy,
	}
	for _, entry := range cfg.Remote {
		rmt, err := remote.New(ctx, remoteCfg, entry)
		if err != nil {
			return nil, fmt.Errorf("failed to register reomte: %s: %w", entry.Protocol, err)
		}
		relayer.remotes = append(relayer.remotes, rmt)
	}

	var serviceCfg = &service.Config{
		Logger: relayer.logger,
		Host:   relayer.host,
		DHT:    relayer.dht,
	}
	// Spawn host handlers for services
	for _, svc := range cfg.Services {
		handler, err := service.Register(serviceCfg, svc)
		if err != nil {
			return nil, fmt.Errorf("failed to register service: %s: %w", svc.Protocol, err)
		}
		relayer.services = append(relayer.services, handler)
	}
	return relayer, nil
}
