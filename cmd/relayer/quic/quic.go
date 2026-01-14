package quic

import (
	"time"

	"github.com/libp2p/go-libp2p"
	libp2pquic "github.com/libp2p/go-libp2p/p2p/transport/quic"
	"github.com/quic-go/quic-go"
)

var quicConf = &quic.Config{
	KeepAlivePeriod: 15 * time.Second,
	MaxIdleTimeout:  30 * time.Second,
}

var TransportOption = libp2p.Transport(libp2pquic.NewTransport, quicConf)
