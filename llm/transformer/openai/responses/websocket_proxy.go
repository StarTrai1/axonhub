package responses

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/gorilla/websocket"
	"golang.org/x/net/proxy"
)

// Resolve once per connection, with the same HTTP(S) URL seen by Gorilla's
// Proxy callback. The shared dialer remains immutable across concurrent turns.
func webSocketDialerWithProxy(ctx context.Context, dialer *websocket.Dialer, wsURL string, headers http.Header) (*websocket.Dialer, error) {
	if dialer.Proxy == nil {
		return dialer, nil
	}
	u, err := url.Parse(wsURL)
	if err != nil {
		return nil, err
	}
	switch u.Scheme {
	case "ws":
		u.Scheme = "http"
	case "wss":
		u.Scheme = "https"
	}
	request := (&http.Request{Method: http.MethodGet, URL: u, Header: headers.Clone()}).WithContext(ctx)
	proxyURL, err := dialer.Proxy(request)
	if err != nil {
		return nil, err
	}
	resolved := *dialer
	resolved.Proxy = nil
	if proxyURL == nil {
		return &resolved, nil
	}
	switch strings.ToLower(proxyURL.Scheme) {
	case "socks", "socks5", "socks5h":
	default:
		resolved.Proxy = http.ProxyURL(proxyURL)
		return &resolved, nil
	}

	// Gorilla 1.5.3's bundled SOCKS implementation converts I/O errors into
	// strings. Use x/net's context dialer so reset/EOF causes survive, enabling
	// the existing pre-output retry policy without parsing localized messages.
	forward := dialer.NetDialContext
	if forward == nil {
		if dialer.NetDial != nil {
			forward = func(_ context.Context, network, address string) (net.Conn, error) {
				return dialer.NetDial(network, address)
			}
		} else {
			forward = (&net.Dialer{}).DialContext
		}
	}
	normalized := *proxyURL
	normalized.Scheme = "socks5"
	proxyDialer, err := proxy.FromURL(&normalized, webSocketProxyForwardDialer(forward))
	if err != nil {
		return nil, err
	}
	contextDialer, ok := proxyDialer.(proxy.ContextDialer)
	if !ok {
		return nil, fmt.Errorf("SOCKS proxy does not support context dialing")
	}
	resolved.NetDialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		conn, err := contextDialer.DialContext(ctx, network, address)
		if err != nil && ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return conn, err
	}
	// TLS must be negotiated by Gorilla after the SOCKS tunnel is established.
	resolved.NetDial = nil
	resolved.NetDialTLSContext = nil
	return &resolved, nil
}

type webSocketProxyForwardDialer func(context.Context, string, string) (net.Conn, error)

func (d webSocketProxyForwardDialer) Dial(network, address string) (net.Conn, error) {
	return d(context.Background(), network, address)
}

func (d webSocketProxyForwardDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return d(ctx, network, address)
}
