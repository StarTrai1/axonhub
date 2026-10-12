package httpclient

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Exercise Go's real RST_STREAM error, both before headers and during SSE reads.
// Every attempt has exactly one POST; only the caller initiates the next one.
func TestHTTP2FallbackRealReset(t *testing.T) {
	for _, throughProxy := range []bool{false, true} {
		for _, beforeHeaders := range []bool{false, true} {
			t.Run(fmt.Sprintf("proxy=%v/before_headers=%v", throughProxy, beforeHeaders), func(t *testing.T) {
				type received struct { proto int; body, auth string }
				requests := make(chan received, 8)
				upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					body, _ := io.ReadAll(r.Body)
					requests <- received{r.ProtoMajor, string(body), r.Header.Get("Authorization")}
					if r.ProtoMajor == 2 && beforeHeaders { panic(http.ErrAbortHandler) }
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = fmt.Fprint(w, "event: response.output_text.delta\ndata: {\"delta\":\"partial\"}\n\n")
					w.(http.Flusher).Flush()
					if r.ProtoMajor == 2 { panic(http.ErrAbortHandler) }
					_, _ = fmt.Fprint(w, "event: response.completed\ndata: {\"type\":\"response.completed\"}\n\n")
				}))
				upstream.EnableHTTP2 = true
				upstream.StartTLS()
				t.Cleanup(upstream.Close)
				roots := x509.NewCertPool()
				roots.AddCert(upstream.Certificate())
				proxyConfig := &ProxyConfig{Type: ProxyTypeDisabled}
				if throughProxy {
					proxy := newHTTP2FallbackProxy(t, upstream.Listener.Addr().String())
					proxyConfig = &ProxyConfig{Type: ProxyTypeURL, URL: proxy.URL, Username: "fixture-user", Password: "fixture-pass"}
				}
				client := NewHttpClientWithProxy(proxyConfig, WithTLSConfig(&tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}), WithHTTPTransport(HTTPProtocolAuto, 2))
				t.Cleanup(client.CloseIdleConnections)
				ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
				defer cancel()
				request := &Request{Method: http.MethodPost, URL: upstream.URL + "/v1/responses", Body: []byte(`{"model":"gpt-6.1-sol","input":[{"type":"reasoning","encrypted_content":"synthetic-unchanged"}]}`), Auth: &AuthConfig{Type: "bearer", APIKey: "synthetic-key"}}
				stream, err := client.DoStream(ctx, request)
				if beforeHeaders {
					require.ErrorContains(t, err, "INTERNAL_ERROR; received from peer")
				} else {
					require.NoError(t, err)
					var events []string
					for stream.Next() { events = append(events, stream.Current().Type) }
					require.ErrorContains(t, stream.Err(), "INTERNAL_ERROR; received from peer")
					require.Equal(t, []string{"response.output_text.delta"}, events)
					require.NoError(t, stream.Close())
				}
				require.Len(t, requests, 1, "transport must not replay a POST")
				first := <-requests
				require.Equal(t, 2, first.proto)
				stream, err = client.DoStream(ctx, request)
				require.NoError(t, err)
				var events []string
				for stream.Next() { events = append(events, stream.Current().Type) }
				require.NoError(t, stream.Err())
				require.NoError(t, stream.Close())
				require.Equal(t, []string{"response.output_text.delta", "response.completed"}, events)
				require.Len(t, requests, 1)
				second := <-requests
				require.Equal(t, 1, second.proto)
				require.Equal(t, first.body, second.body)
				require.Equal(t, first.auth, second.auth)
				require.False(t, client.TLSConfig().InsecureSkipVerify)
			})
		}
	}
}

func newHTTP2FallbackProxy(t *testing.T, upstream string) *httptest.Server {
	t.Helper()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect || r.Host != upstream || r.Header.Get("Proxy-Authorization") == "" {
			http.Error(w, "invalid synthetic proxy request", http.StatusForbidden)
			return
		}
		dst, err := net.DialTimeout("tcp", upstream, 2*time.Second)
		if err != nil { http.Error(w, "fixture unavailable", http.StatusBadGateway); return }
		defer dst.Close()
		src, buffered, err := w.(http.Hijacker).Hijack()
		if err != nil { return }
		defer src.Close()
		_, _ = buffered.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		_ = buffered.Flush()
		done := make(chan struct{})
		go func() {
			defer func() {
				if recovered := recover(); recovered != nil { slog.Error("fixture tunnel panic", "panic", recovered) }
				close(done)
			}()
			_, _ = io.Copy(dst, buffered)
			_ = dst.Close()
		}()
		_, _ = io.Copy(src, dst)
		_ = src.Close()
		<-done
	}))
	t.Cleanup(proxy.Close)
	return proxy
}

func TestHTTP2FallbackScopeAndExpiry(t *testing.T) {
	now := time.Now()
	transport := newHTTP2FallbackTransport(nil, nil)
	transport.now = func() time.Time { return now }
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://user:pass@UPSTREAM.test/path?secret=1", nil)
	require.NoError(t, err)
	origin := http2FallbackOrigin(req.URL)
	require.Equal(t, "upstream.test:443", origin)
	reset := errors.New("stream error: stream ID 3; INTERNAL_ERROR; received from peer")
	transport.observe(req, origin, fmt.Errorf("read body: %w", reset))
	require.True(t, transport.useHTTP1(origin))
	require.False(t, transport.useHTTP1("other.test:443"))
	require.False(t, transport.useHTTP1("upstream.test:8443"))
	require.False(t, newHTTP2FallbackTransport(nil, nil).useHTTP1(origin), "separate client/proxy pools must remain isolated")
	now = now.Add(http2FallbackDuration - time.Second)
	transport.observe(req, origin, reset)
	now = now.Add(time.Second)
	require.False(t, transport.useHTTP1(origin), "in-flight failures do not extend cooldown")
	for i := range maxHTTP2FallbackOrigins + 1 {
		transport.remember(fmt.Sprintf("host-%d:443", i))
	}
	require.Len(t, transport.until, maxHTTP2FallbackOrigins)
}

func TestHTTP2FallbackIgnoresUnrelatedFailures(t *testing.T) {
	for _, err := range []error{
		io.EOF, io.ErrUnexpectedEOF, context.Canceled,
		errors.New("stream error: stream ID 1; REFUSED_STREAM; received from peer"),
		errors.New("stream error: stream ID 1; PROTOCOL_ERROR; received from peer"),
		errors.New("stream error: stream ID 1; INTERNAL_ERROR; local failure"),
		&Error{StatusCode: 400, Body: []byte("stream error: stream ID 1; INTERNAL_ERROR; received from peer")},
	} {
		transport := newHTTP2FallbackTransport(nil, nil)
		req, requestErr := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://example.test/", nil)
		require.NoError(t, requestErr)
		transport.observe(req, http2FallbackOrigin(req.URL), err)
		require.Empty(t, transport.until)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://example.test/", nil)
	require.NoError(t, err)
	transport := newHTTP2FallbackTransport(nil, nil)
	transport.observe(req, http2FallbackOrigin(req.URL), errors.New("stream error: stream ID 1; INTERNAL_ERROR; received from peer"))
	require.Empty(t, transport.until)
}

func TestHTTP2FallbackConcurrentOrigins(t *testing.T) {
	transport := newHTTP2FallbackTransport(nil, nil)
	var workers sync.WaitGroup
	for i := range 32 {
		workers.Go(func() {
			defer func() {
				if recovered := recover(); recovered != nil { slog.Error("fixture worker panic", "panic", recovered) }
			}()
			for n := range 100 {
				origin := fmt.Sprintf("host-%d:443", (i+n)%8)
				transport.remember(origin)
				transport.useHTTP1(origin)
			}
		})
	}
	workers.Wait()
	require.Len(t, transport.until, 8)
}

func TestHTTP2FallbackRetainsTLSAndExplicitHTTP1(t *testing.T) {
	client := NewHttpClient(WithTLSConfig(&tls.Config{ServerName: "trusted.test", MinVersion: tls.VersionTLS12}), WithHTTPTransport(HTTPProtocolAuto, 4))
	fallback := client.GetNativeClient().Transport.(*http2FallbackTransport)
	http1 := fallback.http1.(*http.Transport)
	require.Equal(t, "trusted.test", http1.TLSClientConfig.ServerName)
	require.False(t, http1.TLSClientConfig.InsecureSkipVerify)
	require.Equal(t, []string{"http/1.1"}, http1.TLSClientConfig.NextProtos)
	require.False(t, http1.Protocols.HTTP2())
	cloned := client.TLSConfig()
	cloned.ServerName = "changed.test"
	require.Equal(t, "trusted.test", client.TLSConfig().ServerName)
	explicit := client.WithHTTPTransport(HTTPProtocolHTTP1, 1)
	_, wrapped := explicit.GetNativeClient().Transport.(*http2FallbackTransport)
	require.False(t, wrapped)
	require.Equal(t, "trusted.test", explicit.TLSConfig().ServerName)
}

// Error text inside a valid SSE event is application data, not a reset.
func TestHTTP2FallbackDoesNotInspectPayloads(t *testing.T) {
	upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: stream error: stream ID 1; INTERNAL_ERROR; received from peer\n\n")
	}))
	upstream.EnableHTTP2 = true
	upstream.StartTLS()
	t.Cleanup(upstream.Close)
	client := NewHttpClientWithProxy(&ProxyConfig{Type: ProxyTypeDisabled}, WithInsecureSkipVerify(true))
	t.Cleanup(client.CloseIdleConnections)
	stream, err := client.DoStream(t.Context(), &Request{Method: http.MethodPost, URL: upstream.URL})
	require.NoError(t, err)
	for stream.Next() { require.True(t, strings.Contains(string(stream.Current().Data), "INTERNAL_ERROR")) }
	require.NoError(t, stream.Err())
	require.NoError(t, stream.Close())
	require.Empty(t, client.GetNativeClient().Transport.(*http2FallbackTransport).until)
}
