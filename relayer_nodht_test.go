package relayer_test

import (
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/RogueTeam/relayer"
	"github.com/RogueTeam/relayer/internal/p2p/identity"
	"github.com/RogueTeam/relayer/internal/utils"
	"github.com/RogueTeam/relayer/remote"
	"github.com/RogueTeam/relayer/service"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
	"github.com/stretchr/testify/assert"
)

func Test_NoDHT(t *testing.T) {
	var Payload = []byte(strings.Repeat("Hello world", 1024))
	var DefaultSlogHandler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
	t.Run("Succeed", func(t *testing.T) {
		t.Run("With Allowed Peers", func(t *testing.T) {
			ctx, cancel := utils.NewContext()
			defer cancel()
			assertions := assert.New(t)

			// Local service ===============================
			localListener, err := manet.Listen(multiaddr.StringCast("/ip4/127.0.0.1/tcp/0"))
			if !assertions.Nil(err, "failed to listen local service") {
				return
			}
			defer localListener.Close()

			go func() {
				conn, err := localListener.Accept()
				if !assertions.Nil(err, "failed to accept connection") {
					return
				}
				defer conn.Close()

				t.Log("Writing buffer")
				conn.Write(Payload)
			}()
			// Prepare identities ==========================
			servicerIdentity, err := identity.NewKey()
			if !assertions.Nil(err, "failed to create client identity") {
				return
			}
			allowedClientIdentity, err := identity.NewKey()
			if !assertions.Nil(err, "failed to create allowed client identity") {
				return
			}
			disallowedClientIdentity, err := identity.NewKey()
			if !assertions.Nil(err, "failed to create allowed client identity") {
				return
			}

			// Prepare services ============================
			servicerHost, err := libp2p.New(
				libp2p.ListenAddrStrings("/ip4/127.0.0.1/udp/0/quic-v1"),
				libp2p.Identity(servicerIdentity),
			)
			if !assertions.Nil(err, "failed to create servicer") {
				return
			}
			defer servicerHost.Close()

			servicerConfig := relayer.Config{
				Logger: slog.New(DefaultSlogHandler),
				Host:   servicerHost,
				Services: []*service.Service{
					{
						Name: "RAW",
						Addresses: []multiaddr.Multiaddr{
							localListener.Multiaddr(),
						},
						AllowedPeers: []peer.ID{
							utils.Must(peer.IDFromPrivateKey(allowedClientIdentity)),
						},
					},
				},
			}
			servicer, err := relayer.New(ctx, &servicerConfig)
			assertions.Nil(err, "failed to prepare servicer")

			err = servicer.Serve()
			assertions.Nil(err, "failed to bind")
			defer servicer.Close()

			addrInfo := servicerHost.Peerstore().PeerInfo(servicerHost.ID())
			remoteAddrs, err := peer.AddrInfoToP2pAddrs(&addrInfo)
			// Prepare not allowed client ==================
			disallowedClientHost, err := libp2p.New(
				libp2p.ListenAddrStrings("/ip4/127.0.0.1/udp/0/quic-v1"),
				libp2p.Identity(disallowedClientIdentity),
			)
			if !assertions.Nil(err, "failed to create client") {
				return
			}
			defer disallowedClientHost.Close()

			disallowedBindListener, err := manet.Listen(multiaddr.StringCast("/ip4/127.0.0.1/tcp/0"))
			if !assertions.Nil(err, "failed to listen temp bind") {
				return
			}
			disallowedBindListener.Close()

			assertions.Nil(err, "failed to get remote addresses")
			disallowedClientConfig := relayer.Config{
				Logger: slog.New(DefaultSlogHandler),
				Host:   disallowedClientHost,
				Remote: []*remote.Remote{
					{
						Name:          "RAW",
						ListenAddress: disallowedBindListener.Multiaddr(),
						Addresses:     remoteAddrs,
					},
				},
			}
			disallowedClient, err := relayer.New(ctx, &disallowedClientConfig)
			assertions.Nil(err, "failed to prepare client")

			err = disallowedClient.Serve()
			assertions.Nil(err, "failed to bind")
			defer disallowedClient.Close()

			// Test disallowed client ======================
			conn, err := manet.Dial(disallowedBindListener.Multiaddr())
			if !assertions.Nil(err, "failed to dial to binded address") {
				return
			}
			defer conn.Close()

			t.Log("Reading from disallowed peer")
			var recv = make([]byte, len(Payload))
			_, err = conn.Read(recv)
			if !assertions.NotNil(err, "failed to read") {
				return
			}
			if !assertions.NotEqual(Payload, recv, "doesn't match") {
				return
			}

			// Prepare allowed client ======================
			allowedClientHost, err := libp2p.New(
				libp2p.ListenAddrStrings("/ip4/127.0.0.1/udp/0/quic-v1"),
				libp2p.Identity(allowedClientIdentity),
			)
			if !assertions.Nil(err, "failed to create client") {
				return
			}
			defer allowedClientHost.Close()

			allowedBindListener, err := manet.Listen(multiaddr.StringCast("/ip4/127.0.0.1/tcp/0"))
			if !assertions.Nil(err, "failed to listen temp bind") {
				return
			}
			allowedBindListener.Close()

			assertions.Nil(err, "failed to get remote addresses")
			allowedClientConfig := relayer.Config{
				Logger: slog.New(DefaultSlogHandler),
				Host:   allowedClientHost,
				Remote: []*remote.Remote{
					{
						Name:          "RAW",
						ListenAddress: allowedBindListener.Multiaddr(),
						Addresses:     remoteAddrs,
					},
				},
			}
			client, err := relayer.New(ctx, &allowedClientConfig)
			assertions.Nil(err, "failed to prepare client")

			err = client.Serve()
			assertions.Nil(err, "failed to bind")
			defer client.Close()

			// Test allowed client =========================
			t.Log("Reading from allowed peer")
			conn, err = manet.Dial(allowedBindListener.Multiaddr())
			if !assertions.Nil(err, "failed to dial to binded address") {
				return
			}
			defer conn.Close()

			recv = make([]byte, len(Payload))
			_, err = conn.Read(recv)
			if !assertions.Nil(err, "failed to read") {
				return
			}
			t.Log("Readed from allowed peer, comparing data")

			if !assertions.Equal(Payload, recv, "doesn't match") {
				return
			}
		})
		t.Run("No Allowed Peers", func(t *testing.T) {
			ctx, cancel := utils.NewContext()
			defer cancel()
			assertions := assert.New(t)

			// Local service ===============================
			localListener, err := manet.Listen(multiaddr.StringCast("/ip4/127.0.0.1/tcp/0"))
			if !assertions.Nil(err, "failed to listen local service") {
				return
			}
			defer localListener.Close()

			go func() {
				conn, err := localListener.Accept()
				if !assertions.Nil(err, "failed to accept connection") {
					return
				}
				defer conn.Close()

				conn.Write(Payload)
			}()

			// Prepare services ============================
			servicerIdentity, err := identity.NewKey()
			if !assertions.Nil(err, "failed to create client identity") {
				return
			}

			servicerHost, err := libp2p.New(
				libp2p.ListenAddrStrings("/ip4/127.0.0.1/udp/0/quic-v1"),
				libp2p.Identity(servicerIdentity),
			)
			if !assertions.Nil(err, "failed to create servicer") {
				return
			}
			defer servicerHost.Close()

			servicerConfig := relayer.Config{
				Logger: slog.New(DefaultSlogHandler),
				Host:   servicerHost,
				Services: []*service.Service{
					{
						Name: "RAW",
						Addresses: []multiaddr.Multiaddr{
							localListener.Multiaddr(),
						},
					},
				},
			}
			servicer, err := relayer.New(ctx, &servicerConfig)
			assertions.Nil(err, "failed to prepare servicer")

			err = servicer.Serve()
			assertions.Nil(err, "failed to bind")
			defer servicer.Close()

			// Prepare client ==============================
			clientIdentity, err := identity.NewKey()
			if !assertions.Nil(err, "failed to create identity") {
				return
			}

			clientHost, err := libp2p.New(
				libp2p.ListenAddrStrings("/ip4/127.0.0.1/udp/0/quic-v1"),
				libp2p.Identity(clientIdentity),
			)
			if !assertions.Nil(err, "failed to create client") {
				return
			}
			defer clientHost.Close()

			tempBindListener, err := manet.Listen(multiaddr.StringCast("/ip4/127.0.0.1/tcp/0"))
			if !assertions.Nil(err, "failed to listen temp bind") {
				return
			}
			tempBindListener.Close()

			addrInfo := servicerHost.Peerstore().PeerInfo(servicerHost.ID())
			remoteAddrs, err := peer.AddrInfoToP2pAddrs(&addrInfo)
			assertions.Nil(err, "failed to get remote addresses")
			clientConfig := relayer.Config{
				Logger: slog.New(DefaultSlogHandler),
				Host:   clientHost,
				Remote: []*remote.Remote{
					{
						Name:          "RAW",
						ListenAddress: tempBindListener.Multiaddr(),
						Addresses:     remoteAddrs,
					},
				},
			}
			allowedClient, err := relayer.New(ctx, &clientConfig)
			assertions.Nil(err, "failed to prepare client")

			err = allowedClient.Serve()
			assertions.Nil(err, "failed to bind")
			defer allowedClient.Close()

			conn, err := manet.Dial(tempBindListener.Multiaddr())
			assertions.Nil(err, "failed to dial to binded address")
			defer conn.Close()

			var recv = make([]byte, len(Payload))
			_, err = conn.Read(recv)
			assertions.Nil(err, "failed to read")

			assertions.Equal(Payload, recv, "doesn't match")
		})
	})
}
