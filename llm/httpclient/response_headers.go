package httpclient

import (
	"context"
	"net/http"
	"sync"
)

type responseHeaderCaptureContextKey struct{}

// ResponseHeaderCapture stores the headers from the successful upstream HTTP
// attempt. It is shared through context so streaming executors do not need to
// change the Stream interface solely to expose response metadata.
type ResponseHeaderCapture struct {
	mu      sync.RWMutex
	headers http.Header
}

// WithResponseHeaderCapture installs a request-scoped response header capture.
func WithResponseHeaderCapture(ctx context.Context) (context.Context, *ResponseHeaderCapture) {
	capture := &ResponseHeaderCapture{}

	return context.WithValue(ctx, responseHeaderCaptureContextKey{}, capture), capture
}

// RecordResponseHeaders records headers for the current successful upstream
// attempt when a capture is installed in ctx.
func RecordResponseHeaders(ctx context.Context, headers http.Header) {
	capture, _ := ctx.Value(responseHeaderCaptureContextKey{}).(*ResponseHeaderCapture)
	if capture == nil {
		return
	}

	capture.mu.Lock()
	capture.headers = headers.Clone()
	capture.mu.Unlock()
}

// Reset clears headers before a retry attempt so an executor that does not use
// HTTP cannot inherit metadata from a previous failed attempt.
func (c *ResponseHeaderCapture) Reset() {
	if c == nil {
		return
	}

	c.mu.Lock()
	c.headers = nil
	c.mu.Unlock()
}

// Headers returns a defensive copy of the captured headers.
func (c *ResponseHeaderCapture) Headers() http.Header {
	if c == nil {
		return nil
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.headers.Clone()
}
