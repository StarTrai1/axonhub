package shared

import (
	"context"
	"sync/atomic"
)

// sessionContextKey is the key used to store and retrieve the session ID from the context.
type sessionContextKey struct{}

// sessionScopeContextKey is the key used to namespace client-provided session IDs.
type sessionScopeContextKey struct{}

// responsesWebSocketContextKey marks requests received through the downstream
// Responses WebSocket endpoint.
type responsesWebSocketContextKey struct{}

// responsesWebSocketSteerContextKey carries downstream GPT-6 Astra steering
// messages to the active upstream Responses WebSocket lease.
type responsesWebSocketSteerContextKey struct{}

// ResponsesWebSocketSteering forwards GPT-6 Astra mid-turn input to the
// active upstream Responses WebSocket connection.
type ResponsesWebSocketSteering struct {
	events chan []byte
	ready  atomic.Bool
}

// responsesAPIContextKey marks requests using the OpenAI Responses API.
type responsesAPIContextKey struct{}

// WithSessionID sets the session ID in the context.
// This is essential for features that require cross-request state, such as:
// 1. Prompt Caching: Providers like Anthropic use session/trace IDs to optimize cache hits.
// 2. Tracing: It allows linking the transformation pipeline with the unified tracing system (AH-Trace-Id).
func WithSessionID(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, sessionContextKey{}, sessionID)
}

// GetSessionID retrieves the session ID from the context.
func GetSessionID(ctx context.Context) (string, bool) {
	sessionID, ok := ctx.Value(sessionContextKey{}).(string)
	return sessionID, ok
}

// WithSessionScope sets a trusted server-side namespace for session-scoped upstream state.
func WithSessionScope(ctx context.Context, scope string) context.Context {
	return context.WithValue(ctx, sessionScopeContextKey{}, scope)
}

// GetSessionScope retrieves the trusted server-side namespace for session-scoped upstream state.
func GetSessionScope(ctx context.Context) (string, bool) {
	scope, ok := ctx.Value(sessionScopeContextKey{}).(string)
	return scope, ok
}

// WithResponsesWebSocket marks a request as originating from the downstream
// Responses WebSocket endpoint.
func WithResponsesWebSocket(ctx context.Context) context.Context {
	return context.WithValue(WithResponsesAPI(ctx), responsesWebSocketContextKey{}, true)
}

// IsResponsesWebSocket reports whether a request originated from the
// downstream Responses WebSocket endpoint.
func IsResponsesWebSocket(ctx context.Context) bool {
	enabled, _ := ctx.Value(responsesWebSocketContextKey{}).(bool)
	return enabled
}

// NewResponsesWebSocketSteering creates a bounded steering queue.
func NewResponsesWebSocketSteering(capacity int) *ResponsesWebSocketSteering {
	return &ResponsesWebSocketSteering{events: make(chan []byte, capacity)}
}

// Events returns queued response.steer payloads.
func (s *ResponsesWebSocketSteering) Events() <-chan []byte {
	if s == nil {
		return nil
	}
	return s.events
}

// Activate marks the upstream WebSocket as ready for steering.
func (s *ResponsesWebSocketSteering) Activate() {
	if s != nil {
		s.ready.Store(true)
	}
}

// Deactivate marks the upstream WebSocket as unavailable for steering.
func (s *ResponsesWebSocketSteering) Deactivate() {
	if s != nil {
		s.ready.Store(false)
	}
}

// Ready reports whether an upstream WebSocket is accepting steering input.
func (s *ResponsesWebSocketSteering) Ready() bool {
	return s != nil && s.ready.Load()
}

// Send queues a response.steer payload without blocking.
func (s *ResponsesWebSocketSteering) Send(message []byte) bool {
	if !s.Ready() {
		return false
	}
	select {
	case s.events <- message:
		return true
	default:
		return false
	}
}

// WithResponsesWebSocketSteer attaches downstream GPT-6 Astra steering to a request.
func WithResponsesWebSocketSteer(ctx context.Context, steering *ResponsesWebSocketSteering) context.Context {
	return context.WithValue(ctx, responsesWebSocketSteerContextKey{}, steering)
}

// GetResponsesWebSocketSteer retrieves downstream GPT-6 Astra steering state.
func GetResponsesWebSocketSteer(ctx context.Context) (*ResponsesWebSocketSteering, bool) {
	steering, ok := ctx.Value(responsesWebSocketSteerContextKey{}).(*ResponsesWebSocketSteering)
	return steering, ok
}

// WithResponsesAPI marks a request as using the OpenAI Responses API.
func WithResponsesAPI(ctx context.Context) context.Context {
	return context.WithValue(ctx, responsesAPIContextKey{}, true)
}

// IsResponsesAPI reports whether a request uses the OpenAI Responses API.
func IsResponsesAPI(ctx context.Context) bool {
	enabled, _ := ctx.Value(responsesAPIContextKey{}).(bool)
	return enabled
}
