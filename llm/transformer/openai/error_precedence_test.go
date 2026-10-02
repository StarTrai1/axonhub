package openai

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
)

func TestTransformError_PrefersDetailAndRetainsCauseHeaders(t *testing.T) {
	raw := &httpclient.Error{
		StatusCode: http.StatusTooManyRequests,
		Headers: http.Header{
			"Retry-After":       []string{"30"},
			"Content-Type":      []string{"text/plain"},
			"Content-Length":    []string{"99"},
			"Content-Encoding":  []string{"gzip"},
			"Transfer-Encoding": []string{"chunked"},
		},
		Body: []byte("original upstream identity"),
	}
	originalHeaders := raw.Headers.Clone()
	normalized := &llm.ResponseError{
		StatusCode: http.StatusTooManyRequests,
		Detail:     llm.ErrorDetail{Message: "sanitized message", Type: "RESOURCE_EXHAUSTED", Code: "429"},
		Cause:      fmt.Errorf("upstream: %w", raw),
	}
	adapter := NewInboundTransformer()
	result := adapter.TransformError(t.Context(), fmt.Errorf("pipeline: %w", normalized))
	require.Equal(t, http.StatusTooManyRequests, result.StatusCode)
	require.JSONEq(t, `{"error":{"message":"sanitized message","type":"RESOURCE_EXHAUSTED","code":"429"}}`, string(result.Body))
	require.Equal(t, "30", result.Headers.Get("Retry-After"))
	require.Equal(t, "application/json", result.Headers.Get("Content-Type"))
	require.Empty(t, result.Headers.Get("Content-Length"))
	require.Empty(t, result.Headers.Get("Content-Encoding"))
	require.Empty(t, result.Headers.Get("Transfer-Encoding"))
	require.Equal(t, originalHeaders, raw.Headers)
	require.Equal(t, "original upstream identity", string(raw.Body))

	// A bare HTTP error still follows the existing raw pass-through contract.
	require.Same(t, raw, adapter.TransformError(t.Context(), fmt.Errorf("pipeline: %w", raw)))
}
