package orchestrator

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
	"github.com/looplj/axonhub/llm/transformer/gemini"
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
		{name: "credit balance", body: `{"error":{"code":"credit_balance_exhausted"}}`},
		{name: "organization spend", body: `{"error":{"code":"organization_spend_limit_exceeded"}}`},
		{name: "project spend", body: `{"error":{"code":"project_spend_limit_exceeded"}}`},
		{name: "organization usage", body: `{"error":{"code":"organization_usage_limit_exceeded"}}`},
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

func TestTransientOverloadHonorsProviderDelay(t *testing.T) {
	candidate := &ChannelModelsCandidate{Channel: &biz.Channel{Channel: &ent.Channel{ID: 29}}, TraceSticky: true}
	outbound := &PersistentOutboundTransformer{state: &PersistenceState{CurrentCandidate: candidate, ChannelModelsCandidates: []*ChannelModelsCandidate{candidate}}}
	failure := &httpclient.Error{StatusCode: http.StatusServiceUnavailable, Headers: http.Header{"Retry-After": {"15"}}, Body: []byte(`{"error":{"code":"server_is_overloaded"}}`)}
	require.True(t, outbound.CanRetry(failure))
	delay := outbound.SameChannelRetryDelay(failure, 1)
	require.GreaterOrEqual(t, delay, 15*time.Second)
	require.LessOrEqual(t, delay, 15*time.Second+500*time.Millisecond)
	failure.Headers.Set("Retry-After", "120")
	require.False(t, outbound.CanRetry(failure))
}

func TestWebSocketOverloadHeadersReachPersistentRetryPolicy(t *testing.T) {
	for _, event := range []string{
		`{"type":"error","status_code":429,"error":{"code":"slow_down","message":"temporarily busy"},"headers":{"retry-after":12}}`,
		`{"type":"error","status_code":503,"error":{"code":"server_is_overloaded","message":"temporarily busy"},"headers":{"retry-after":"12"}}`,
		`{"type":"error","error":{"status":429,"code":"opaque_provider_error","message":"temporarily busy"},"headers":{"retry-after":12}}`,
		`{"type":"error","error":{"status_code":"503","code":"opaque_provider_error","message":"temporarily busy"},"headers":{"retry-after":"12"}}`,
	} {
		wrapped, err := responses.NewOutboundTransformer("https://api.openai.com", "test-key")
		require.NoError(t, err)
		stream, err := wrapped.TransformStream(t.Context(), &httpclient.Request{}, streams.SliceStream([]*httpclient.StreamEvent{{Type: "error", Data: []byte(event)}}))
		require.NoError(t, err)
		_, streamErr := streams.All(stream)
		require.Error(t, streamErr)
		candidate := &ChannelModelsCandidate{Channel: &biz.Channel{Channel: &ent.Channel{ID: 29}}, TraceSticky: true}
		outbound := &PersistentOutboundTransformer{state: &PersistenceState{CurrentCandidate: candidate, ChannelModelsCandidates: []*ChannelModelsCandidate{candidate}}}
		require.True(t, outbound.CanRetry(streamErr))
		delay := outbound.SameChannelRetryDelay(streamErr, 1)
		require.GreaterOrEqual(t, delay, 12*time.Second)
		require.LessOrEqual(t, delay, 12*time.Second+500*time.Millisecond)
		outbound.state.ChannelModelsCandidates = append(outbound.state.ChannelModelsCandidates, &ChannelModelsCandidate{Channel: &biz.Channel{Channel: &ent.Channel{ID: 30}}})
		require.False(t, outbound.CanRetry(streamErr))
		require.NoError(t, stream.Close())
	}
}

func TestGoogleRetryInfoReachesRetryPolicyAndChannelCooldown(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		delay      string
		retryAfter string
		want       time.Duration
		allowed    bool
	}{
		{"short capacity window", "39s", "", 39 * time.Second, true},
		{"fractional window", "1.5s", "", 1500 * time.Millisecond, true},
		{"long window does not block retry loop", "90s", "", 90 * time.Second, false},
		{"cooldown remains bounded", "86400s", "", httpclient.MaxRetryAfterDuration, false},
		{"header precedence", "90s", "10", 10 * time.Second, true},
		{"invalid advice", "later", "", 0, true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			wrapped, err := gemini.NewOutboundTransformer("https://example.com", "test-key")
			require.NoError(t, err)
			candidate := &ChannelModelsCandidate{Channel: &biz.Channel{Channel: &ent.Channel{ID: 29}}}
			outbound := &PersistentOutboundTransformer{
				wrapped: wrapped,
				state: &PersistenceState{
					CurrentCandidate:        candidate,
					ChannelModelsCandidates: []*ChannelModelsCandidate{candidate},
				},
			}
			body := fmt.Sprintf(`{"error":{"code":429,"status":"RESOURCE_EXHAUSTED","message":"temporarily busy","details":[{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":%q}]}}`, testCase.delay)
			headers := make(http.Header)
			if testCase.retryAfter != "" {
				headers.Set("Retry-After", testCase.retryAfter)
			}
			failure := outbound.TransformError(t.Context(), &httpclient.Error{
				StatusCode: http.StatusTooManyRequests, Headers: headers, Body: []byte(body),
			})
			require.Equal(t, testCase.allowed, outbound.CanRetry(failure))
			if testCase.allowed {
				delay := outbound.SameChannelRetryDelay(failure, 1)
				minimum := max(2*time.Second, testCase.want)
				require.GreaterOrEqual(t, delay, minimum)
				require.LessOrEqual(t, delay, minimum+500*time.Millisecond)
			}
			tracker := NewChannelRequestTracker()
			middleware := &rateLimitTracking{outbound: outbound, tracker: tracker}
			before := time.Now()
			middleware.OnOutboundRawError(t.Context(), failure)
			until, ok := tracker.GetCooldownUntil(candidate.Channel.ID)
			require.Equal(t, testCase.want > 0, ok)
			if ok {
				require.WithinDuration(t, before.Add(testCase.want), until, time.Second)
			}
		})
	}
}
