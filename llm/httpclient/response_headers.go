package httpclient

import (
	"context"
	"net/http"
	"strings"
	"sync"

	"github.com/looplj/axonhub/llm/streams"
)

// ReasoningIncludedHeader tells Codex that upstream usage already includes
// previously generated reasoning tokens.
const ReasoningIncludedHeader = "X-Reasoning-Included"

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

type responseHeadersProvider interface {
	responseHeaders() http.Header
}

type responseHeadersStream struct {
	streams.Stream[*StreamEvent]

	headers         http.Header
	observer        streams.DeliveryAcknowledger
	waiter          streams.FinalizationWaiter
	expectedChoices int
}

func (s *responseHeadersStream) responseHeaders() http.Header {
	return s.headers
}

// WithResponseHeaders attaches HTTP response metadata to a stream without
// widening the generic streams.Stream interface.
func WithResponseHeaders(stream streams.Stream[*StreamEvent], headers http.Header) streams.Stream[*StreamEvent] {
	if stream == nil || len(headers) == 0 {
		return stream
	}

	result := &responseHeadersStream{
		Stream:  stream,
		headers: headers.Clone(),
	}
	if observer, ok := stream.(streams.DeliveryAcknowledger); ok {
		result.observer = observer
	}
	if waiter, ok := stream.(streams.FinalizationWaiter); ok {
		result.waiter = waiter
	}
	if choices, ok := stream.(interface{ ExpectedStreamChoices() int }); ok {
		result.expectedChoices = choices.ExpectedStreamChoices()
	}
	return result
}

func (s *responseHeadersStream) AcknowledgeDelivery() {
	if s.observer != nil {
		s.observer.AcknowledgeDelivery()
	}
}

func (s *responseHeadersStream) DeliveryAcknowledged() bool {
	return s.observer != nil && s.observer.DeliveryAcknowledged()
}

func (s *responseHeadersStream) WaitForFinalization() {
	if s.waiter != nil {
		s.waiter.WaitForFinalization()
	}
}

func (s *responseHeadersStream) Deliver(ctx context.Context, write func() (bool, error)) error {
	boundary, ok := s.Stream.(streams.DeliveryBoundary)
	if ok {
		return boundary.Deliver(ctx, write)
	}
	terminal, err := write()
	if err == nil && terminal {
		s.AcknowledgeDelivery()
	}
	return err
}

func (s *responseHeadersStream) ExpectedStreamChoices() int { return s.expectedChoices }

// GetResponseHeaders returns a copy of response metadata attached to a stream.
func GetResponseHeaders(stream streams.Stream[*StreamEvent]) http.Header {
	provider, ok := stream.(responseHeadersProvider)
	if !ok {
		return nil
	}

	return provider.responseHeaders().Clone()
}

// MergeForwardResponseHeaders copies the small, explicit set of upstream
// headers that are safe and meaningful at AxonHub's client boundary.
func MergeForwardResponseHeaders(dst, src http.Header) http.Header {
	forward := hasOnlyTrueHeaderValues(src, ReasoningIncludedHeader)
	if dst != nil {
		for key := range dst {
			if strings.EqualFold(key, ReasoningIncludedHeader) {
				delete(dst, key)
			}
		}
	}
	if !forward {
		return dst
	}
	if dst == nil {
		dst = make(http.Header)
	}

	dst[ReasoningIncludedHeader] = []string{"true"}

	return dst
}

func hasOnlyTrueHeaderValues(headers http.Header, name string) bool {
	found := false
	for key, values := range headers {
		if !strings.EqualFold(key, name) {
			continue
		}

		for _, value := range values {
			found = true
			if !strings.EqualFold(strings.TrimSpace(value), "true") {
				return false
			}
		}
	}

	return found
}
