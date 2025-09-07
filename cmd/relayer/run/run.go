package run

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/RogueTeam/relayer"
	"github.com/RogueTeam/relayer/internal/p2p/identity"
	"github.com/ipfs/go-datastore"
	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	"github.com/urfave/cli/v3"
	"gopkg.in/yaml.v3"
)

const (
	ExampleFlag = "example"
	ConfigFlag  = "config"
)

type (
	DHT struct {
		Enabled bool                  `yaml:"enabled"`
		Peers   []multiaddr.Multiaddr `yaml:"bootstrap"`
	}
	Remote struct {
		Name          string                `yaml:"name"`
		ListenAddress multiaddr.Multiaddr   `yaml:"listen"`
		Addresses     []multiaddr.Multiaddr `yaml:"addrs"`
		AllowedPeers  []peer.ID             `yaml:"allowed-peers"`
	}
	Service struct {
		Name         string                `yaml:"name"`
		Addresses    []multiaddr.Multiaddr `yaml:"addrs"`
		AllowedPeers []peer.ID             `yaml:"allowed-peers"`
		Advertise    bool                  `yaml:"advertise"`
	}
	Config struct {
		Listen             []multiaddr.Multiaddr `yaml:"listen"`
		AdvertiseAddresses []multiaddr.Multiaddr `yaml:"advertise-addrs"`
		IdentityFile       string                `yaml:"identity-file"`
		DHT                *DHT                  `yaml:"dht,omitempty"`
		Remotes            []Remote              `yaml:"remotes,omitempty"`
		Services           []Service             `yaml:"services,omitempty"`
	}
)

var Example = Config{
	Listen: []multiaddr.Multiaddr{
		multiaddr.StringCast("/ip4/0.0.0.0/udp/9999/quic-v1"),
	},
	AdvertiseAddresses: []multiaddr.Multiaddr{
		multiaddr.StringCast("/ip4/10.0.0.5/udp/9999/quic-v1"),
	},
	IdentityFile: "identity",
	DHT: &DHT{
		Enabled: true,
		Peers: []multiaddr.Multiaddr{
			multiaddr.StringCast("/ip4/10.0.0.4/udp/9999/quic-v1"),
		},
	},
	Remotes: []Remote{
		{
			Name:          "HTTP",
			ListenAddress: multiaddr.StringCast("/ip4/10.0.0.4/tcp/8080"),
		},
	},
	Services: []Service{
		{
			Name: "FTP",
			Addresses: []multiaddr.Multiaddr{
				multiaddr.StringCast("/ip4/10.0.0.4/tcp/21"),
			},
			Advertise: true,
		},
	},
}

var Run = &cli.Command{
	Name:        "run",
	Description: "Run the relayer in worker mode, allowing exposing locally accessible services or binding remote services",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:        ExampleFlag,
			DefaultText: "Write an example configuration to stdout",
		},
		&cli.StringFlag{
			Name:        ConfigFlag,
			DefaultText: "Configuration file to load",
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) (err error) {
		if cmd.Bool(ExampleFlag) {
			asBytes, err := yaml.Marshal(Example)
			if err != nil {
				return fmt.Errorf("failed to marshal: %w", err)
			}

			fmt.Println(string(asBytes))
			return nil
		}

		log.Println("Bootstrap mode")

		log.Println("Loading configuration")
		filename := cmd.String(ConfigFlag)
		configContents, err := os.ReadFile(filename)
		if err != nil {
			return fmt.Errorf("failed to read contents: %s: %w", filename, err)
		}

		var config Config
		err = yaml.Unmarshal(configContents, &config)
		if err != nil {
			return fmt.Errorf("failed to parse configuration: %w", err)
		}

		log.Println("Configuration loaded")

		log.Println("Preparing Host")
		hostId, err := identity.LoadIdentity(config.IdentityFile)
		if err != nil {
			return fmt.Errorf("failed to load identity: %w", err)
		}

		hostOptions := []libp2p.Option{
			libp2p.Identity(hostId),
			libp2p.ListenAddrs(config.Listen...),
		}
		if len(config.AdvertiseAddresses) > 0 {
			hostOptions = append(hostOptions, libp2p.AddrsFactory(func(_ []multiaddr.Multiaddr) []multiaddr.Multiaddr { return config.AdvertiseAddresses }))
		}

		host, err := libp2p.New(hostOptions...)
		if err != nil {
			return fmt.Errorf("failed to create host: %w", err)
		}

		log.Println("Host prepared")
		var hostDht *dht.IpfsDHT
		if config.DHT != nil {
			log.Println("Preparing DHT")
			dhtOptions := []dht.Option{
				dht.Mode(dht.ModeClient),
				dht.Datastore(datastore.NewMapDatastore()),
			}
			if len(config.DHT.Peers) > 0 {
				infos, err := peer.AddrInfosFromP2pAddrs(config.DHT.Peers...)
				if err != nil {
					return fmt.Errorf("failed to convert to address infos: %w", err)
				}
				dhtOptions = append(dhtOptions, dht.BootstrapPeers(infos...))
			}

			hostDht, err = dht.New(ctx, host, dhtOptions...)
			if err != nil {
				return fmt.Errorf("failed to prepare server DHT: %w", err)
			}
			defer hostDht.Close()

			log.Println("DHT Running")

			log.Println("Bootstraping...")
			err = hostDht.Bootstrap(ctx)
			if err != nil {
				return fmt.Errorf("failed to bootstrap: %w", err)
			}

			if len(config.DHT.Peers) > 0 {
				var bootstraped bool
				for range 60 {
					if host.Peerstore().Peers().Len() > 1 {
						bootstraped = true
						break
					}
					time.Sleep(time.Second)
				}
				if !bootstraped {
					return errors.New("failed to bootstrap peers, timeout")
				}
			}
		}

		var remotes []relayer.Remote
		for _, remote := range config.Remotes {
			remotes = append(remotes, relayer.Remote{
				Name:          remote.Name,
				ListenAddress: remote.ListenAddress,
				Addresses:     remote.Addresses,
				AllowedPeers:  remote.AllowedPeers,
			})
		}
		var svcs []relayer.Service
		for _, svc := range config.Services {
			svcs = append(svcs, relayer.Service{
				Name:         svc.Name,
				Addresses:    svc.Addresses,
				AllowedPeers: svc.AllowedPeers,
				Advertise:    svc.Advertise,
			})
		}
		relayerConf := relayer.Config{
			Logger:   log.Default(),
			Host:     host,
			DHT:      hostDht,
			Remote:   remotes,
			Services: svcs,
		}

		log.Println("Preparing relayer")
		r, err := relayer.New(&relayerConf)
		if err != nil {
			return fmt.Errorf("failed to create relayer: %w", err)
		}

		log.Println("Running relayer")
		err = r.Serve()
		if err != nil {
			return fmt.Errorf("failed to start relayer: %w", err)
		}
		defer r.Close()

		log.Println("Everything set, ready to receive requests")
		<-make(chan struct{}, 1)
		return nil
	},
}
