package shared

import (
	"slices"
	"sync"
)

// ResponsesWebSocketTerminals retains a bounded set of recent response IDs.
// Its zero value is ready for use. Each instance belongs to one connection or
// one upstream execution; it must not be shared across those ownership scopes.
type ResponsesWebSocketTerminals struct {
	mu   sync.Mutex
	ids  [128]string
	next int
}

// Remember records a response-level terminal event, before delivering it.
func (s *ResponsesWebSocketTerminals) Remember(id string) {
	if id == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if slices.Contains(s.ids[:], id) {
		return
	}
	s.ids[s.next] = id
	s.next = (s.next + 1) % len(s.ids)
}

// Contains reports whether the response recently ended in this scope.
func (s *ResponsesWebSocketTerminals) Contains(id string) bool {
	if id == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Contains(s.ids[:], id)
}

// RememberTerminalResponse closes the race between the upstream reader ending
// a response and the downstream writer delivering its terminal event.
func (s *ResponsesWebSocketSteering) RememberTerminalResponse(id string) {
	if s != nil {
		s.terminals.Remember(id)
	}
}

// IsTerminalResponse only recognizes responses from this upstream execution.
func (s *ResponsesWebSocketSteering) IsTerminalResponse(id string) bool {
	return s != nil && s.terminals.Contains(id)
}
