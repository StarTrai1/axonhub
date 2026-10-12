package httpclient

import (
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	http2FallbackDuration   = 10 * time.Minute
	maxHTTP2FallbackOrigins = 128
)

// net/http's bundled HTTP/2 implementation uses an unexported error type.
// Match only its complete peer-reset message, never an HTTP/SSE error payload.
var http2FallbackReset = regexp.MustCompile(`^stream error: stream ID [1-9][0-9]*; (INTERNAL_ERROR|HTTP_1_1_REQUIRED); received from peer$`)

// http2FallbackTransport learns protocol failures without replaying requests.
// The pipeline still owns retry budgets and the first-output commit boundary.
// A reset after output only changes the transport used by subsequent requests.
type http2FallbackTransport struct {
	primary http.RoundTripper
	http1   http.RoundTripper
	mu      sync.Mutex
	until   map[string]time.Time
	now     func() time.Time
}

func newHTTP2FallbackTransport(primary, http1 http.RoundTripper) *http2FallbackTransport {
	return &http2FallbackTransport{
		primary: primary,
		http1:   http1,
		until:   make(map[string]time.Time),
		now:     time.Now,
	}
}

func http2FallbackOrigin(u *url.URL) string {
	if u == nil || !strings.EqualFold(u.Scheme, "https") {
		return ""
	}
	port := u.Port()
	if port == "" {
		port = "443"
	}
	return net.JoinHostPort(strings.ToLower(u.Hostname()), port)
}

func (t *http2FallbackTransport) useHTTP1(origin string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	expires, ok := t.until[origin]
	if ok && !t.now().Before(expires) {
		delete(t.until, origin)
		return false
	}
	return ok
}

func (t *http2FallbackTransport) remember(origin string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	if expires, ok := t.until[origin]; ok && now.Before(expires) {
		// Concurrent failing streams must not indefinitely extend the cooldown.
		return false
	}
	for key, expires := range t.until {
		if !now.Before(expires) {
			delete(t.until, key)
		}
	}
	if len(t.until) >= maxHTTP2FallbackOrigins {
		var oldest string
		var first time.Time
		for key, expires := range t.until {
			if oldest == "" || expires.Before(first) {
				oldest, first = key, expires
			}
		}
		delete(t.until, oldest)
	}
	t.until[origin] = now.Add(http2FallbackDuration)
	return true
}

func (t *http2FallbackTransport) observe(req *http.Request, origin string, err error) {
	if origin == "" || err == nil || req.Context().Err() != nil {
		return
	}
	for cause := err; cause != nil; cause = errors.Unwrap(cause) {
		if http2FallbackReset.MatchString(cause.Error()) {
			if t.remember(origin) {
				slog.WarnContext(req.Context(), "upstream HTTP/2 reset; temporarily using HTTP/1.1 for subsequent requests",
					slog.String("origin", origin), slog.Duration("cooldown", http2FallbackDuration))
			}
			return
		}
	}
}

func (t *http2FallbackTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	origin := http2FallbackOrigin(req.URL)
	if origin != "" && t.useHTTP1(origin) {
		return t.http1.RoundTrip(req)
	}
	resp, err := t.primary.RoundTrip(req)
	t.observe(req, origin, err)
	if err == nil && resp != nil && resp.ProtoMajor == 2 && resp.Body != nil && origin != "" {
		resp.Body = &http2FallbackBody{
			ReadCloser: resp.Body,
			onError:    func(err error) { t.observe(req, origin, err) },
		}
	}
	return resp, err
}

func (t *http2FallbackTransport) CloseIdleConnections() {
	for _, transport := range []http.RoundTripper{t.primary, t.http1} {
		if closer, ok := transport.(interface{ CloseIdleConnections() }); ok {
			closer.CloseIdleConnections()
		}
	}
}

type http2FallbackBody struct {
	io.ReadCloser
	onError func(error)
	once    sync.Once
}

func (b *http2FallbackBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil {
		b.once.Do(func() { b.onError(err) })
	}
	return n, err
}
