package orchestrator

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

func TestTransientRateLimitPreservesHeadersAcrossErrorTransformation(t *testing.T) {
	wrapped, err := responses.NewOutboundTransformer("https://api.openai.com", "test-key")
	require.NoError(t, err)
	outbound := &PersistentOutboundTransformer{wrapped: wrapped}
	raw := &httpclient.Error{StatusCode: 429, Headers: http.Header{"Retry-After": {"12"}}, Body: []byte(`{"error":{"code":"rate_limit_exceeded","message":"rate limit"}}`)}
	transformed := outbound.TransformError(context.Background(), raw)
	require.NotNil(t, transformed)
	delay, found := transientRateLimitRetryAfter(transformed, time.Now())
	require.True(t, found)
	require.Equal(t, 12*time.Second, delay)
	now := time.Now().UTC().Truncate(time.Second)
	raw.Headers.Set("Retry-After", now.Add(20*time.Second).Format(http.TimeFormat))
	delay, found = transientRateLimitRetryAfter(transformed, now)
	require.True(t, found)
	require.Equal(t, 20*time.Second, delay)
	raw.Headers = nil
	for attempt := 1; attempt <= 5; attempt++ {
		delay = outbound.SameChannelRetryDelay(transformed, attempt)
		minimum := time.Duration(1<<attempt) * time.Second
		require.GreaterOrEqual(t, delay, minimum)
		require.LessOrEqual(t, delay, minimum+500*time.Millisecond)
	}
}

func TestTransientRateLimitRetriesStickyLastChannelButPrefersAlternative(t *testing.T) {
	first := &ChannelModelsCandidate{Channel: &biz.Channel{Channel: &ent.Channel{ID: 29}}, TraceSticky: true}
	state := &PersistenceState{CurrentCandidate: first, ChannelModelsCandidates: []*ChannelModelsCandidate{first}}
	outbound := &PersistentOutboundTransformer{state: state}
	providerErr := &llm.ResponseError{StatusCode: 429, Detail: llm.ErrorDetail{Code: "rate_limit_exceeded", Type: "too_many_requests", Message: "eastus rate limit"}}
	require.True(t, outbound.CanRetry(providerErr))
	state.ChannelModelsCandidates = append(state.ChannelModelsCandidates, &ChannelModelsCandidate{Channel: &biz.Channel{Channel: &ent.Channel{ID: 30}}})
	require.False(t, outbound.CanRetry(providerErr))
	state.ChannelModelsCandidates = state.ChannelModelsCandidates[:1]
	providerErr.Detail.Code = "insufficient_quota"
	require.False(t, outbound.CanRetry(providerErr))
}

func TestTransientRateLimitWaitHonorsProviderAndRejectsLongQuotaWindows(t *testing.T) {
	for _, testcase := range []struct {
		name    string
		headers http.Header
		body    string
		allowed bool
		minimum time.Duration
	}{
		{name: "no_header", allowed: true, minimum: 2 * time.Second},
		{name: "seconds", headers: http.Header{"Retry-After": {"10"}}, allowed: true, minimum: 10 * time.Second},
		{name: "milliseconds", headers: http.Header{"Retry-After-Ms": {"3000"}}, allowed: true, minimum: 3 * time.Second},
		{name: "azure", headers: http.Header{"X-Ms-Retry-After-Ms": {"4000"}}, allowed: true, minimum: 4 * time.Second},
		{name: "long_reset", headers: http.Header{"Retry-After": {"3600"}}},
		{name: "overflow", headers: http.Header{"Retry-After": {"1e100"}}},
		{name: "quota", body: `{"error":{"type":"usage_limit_reached"}}`},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			providerErr := &httpclient.Error{StatusCode: 429, Headers: testcase.headers, Body: []byte(testcase.body)}
			require.Equal(t, testcase.allowed, canRetryTransientRateLimit(providerErr))
			if testcase.allowed {
				outbound := new(PersistentOutboundTransformer)
				delay := outbound.SameChannelRetryDelay(providerErr, 1)
				require.GreaterOrEqual(t, delay, testcase.minimum)
				require.LessOrEqual(t, delay, testcase.minimum+500*time.Millisecond)
			}
		})
	}
}
