package proxy_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"testing"

	"github.com/RogueTeam/relayer/proxy"
	"github.com/stretchr/testify/assert"
)

func Test_ProxyHandler(t *testing.T) {
	newHttpMux := func(t *testing.T, payload string) (l net.Listener) {
		assertions := assert.New(t)

		l, err := net.Listen("tcp", "127.0.0.1:0")
		if !assertions.Nil(err, "failed to listen") {
			return nil
		}
		mux := http.NewServeMux()
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(payload))
		})

		go http.Serve(l, mux)
		return l
	}

	testUrl := func(fail bool, t *testing.T, client *http.Client, url string, expect string) {
		assertions := assert.New(t)

		res, err := client.Get(url)
		if fail {
			assertions.NotNil(err, "should fail")
			return
		} else {
			if !assertions.Nil(err, "failed to get proxied url") {
				return
			}
		}
		defer res.Body.Close()

		proxiedContents, err := io.ReadAll(res.Body)
		if !assertions.Nil(err, "failed to read body") {
			return
		}
		if !assertions.Equal(expect, string(proxiedContents), "proxied contents doesn't match") {
			return
		}
	}

	t.Run("Succeed", func(t *testing.T) {
		assertions := assert.New(t)

		const hostname = "rogueteam.com"
		p, err := proxy.New(hostname)
		if !assertions.Nil(err, "failed to prepare proxy") {
			return
		}

		const DirectPayload = "DIRECT"
		directListener := newHttpMux(t, DirectPayload)
		defer directListener.Close()

		const ProxiedPayload = "PROXIED"
		proxiedListener := newHttpMux(t, ProxiedPayload)
		defer proxiedListener.Close()

		proxyListener, err := net.Listen("tcp", "127.0.0.1:0")
		if !assertions.Nil(err, "failed to serve proxy listener") {
			return
		}
		defer proxyListener.Close()

		var proxiedCaptured bool
		var directCaptured bool
		const host = "proxy"
		p.Register(host, func(ctx context.Context, network, addr string) (conn net.Conn, err error) {
			proxiedCaptured = addr == "proxy.rogueteam.com"
			directCaptured = addr != "proxy.rogueteam.com"

			return net.Dial(proxiedListener.Addr().Network(), proxiedListener.Addr().String())
		})

		go p.Serve(proxyListener)

		client := &http.Client{
			Transport: &http.Transport{
				Proxy: func(req *http.Request) (*url.URL, error) {
					return url.Parse(fmt.Sprintf("socks5h://%s", proxyListener.Addr().String()))
				},
			},
		}

		t.Run("Proxied", func(t *testing.T) {
			assertions := assert.New(t)
			testUrl(false, t, client, "http://"+host+"."+hostname, ProxiedPayload)
			if !assertions.True(proxiedCaptured, "proxied not captured") {
				return
			}
		})
		t.Run("Direct", func(t *testing.T) {
			assertions := assert.New(t)
			testUrl(true, t, client, "http://"+directListener.Addr().String(), DirectPayload)
			if !assertions.False(directCaptured, "proxied not captured") {
				return
			}
		})
	})
}
