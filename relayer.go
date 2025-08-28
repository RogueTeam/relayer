package relayer

import (
	"errors"
	"fmt"
	"log"

	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/host"
	manet "github.com/multiformats/go-multiaddr/net"
)

type Relayer struct {
	logger   *log.Logger
	host     host.Host
	dht      *dht.IpfsDHT
	remote   []Remote
	services []Service

	running         bool
	remoteListeners map[string]manet.Listener
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
