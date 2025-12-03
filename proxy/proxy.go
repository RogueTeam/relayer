package proxy

import (
	"context"
	"fmt"
	"net"

	"github.com/armon/go-socks5"
)

type Dialer func(ctx context.Context, network, addr string) (conn net.Conn, err error)

type Proxy struct {
	hostname string
	server   *socks5.Server

	hosts map[string]Dialer
}

func (p *Proxy) dialer(ctx context.Context, network, addr string) (conn net.Conn, err error) {
	host, _, _ := net.SplitHostPort(addr)
	dialer, found := p.hosts[host]
	if found {
		return dialer(ctx, network, host)
	}

	return nil, fmt.Errorf("host not registered: %s", host)
}

func (p *Proxy) Register(service string, dialer Dialer) {
	var host = service
	if p.hostname != "" {
		host += "." + p.hostname
	}
	p.hosts[host] = dialer
}

func (p *Proxy) Serve(l net.Listener) (err error) {
	err = p.server.Serve(l)
	if err != nil {
		return fmt.Errorf("failed to serve http proxy: %w", err)
	}
	return nil
}

type resolver struct {
}

func (r *resolver) Resolve(ctx context.Context, host string) (outCtx context.Context, ip net.IP, err error) {
	return ctx, nil, nil
}

func New(hostname string) (p *Proxy, err error) {
	p = &Proxy{
		hostname: hostname,
		hosts:    map[string]Dialer{},
	}

	server, err := socks5.New(&socks5.Config{
		Resolver: &resolver{},
		Dial:     p.dialer,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create socks5 server: %w", err)
	}

	p.server = server
	return p, nil
}
