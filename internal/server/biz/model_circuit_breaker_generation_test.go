package biz

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestModelCircuitBreakerStaleSuccessPreservesNewFailure(t *testing.T) {
	cb := NewModelCircuitBreaker()
	ctx := t.Context()
	first := cb.BeginAttempt(1, "gpt-6.1-sol")
	for range 5 {
		cb.RecordError(ctx, 1, "gpt-6.1-sol", false)
	}
	before := cb.GetModelCircuitBreakerStats(ctx, 1, "gpt-6.1-sol")
	cb.RecordAttemptSuccess(ctx, 1, "gpt-6.1-sol", first)
	after := cb.GetModelCircuitBreakerStats(ctx, 1, "gpt-6.1-sol")
	require.Equal(t, StateOpen, after.State)
	require.Equal(t, before.ConsecutiveFailures, after.ConsecutiveFailures)
	require.Equal(t, before.NextProbeAt, after.NextProbeAt)
	fresh := cb.BeginAttempt(1, "gpt-6.1-sol")
	cb.RecordError(ctx, 2, "gpt-6.1-sol", false)
	cb.RecordAttemptSuccess(ctx, 1, "gpt-6.1-sol", fresh)
	require.Equal(t, StateClosed, cb.GetModelCircuitBreakerStats(ctx, 1, "gpt-6.1-sol").State)
	require.Equal(t, 1, cb.GetModelCircuitBreakerStats(ctx, 2, "gpt-6.1-sol").ConsecutiveFailures)
}
