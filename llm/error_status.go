package llm

import (
	"net/http"
	"strings"
)

// InferResponseErrorStatusCode restores an HTTP-like status for providers that
// report failures inside an otherwise successful streaming response.
func InferResponseErrorStatusCode(code, errorType, message string) int {
	signal := strings.ToLower(strings.Join([]string{code, errorType}, " "))

	switch {
	case containsAny(signal, "authentication", "unauthorized", "invalid_api_key", "invalid_token"):
		return http.StatusUnauthorized
	case containsAny(signal, "permission", "forbidden", "access_denied"):
		return http.StatusForbidden
	case containsAny(signal, "not_found", "model_not_found"):
		return http.StatusNotFound
	case containsAny(signal, "rate_limit", "too_many_requests", "slow_down", "insufficient_quota"):
		return http.StatusTooManyRequests
	case containsAny(signal, "overloaded", "server_is_overloaded", "capacity"):
		return http.StatusServiceUnavailable
	case containsAny(signal, "timeout", "timed_out"):
		return http.StatusGatewayTimeout
	case containsAny(signal, "server_error", "internal_error", "api_error", "upstream_error", "stream_error"):
		return http.StatusBadGateway
	case containsAny(signal, "invalid_request", "bad_request", "unprocessable", "validation_error"):
		return http.StatusBadRequest
	}

	message = strings.ToLower(message)
	switch {
	case containsAny(message, "too many requests", "rate limit", "slow down"):
		return http.StatusTooManyRequests
	case containsAny(message, "server is overloaded", "server overloaded", "model is at capacity"):
		return http.StatusServiceUnavailable
	case containsAny(message, "upstream timed out", "request timed out"):
		return http.StatusGatewayTimeout
	default:
		return 0
	}
}

func containsAny(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if strings.Contains(value, candidate) {
			return true
		}
	}

	return false
}
