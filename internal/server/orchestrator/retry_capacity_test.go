package orchestrator

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/samber/lo"

	"github.com/looplj/axonhub/llm"
)

func TestModelCapacityExhaustionIsNotAnOrdinaryServerRetry(t *testing.T) {
	for _, tt := range []struct {
		status   int
		message  string
		capacity bool
	}{
		{500, "当前模型 gpt-6.1-sol 负载已经达到上限，请稍后重试 (request id: synthetic)", true},
		{503, "The model is overloaded", true},
		{500, "model capacity reached", true},
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

func TestModelCapacityStopsCompatibilityRetry(t *testing.T) {
	outbound := newCodexResponsesPassThroughOutbound()
	outbound.state.CurrentCandidate.Channel.Settings.MaxSingleChannelRetries = lo.ToPtr(5)
	limit, overridden := outbound.SameChannelRetryLimit()
	require.True(t, overridden)
	require.Equal(t, 5, limit)
	channelID := outbound.state.CurrentCandidate.Channel.ID
	outbound.state.responsesRejectedStatusRetryChannel = channelID
	err := &llm.ResponseError{StatusCode: http.StatusInternalServerError, Detail: llm.ErrorDetail{Message: "model capacity reached"}}
	require.False(t, outbound.CanRetry(err))
	require.False(t, canRetryLocalCompactionStream(outbound, err))
	require.True(t, isRetryableError(err), "an independent channel remains eligible")
}
