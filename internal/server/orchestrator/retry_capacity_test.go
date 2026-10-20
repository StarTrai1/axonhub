package orchestrator

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
)

func TestModelCapacityExhaustionIsNotAnOrdinaryServerRetry(t *testing.T) {
	for _, tt := range []struct {
		status int
		message string
		capacity bool
	}{
		{500, "当前模型 gpt-6.1-sol 负载已经达到上限，请稍后重试 (request id: synthetic)", true},
		{503, "The model is overloaded", true},
		{500, "internal server error", false},
		{502, "bad response status code 502", false},
		{400, "model capacity exhausted", false},
	} {
		err := &llm.ResponseError{StatusCode: tt.status, Detail: llm.ErrorDetail{Message: tt.message}}
		require.Equal(t, tt.capacity, isModelCapacityExhausted(err))
		if tt.status == http.StatusInternalServerError {
			require.True(t, isRetryableError(err), "cross-channel failover remains available")
		}
	}
}
