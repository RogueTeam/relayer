package ioutils

import (
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/p2p/transport/websocket"
	manet "github.com/multiformats/go-multiaddr/net"
)

type TCPStreamConn struct {
	network.Stream
}

func (s *TCPStreamConn) LocalAddr() net.Addr {
	addr, _ := manet.ToNetAddr(s.Conn().LocalMultiaddr())
	return addrToTcpAddr(addr)
}

func addrToTcpAddr(addr net.Addr) (out net.Addr) {
	switch addr.(type) {
	case *websocket.Addr:
		u, _ := url.Parse(addr.String())
		if u == nil {
			return &net.TCPAddr{}
		}
		if strings.Contains(u.Host, ":") {
			host, ports, _ := net.SplitHostPort(u.Host)
			port, _ := strconv.ParseInt(ports, 10, 64)
			return &net.TCPAddr{
				IP:   net.ParseIP(host),
				Port: int(port),
			}
		} else {
			var port = 80
			if u.Scheme == "wss" {
				port = 443
			}
			return &net.TCPAddr{
				IP:   net.ParseIP(u.Host),
				Port: port,
			}
		}
	default:
		return addr
	}
}

func (s *TCPStreamConn) RemoteAddr() net.Addr {
	addr, _ := manet.ToNetAddr(s.Conn().RemoteMultiaddr())
	return addrToTcpAddr(addr)
}

var _ net.Conn = (*TCPStreamConn)(nil)
