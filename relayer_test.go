package relayer_test

import (
	"log"
	"testing"

	"github.com/RogueTeam/relayer"
	"github.com/RogueTeam/relayer/internal/p2p/identity"
	"github.com/RogueTeam/relayer/internal/utils"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
	"github.com/stretchr/testify/assert"
)

func Test_Relayer(t *testing.T) {
	t.Run("No DHT", func(t *testing.T) {
		t.Run("With Allowed Peers", func(t *testing.T) {
			assertions := assert.New(t)

			// Local service ===============================
			localListener, err := manet.Listen(multiaddr.StringCast("/ip4/127.0.0.1/tcp/0"))
			if !assertions.Nil(err, "failed to listen local service") {
				return
			}
			defer localListener.Close()

			var payload = []byte("HELLO WORLD")
			go func() {
				conn, err := localListener.Accept()
				if !assertions.Nil(err, "failed to accept connection") {
					return
				}
				defer conn.Close()

				conn.Write(payload)
			}()
			// Prepare identities ==========================
			servicerIdentity, err := identity.NewKey()
			if !assertions.Nil(err, "failed to create binder identity") {
				return
			}
			allowedBinderIdentity, err := identity.NewKey()
			if !assertions.Nil(err, "failed to create allowed binder identity") {
				return
			}
			disallowedBinderIdentity, err := identity.NewKey()
			if !assertions.Nil(err, "failed to create allowed binder identity") {
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
				Logger: log.Default(),
				Host:   servicerHost,
				Services: []relayer.Service{
					{
						Name: "RAW",
						Addresses: []multiaddr.Multiaddr{
							localListener.Multiaddr(),
						},
						AllowedPeers: []peer.ID{
							utils.Must(peer.IDFromPrivateKey(allowedBinderIdentity)),
						},
					},
				},
			}
			servicer, err := relayer.New(&servicerConfig)
			assertions.Nil(err, "failed to prepare servicer")

			err = servicer.Serve()
			assertions.Nil(err, "failed to bind")
			defer servicer.Close()

			addrInfo := servicerHost.Peerstore().PeerInfo(servicerHost.ID())
			remoteAddrs, err := peer.AddrInfoToP2pAddrs(&addrInfo)
			// Prepare not allowed binder ==================
			disallowedBinderHost, err := libp2p.New(
				libp2p.ListenAddrStrings("/ip4/127.0.0.1/udp/0/quic-v1"),
				libp2p.Identity(disallowedBinderIdentity),
			)
			if !assertions.Nil(err, "failed to create binder") {
				return
			}
			defer disallowedBinderHost.Close()

			disallowedBindListener, err := manet.Listen(multiaddr.StringCast("/ip4/127.0.0.1/tcp/0"))
			if !assertions.Nil(err, "failed to listen temp bind") {
				return
			}
			disallowedBindListener.Close()

			assertions.Nil(err, "failed to get remote addresses")
			disallowedBinderConfig := relayer.Config{
				Logger: log.Default(),
				Host:   disallowedBinderHost,
				Remote: []relayer.Remote{
					{
						Name:          "RAW",
						ListenAddress: disallowedBindListener.Multiaddr(),
						Addresses:     remoteAddrs,
					},
				},
			}
			disallowedBinder, err := relayer.New(&disallowedBinderConfig)
			assertions.Nil(err, "failed to prepare binder")

			err = disallowedBinder.Serve()
			assertions.Nil(err, "failed to bind")
			defer disallowedBinder.Close()

			// Test disallowed binder ======================
			conn, err := manet.Dial(disallowedBindListener.Multiaddr())
			assertions.Nil(err, "failed to dial to binded address")
			defer conn.Close()

			var recv = make([]byte, len(payload))
			_, err = conn.Read(recv)
			assertions.NotNil(err, "failed to read")

			assertions.NotEqual(payload, recv, "doesn't match")

			// Prepare allowed binder ======================
			allowedBinderHost, err := libp2p.New(
				libp2p.ListenAddrStrings("/ip4/127.0.0.1/udp/0/quic-v1"),
				libp2p.Identity(allowedBinderIdentity),
			)
			if !assertions.Nil(err, "failed to create binder") {
				return
			}
			defer allowedBinderHost.Close()

			allowedBindListener, err := manet.Listen(multiaddr.StringCast("/ip4/127.0.0.1/tcp/0"))
			if !assertions.Nil(err, "failed to listen temp bind") {
				return
			}
			allowedBindListener.Close()

			assertions.Nil(err, "failed to get remote addresses")
			allowedBinderConfig := relayer.Config{
				Logger: log.Default(),
				Host:   allowedBinderHost,
				Remote: []relayer.Remote{
					{
						Name:          "RAW",
						ListenAddress: allowedBindListener.Multiaddr(),
						Addresses:     remoteAddrs,
					},
				},
			}
			binder, err := relayer.New(&allowedBinderConfig)
			assertions.Nil(err, "failed to prepare binder")

			err = binder.Serve()
			assertions.Nil(err, "failed to bind")
			defer binder.Close()

			// Test allowed binder =========================
			conn, err = manet.Dial(allowedBindListener.Multiaddr())
			assertions.Nil(err, "failed to dial to binded address")
			defer conn.Close()

			recv = make([]byte, len(payload))
			_, err = conn.Read(recv)
			assertions.Nil(err, "failed to read")

			assertions.Equal(payload, recv, "doesn't match")

		})
		t.Run("No Allowed Peers", func(t *testing.T) {
			assertions := assert.New(t)

			// Local service ===============================
			localListener, err := manet.Listen(multiaddr.StringCast("/ip4/127.0.0.1/tcp/0"))
			if !assertions.Nil(err, "failed to listen local service") {
				return
			}
			defer localListener.Close()

			var payload = []byte("HELLO WORLD")
			go func() {
				conn, err := localListener.Accept()
				if !assertions.Nil(err, "failed to accept connection") {
					return
				}
				defer conn.Close()

				conn.Write(payload)
			}()

			// Prepare services ============================
			servicerIdentity, err := identity.NewKey()
			if !assertions.Nil(err, "failed to create binder identity") {
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
				Logger: log.Default(),
				Host:   servicerHost,
				Services: []relayer.Service{
					{
						Name: "RAW",
						Addresses: []multiaddr.Multiaddr{
							localListener.Multiaddr(),
						},
					},
				},
			}
			servicer, err := relayer.New(&servicerConfig)
			assertions.Nil(err, "failed to prepare servicer")

			err = servicer.Serve()
			assertions.Nil(err, "failed to bind")
			defer servicer.Close()

			// Prepare binder ==============================
			binderIdentity, err := identity.NewKey()
			if !assertions.Nil(err, "failed to create identity") {
				return
			}

			binderHost, err := libp2p.New(
				libp2p.ListenAddrStrings("/ip4/127.0.0.1/udp/0/quic-v1"),
				libp2p.Identity(binderIdentity),
			)
			if !assertions.Nil(err, "failed to create binder") {
				return
			}
			defer binderHost.Close()

			tempBindListener, err := manet.Listen(multiaddr.StringCast("/ip4/127.0.0.1/tcp/0"))
			if !assertions.Nil(err, "failed to listen temp bind") {
				return
			}
			tempBindListener.Close()

			addrInfo := servicerHost.Peerstore().PeerInfo(servicerHost.ID())
			remoteAddrs, err := peer.AddrInfoToP2pAddrs(&addrInfo)
			assertions.Nil(err, "failed to get remote addresses")
			binderConfig := relayer.Config{
				Logger: log.Default(),
				Host:   binderHost,
				Remote: []relayer.Remote{
					{
						Name:          "RAW",
						ListenAddress: tempBindListener.Multiaddr(),
						Addresses:     remoteAddrs,
					},
				},
			}
			allowedBinder, err := relayer.New(&binderConfig)
			assertions.Nil(err, "failed to prepare binder")

			err = allowedBinder.Serve()
			assertions.Nil(err, "failed to bind")
			defer allowedBinder.Close()

			conn, err := manet.Dial(tempBindListener.Multiaddr())
			assertions.Nil(err, "failed to dial to binded address")
			defer conn.Close()

			var recv = make([]byte, len(payload))
			_, err = conn.Read(recv)
			assertions.Nil(err, "failed to read")

			assertions.Equal(payload, recv, "doesn't match")
		})

	})
}
