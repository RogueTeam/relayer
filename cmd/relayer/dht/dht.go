package dht

import (
	"context"
	"fmt"
	"log"
	"os"

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

type Config struct {
	Listen             []multiaddr.Multiaddr `yaml:"listen"`
	AdvertiseAddresses []multiaddr.Multiaddr `yaml:"advertise-addrs"`
	IdentityFile       string                `yaml:"identity-file"`
	BootstrapPeers     []multiaddr.Multiaddr `yaml:"bootstrap-peers"`
}

var DHT = &cli.Command{
	Name:        "dht",
	Description: "Starts a DHT server. This is useful in in Plug&Play scenarios",
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
			config := Config{
				Listen: []multiaddr.Multiaddr{
					multiaddr.StringCast("/ip4/0.0.0.0/udp/9999/quic-v1"),
				},
				AdvertiseAddresses: []multiaddr.Multiaddr{
					multiaddr.StringCast("/ip4/10.0.0.5/udp/9999/quic-v1"),
				},
				IdentityFile: "identity",
				BootstrapPeers: []multiaddr.Multiaddr{
					multiaddr.StringCast("/ip4/10.0.0.4/udp/9999/quic-v1"),
				},
			}
			asBytes, err := yaml.Marshal(config)
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
		log.Println("Preparing DHT")

		dhtOptions := []dht.Option{
			dht.Mode(dht.ModeServer),
			dht.Datastore(datastore.NewMapDatastore()),
		}
		if len(config.BootstrapPeers) > 0 {
			infos, err := peer.AddrInfosFromP2pAddrs(config.BootstrapPeers...)
			if err != nil {
				return fmt.Errorf("failed to convert to address infos: %w", err)
			}
			dhtOptions = append(dhtOptions, dht.BootstrapPeers(infos...))
		}

		hostDht, err := dht.New(ctx, host, dhtOptions...)
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

		log.Println("Everything set, ready to receive requests")
		<-make(chan struct{}, 1)
		return nil
	},
}
