package relayer_test

import (
	"context"
	"log"
	"testing"
	"time"

	"github.com/RogueTeam/relayer"
	"github.com/RogueTeam/relayer/internal/p2p/identity"
	"github.com/RogueTeam/relayer/internal/utils"
	"github.com/ipfs/go-datastore"
	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
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
	t.Run("DHT", func(t *testing.T) {
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
			dhtIdentity, err := identity.NewKey()
			if !assertions.Nil(err, "failed to create dht identity") {
				return
			}
			servicerIdentity, err := identity.NewKey()
			if !assertions.Nil(err, "failed to create servicer identity") {
				return
			}
			allowedBinderIdentity, err := identity.NewKey()
			if !assertions.Nil(err, "failed to create allowed binder identity") {
				return
			}
			disallowedBinderIdentity, err := identity.NewKey()
			if !assertions.Nil(err, "failed to create disallowed binder identity") {
				return
			}

			// Prepare DHT =================================
			dhtHost, err := libp2p.New(
				libp2p.ListenAddrStrings("/ip4/127.0.0.1/udp/0/quic-v1"),
				libp2p.Identity(dhtIdentity),
			)
			if !assertions.Nil(err, "failed to create dht") {
				return
			}
			defer dhtHost.Close()

			dhtService, err := dht.New(context.TODO(), dhtHost, dht.Mode(dht.ModeServer), dht.Datastore(datastore.NewMapDatastore()))
			if !assertions.Nil(err, "failed to get dht service") {
				return
			}
			defer dhtService.Close()

			// Prepare services ============================
			servicerHost, err := libp2p.New(
				libp2p.ListenAddrStrings("/ip4/127.0.0.1/udp/0/quic-v1"),
				libp2p.Identity(servicerIdentity),
			)
			if !assertions.Nil(err, "failed to create servicer") {
				return
			}
			defer servicerHost.Close()

			servicerDht, err := dht.New(context.TODO(),
				servicerHost,
				dht.BootstrapPeers(dhtHost.Peerstore().PeerInfo(dhtHost.ID())),
				dht.Mode(dht.ModeClient),
				dht.Datastore(datastore.NewMapDatastore()))
			if !assertions.Nil(err, "failed to get dht service") {
				return
			}
			defer servicerDht.Close()

			err = servicerDht.Bootstrap(context.TODO())
			assertions.Nil(err, "failed to bootstrap")
			for range 60 {
				if servicerHost.Peerstore().Peers().Len() > 1 {
					break
				}
				time.Sleep(time.Second)
			}

			servicerConfig := relayer.Config{
				Logger: log.Default(),
				Host:   servicerHost,
				DHT:    servicerDht,
				Services: []relayer.Service{
					{
						Name: "RAW",
						Addresses: []multiaddr.Multiaddr{
							localListener.Multiaddr(),
						},
						AllowedPeers: []peer.ID{
							utils.Must(peer.IDFromPrivateKey(allowedBinderIdentity)),
						},
						Advertise: true,
					},
				},
			}
			servicer, err := relayer.New(&servicerConfig)
			assertions.Nil(err, "failed to prepare servicer")

			err = servicer.Serve()
			assertions.Nil(err, "failed to bind")
			defer servicer.Close()

			// Prepare not allowed binder ==================
			disallowedBinderHost, err := libp2p.New(
				libp2p.ListenAddrStrings("/ip4/127.0.0.1/udp/0/quic-v1"),
				libp2p.Identity(disallowedBinderIdentity),
			)
			if !assertions.Nil(err, "failed to create binder") {
				return
			}
			defer disallowedBinderHost.Close()

			disallowedDht, err := dht.New(context.TODO(),
				disallowedBinderHost,
				dht.BootstrapPeers(dhtHost.Peerstore().PeerInfo(dhtHost.ID())),
				dht.Mode(dht.ModeClient),
				dht.Datastore(datastore.NewMapDatastore()))
			if !assertions.Nil(err, "failed to get dht service") {
				return
			}
			defer disallowedDht.Close()

			err = disallowedDht.Bootstrap(context.TODO())
			assertions.Nil(err, "failed to bootstrap")
			for range 60 {
				if disallowedBinderHost.Peerstore().Peers().Len() > 1 {
					break
				}
				time.Sleep(time.Second)
			}

			disallowedBindListener, err := manet.Listen(multiaddr.StringCast("/ip4/127.0.0.1/tcp/0"))
			if !assertions.Nil(err, "failed to listen temp bind") {
				return
			}
			disallowedBindListener.Close()

			assertions.Nil(err, "failed to get remote addresses")
			disallowedBinderConfig := relayer.Config{
				Logger: log.Default(),
				Host:   disallowedBinderHost,
				DHT:    disallowedDht,
				Remote: []relayer.Remote{
					{
						Name:          "RAW",
						ListenAddress: disallowedBindListener.Multiaddr(),
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

			allowedDht, err := dht.New(context.TODO(),
				allowedBinderHost,
				dht.BootstrapPeers(dhtHost.Peerstore().PeerInfo(dhtHost.ID())),
				dht.Mode(dht.ModeClient),
				dht.Datastore(datastore.NewMapDatastore()))
			if !assertions.Nil(err, "failed to get dht service") {
				return
			}
			defer allowedDht.Close()

			err = allowedDht.Bootstrap(context.TODO())
			assertions.Nil(err, "failed to bootstrap")
			for range 60 {
				if allowedBinderHost.Peerstore().Peers().Len() > 1 {
					break
				}
				time.Sleep(time.Second)
			}

			allowedBindListener, err := manet.Listen(multiaddr.StringCast("/ip4/127.0.0.1/tcp/0"))
			if !assertions.Nil(err, "failed to listen temp bind") {
				return
			}
			allowedBindListener.Close()

			assertions.Nil(err, "failed to get remote addresses")
			allowedBinderConfig := relayer.Config{
				Logger: log.Default(),
				Host:   allowedBinderHost,
				DHT:    allowedDht,
				Remote: []relayer.Remote{
					{
						Name:          "RAW",
						ListenAddress: allowedBindListener.Multiaddr(),
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

			// Prepare DHT =================================
			dhtIdentity, err := identity.NewKey()
			if !assertions.Nil(err, "failed to create dht identity") {
				return
			}
			dhtHost, err := libp2p.New(
				libp2p.ListenAddrStrings("/ip4/127.0.0.1/udp/0/quic-v1"),
				libp2p.Identity(dhtIdentity),
			)
			if !assertions.Nil(err, "failed to create dht") {
				return
			}
			defer dhtHost.Close()

			dhtService, err := dht.New(context.TODO(), dhtHost, dht.Mode(dht.ModeServer), dht.Datastore(datastore.NewMapDatastore()))
			if !assertions.Nil(err, "failed to get dht service") {
				return
			}
			defer dhtService.Close()

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

			servicerDht, err := dht.New(context.TODO(),
				servicerHost,
				dht.BootstrapPeers(dhtHost.Peerstore().PeerInfo(dhtHost.ID())),
				dht.Mode(dht.ModeClient),
				dht.Datastore(datastore.NewMapDatastore()))
			if !assertions.Nil(err, "failed to get dht service") {
				return
			}
			defer servicerDht.Close()

			err = servicerDht.Bootstrap(context.TODO())
			assertions.Nil(err, "failed to bootstrap")
			for range 60 {
				if servicerHost.Peerstore().Peers().Len() > 1 {
					break
				}
				time.Sleep(time.Second)
			}

			servicerConfig := relayer.Config{
				Logger: log.Default(),
				Host:   servicerHost,
				DHT:    servicerDht,
				Services: []relayer.Service{
					{
						Name: "RAW",
						Addresses: []multiaddr.Multiaddr{
							localListener.Multiaddr(),
						},
						Advertise: true,
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

			binderDht, err := dht.New(context.TODO(),
				binderHost,
				dht.BootstrapPeers(dhtHost.Peerstore().PeerInfo(dhtHost.ID())),
				dht.Mode(dht.ModeClient),
				dht.Datastore(datastore.NewMapDatastore()))
			if !assertions.Nil(err, "failed to get dht service") {
				return
			}
			defer binderDht.Close()

			err = binderDht.Bootstrap(context.TODO())
			assertions.Nil(err, "failed to bootstrap")
			for range 60 {
				if binderHost.Peerstore().Peers().Len() > 1 {
					break
				}
				time.Sleep(time.Second)
			}

			tempBindListener, err := manet.Listen(multiaddr.StringCast("/ip4/127.0.0.1/tcp/0"))
			if !assertions.Nil(err, "failed to listen temp bind") {
				return
			}
			tempBindListener.Close()

			assertions.Nil(err, "failed to get remote addresses")
			binderConfig := relayer.Config{
				Logger: log.Default(),
				Host:   binderHost,
				DHT:    binderDht,
				Remote: []relayer.Remote{
					{
						Name:          "RAW",
						ListenAddress: tempBindListener.Multiaddr(),
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
