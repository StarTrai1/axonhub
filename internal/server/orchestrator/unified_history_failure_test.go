package orchestrator

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
)

type unifiedFailingExecutor struct{ calls atomic.Int32 }

func (*unifiedFailingExecutor) Do(context.Context, *httpclient.Request) (*httpclient.Response, error) {
	return nil, errors.New("unexpected nonstream request")
}

func (e *unifiedFailingExecutor) DoStream(ctx context.Context, _ *httpclient.Request) (streams.Stream[*httpclient.StreamEvent], error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	e.calls.Add(1)
	return streams.SliceStream([]*httpclient.StreamEvent{{Data: []byte(`{"type":"response.created","response":{"id":"resp_failed_summary","status":"in_progress","output":[]}}`)}, {Data: []byte(`{"type":"error","error":{"code":"server_error","message":"synthetic summary upstream failed"}}`)}}), nil
}

func TestUnifiedHistoryFailedPlanDoesNotRegenerateSiblingsOnResend(t *testing.T) {
	ctx, _, state, adapter := unifiedHistoryFixture(t)
	executor := new(unifiedFailingExecutor)
	body := unifiedLongToolHistory(t, 24)
	_, _, err := adapter.compileUnifiedContext(ctx, body, state, executor)
	require.ErrorContains(t, err, "synthetic summary upstream failed")
	first := executor.calls.Load()
	require.Positive(t, first)
	require.LessOrEqual(t, first, int32(4))
	for range 12 {
		_, _, err = adapter.compileUnifiedContext(ctx, body, state, executor)
		var failure *llm.ResponseError
		require.ErrorAs(t, err, &failure)
		require.Equal(t, "unified_history_preparation_cooldown", failure.Detail.Code)
	}
	require.Equal(t, first, executor.calls.Load())
	// A different credential is a different destination and is immediately eligible.
	state.CurrentCandidate.Channel.Credentials.APIKey = "changed-synthetic-key"
	_, _, err = adapter.compileUnifiedContext(ctx, body, state, executor)
	require.ErrorContains(t, err, "synthetic summary upstream failed")
	require.Greater(t, executor.calls.Load(), first)
}

func TestUnifiedHistorySummaryFailureExpiresAndIgnoresCancellation(t *testing.T) {
	adapter := newRemoteCompactionAdapter(nil, nil, nil)
	key := unifiedSummaryFailureKey{owner: "owner-1-segment"}
	adapter.rememberUnifiedSummaryFailure(t.Context(), key, errors.New("provider failed"))
	require.Error(t, adapter.unifiedSummaryCooldown(key, time.Now()))
	require.NoError(t, adapter.unifiedSummaryCooldown(key, time.Now().Add(unifiedSummaryFailureTTL)))
	for _, err := range []error{context.Canceled, context.DeadlineExceeded} {
		adapter.rememberUnifiedSummaryFailure(t.Context(), key, err)
		require.NoError(t, adapter.unifiedSummaryCooldown(key, time.Now()))
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	adapter.rememberUnifiedSummaryFailure(ctx, key, errors.New("sibling failed"))
	require.NoError(t, adapter.unifiedSummaryCooldown(key, time.Now()))
}

func TestUnifiedHistorySummaryFailureSeparatesSelectedPoolCredentials(t *testing.T) {
	ctx, _, state, adapter := unifiedHistoryFixture(t)
	segment := &unifiedHistorySegment{key: "summary:synthetic-source"}
	state.RawProviderRequest = &httpclient.Request{URL: "https://example.invalid/v1/responses", Headers: http.Header{"Authorization": {"Bearer first-synthetic-key"}}}
	first := unifiedSegmentFailureKey(state, segment)
	adapter.rememberUnifiedSummaryFailure(ctx, first, errors.New("first key unavailable"))
	state.RawProviderRequest.Headers.Set("Authorization", "Bearer second-synthetic-key")
	second := unifiedSegmentFailureKey(state, segment)
	require.NotEqual(t, first, second)
	require.Error(t, adapter.unifiedSummaryCooldown(first, time.Now()))
	require.NoError(t, adapter.unifiedSummaryCooldown(second, time.Now()))
}
