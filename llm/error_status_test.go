package llm

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInferResponseErrorStatusCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		code      string
		errorType string
		message   string
		want      int
	}{
		{name: "rate limit code", code: "rate_limit_exceeded", want: http.StatusTooManyRequests},
		{name: "flow slow down", code: "slow_down", want: http.StatusTooManyRequests},
		{name: "credit balance exhausted", code: "credit_balance_exhausted", want: http.StatusTooManyRequests},
		{name: "organization spend limit", code: "organization_spend_limit_exceeded", want: http.StatusTooManyRequests},
		{name: "project spend limit", code: "project_spend_limit_exceeded", want: http.StatusTooManyRequests},
		{name: "organization usage limit", code: "organization_usage_limit_exceeded", want: http.StatusTooManyRequests},
		{name: "anthropic overload", errorType: "overloaded_error", want: http.StatusServiceUnavailable},
		{name: "server error", code: "server_error", want: http.StatusBadGateway},
		{name: "invalid request", errorType: "invalid_request_error", want: http.StatusBadRequest},
		{name: "encrypted input", code: "invalid_encrypted_content", errorType: "error", want: http.StatusBadRequest},
		{name: "unknown parameter", code: "unknown_parameter", errorType: "error", want: http.StatusBadRequest},
		{name: "unsupported parameter", code: "unsupported_parameter", errorType: "error", want: http.StatusBadRequest},
		{name: "message fallback", message: "The model is at capacity", want: http.StatusServiceUnavailable},
		{name: "unknown", code: "provider_specific", message: "something failed", want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, InferResponseErrorStatusCode(tt.code, tt.errorType, tt.message))
		})
	}
}
