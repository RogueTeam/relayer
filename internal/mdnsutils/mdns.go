package mdnsutils

import (
	"log"
	"time"

	"github.com/RogueTeam/relayer/internal/utils"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
)

type Notifier struct {
	Host           host.Host
	DHT            *dht.IpfsDHT
	ConnectTimeout time.Duration
}

func (n *Notifier) HandlePeerFound(info peer.AddrInfo) {
	var timeout = n.ConnectTimeout
	if timeout == 0 {
		timeout = utils.DefaultTimeout
	}
	log.Println("FOUND REMOTE PEER", info.ID)
	ctx, cancel := utils.NewContextWithTimeout(timeout)
	defer cancel()
	err := n.Host.Connect(ctx, info)
	if err != nil {
		log.Println("Failed to connect to remote peer:", err)
		return
	}
}

var _ mdns.Notifee = (*Notifier)(nil)
