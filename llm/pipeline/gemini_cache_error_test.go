package pipeline

import (
	"context"
	"testing"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/stretchr/testify/require"
)

func TestMissingGeminiCacheStopsAllRetries(t *testing.T) {
	calls, retries, switches := 0, 0, 0
	upstream := &httpclient.Error{StatusCode: 403, Body: []byte(`{"error":{"message":"CachedContent not found (or permission denied)"}}`)}
	outbound := &mockOutbound{
		transformError: func(context.Context, *httpclient.Error) *llm.ResponseError {
			return &llm.ResponseError{StatusCode: 403, Detail: llm.ErrorDetail{Message: "CachedContent not found (or permission denied)"}}
		},
		transformRequest: func(context.Context, *llm.Request) (*httpclient.Request, error) { return &httpclient.Request{}, nil },
		canRetry:         func(error) bool { return true },
		prepareForRetry:  func(context.Context) error { retries++; return nil },
		hasMoreChannels:  func() bool { return true },
		nextChannel:      func(context.Context) error { switches++; return nil },
	}
	executor := &mockExecutor{do: func(context.Context, *httpclient.Request) (*httpclient.Response, error) {
		calls++
		return nil, upstream
	}}
	_, err := NewFactory(executor).Pipeline(&mockInbound{}, outbound, WithRetry(2, 2, 0)).Process(t.Context(), &httpclient.Request{})
	require.True(t, isMissingGeminiCachedContent(err))
	require.Equal(t, 1, calls)
	require.Zero(t, retries)
	require.Zero(t, switches)
}
