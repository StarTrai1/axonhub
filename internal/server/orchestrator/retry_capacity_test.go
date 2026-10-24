package orchestrator

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/streams"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
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

func TestModelCapacityRetriesWithBackoffWithinPolicy(t *testing.T) {
	outbound := newCodexResponsesPassThroughOutbound()
	outbound.state.CurrentCandidate.Channel.Settings.MaxSingleChannelRetries = lo.ToPtr(5)
	limit, overridden := outbound.SameChannelRetryLimit()
	require.True(t, overridden)
	require.Equal(t, 5, limit)
	channelID := outbound.state.CurrentCandidate.Channel.ID
	outbound.state.responsesRejectedStatusRetryChannel = channelID
	err := &llm.ResponseError{StatusCode: http.StatusInternalServerError, Detail: llm.ErrorDetail{Message: "model capacity reached"}}
	require.True(t, outbound.CanRetry(err))
	require.True(t, canRetryLocalCompactionStream(outbound, err))
	require.True(t, isRetryableError(err), "an independent channel remains eligible")
	delay := outbound.SameChannelRetryDelay(err, 1)
	require.GreaterOrEqual(t, delay, 2*time.Second)
	require.LessOrEqual(t, delay, 2500*time.Millisecond)
	first := outbound.state.CurrentCandidate
	outbound.state.ChannelModelsCandidates = []*ChannelModelsCandidate{first, {Channel: &biz.Channel{Channel: &ent.Channel{ID: 2}}}}
	require.False(t, outbound.CanRetry(err), "prefer an available alternative")
}

func TestModelCapacityHonorsProviderDelayAndHardStops(t *testing.T) {
	outbound := newCodexResponsesPassThroughOutbound()
	outbound.state.responsesRejectedStatusRetryChannel = outbound.GetCurrentChannel().ID
	for _, test := range []struct {
		name    string
		status  int
		body    string
		wait    string
		allowed bool
	}{
		{"relay overload", 500, `{"error":{"message":"当前模型 gpt-6.1-sol 负载已经达到上限，请稍后重试"}}`, "4", true},
		{"503 overload", 503, `{"error":{"message":"model is overloaded"}}`, "4", true},
		{"long wait", 500, `{"error":{"message":"model capacity exhausted"}}`, "3600", false},
		{"quota", 500, `{"error":{"code":"insufficient_quota","message":"model capacity exhausted"}}`, "", false},
		{"provisioned throughput", 429, `{"error":{"message":"throughput limit on your provisioned-managed deployment"}}`, "1", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw := &httpclient.Error{StatusCode: test.status, Headers: http.Header{"Retry-After": {test.wait}}, Body: []byte(test.body)}
			require.Equal(t, test.allowed, outbound.CanRetry(raw))
			require.Equal(t, test.allowed, canRetryLocalCompactionStream(outbound, raw))
			if test.allowed {
				delay := outbound.SameChannelRetryDelay(raw, 1)
				require.GreaterOrEqual(t, delay, 4*time.Second)
				require.LessOrEqual(t, delay, 4500*time.Millisecond)
			}
		})
	}
}

func TestModelCapacityPipelineActuallyRetriesAndHonorsBudget(t *testing.T) {
	for _, test := range []struct {
		name       string
		retries    int
		failures   int
		wantCalls  int
		wantResult bool
	}{
		{"overload then success", 1, 1, 2, true},
		{"budget exhausted", 1, 2, 2, false},
		{"retries disabled", 0, 1, 1, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider, err := responses.NewOutboundTransformer("https://example.invalid", "synthetic")
			require.NoError(t, err)
			candidate := &ChannelModelsCandidate{
				Channel: &biz.Channel{Channel: &ent.Channel{ID: 29}, Outbound: provider},
				APIFormat: llm.APIFormatOpenAIResponse.String(),
				Models: []biz.ChannelModelEntry{{RequestModel: "gpt-6.1-sol", ActualModel: "gpt-6.1-sol"}},
			}
			inbound, outbound := NewPersistentTransformers(&PersistenceState{ChannelModelsCandidates: []*ChannelModelsCandidate{candidate}}, responses.NewInboundTransformer())
			executor := &localCompactionRetryExecutor{events: []*httpclient.StreamEvent{{
				Type: "response.completed", Data: []byte(`{"type":"response.completed","response":{"id":"resp_capacity","model":"gpt-6.1-sol","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"OK"}]}]}}`),
			}}}
			for range test.failures {
				executor.failures = append(executor.failures, &httpclient.Error{StatusCode: 500, Body: []byte(`{"error":{"message":"当前模型 gpt-6.1-sol 负载已经达到上限，请稍后重试"}}`)})
			}
			processor := pipeline.NewFactory(executor).Pipeline(inbound, outbound, pipeline.WithRetry(0, test.retries, 0))
			body := []byte(`{"model":"gpt-6.1-sol","stream":true,"input":"Reply OK"}`)
			result, err := processor.Process(t.Context(), &httpclient.Request{Body: body, Headers: make(http.Header)})
			require.Len(t, executor.bodies, test.wantCalls)
			if test.wantResult {
				require.NoError(t, err)
				_, err = streams.All(result.ChatCompletionStream)
				require.NoError(t, err)
				require.NoError(t, result.ChatCompletionStream.Close())
				require.Equal(t, executor.bodies[0], executor.bodies[1])
			} else {
				require.Error(t, err)
				require.Nil(t, result)
			}
		})
	}
}

func TestModelCapacityCompactionBackoffCanBeCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	outbound := newCodexResponsesPassThroughOutbound()
	outbound.state.RetryPolicyProvider = &mockRetryPolicyProvider{policy: &biz.RetryPolicy{Enabled: true, MaxSingleChannelRetries: 10}}
	executor := &localCompactionRetryExecutor{failure: &httpclient.Error{StatusCode: 500, Body: []byte(`{"error":{"message":"model capacity exhausted"}}`)}}
	var timer *time.Timer
	executor.onRequest = func() { timer = time.AfterFunc(10*time.Millisecond, cancel) }
	t.Cleanup(func() {
		if timer != nil {
			timer.Stop()
		}
	})
	stream, _, err := newRemoteCompactionAdapter(nil, nil, nil).startLocalCompactionStream(ctx, outbound, outbound.state.RawProviderRequest, executor)
	require.Error(t, err)
	require.Nil(t, stream)
	require.Len(t, executor.bodies, 1)
}
