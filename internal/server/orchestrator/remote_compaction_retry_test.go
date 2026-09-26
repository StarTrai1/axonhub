package orchestrator

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

func TestResponsesRejectedMetadataLocalCompactionRetry(t *testing.T) {
	for index, scenario := range []struct {
		name       string
		enabled    bool
		maxRetries int
		errorBody  string
		wantCalls  int
	}{
		{"compatible retry", true, 1, `{"error":{"code":"unknown_parameter","param":"input[0].internal_chat_message_metadata_passthrough.content_item_kinds"}}`, 2},
		{"disabled retry", false, 3, `{"error":{"code":"unknown_parameter","param":"input[0].internal_chat_message_metadata_passthrough.content_item_kinds"}}`, 1},
		{"zero retry budget", true, 0, `{"error":{"code":"unknown_parameter","param":"input[0].internal_chat_message_metadata_passthrough.content_item_kinds"}}`, 1},
		{"unrelated validation error", true, 3, `{"error":{"code":"invalid_responses_request","param":"input"}}`, 1},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			outbound := newCodexResponsesPassThroughOutbound()
			outbound.state.CurrentCandidate.Channel.ID = 91000 + index
			outbound.state.RetryPolicyProvider = &mockRetryPolicyProvider{policy: &biz.RetryPolicy{
				Enabled: scenario.enabled, MaxSingleChannelRetries: scenario.maxRetries,
			}}
			providerRequest := &httpclient.Request{
				URL:       "https://example.invalid/v1/responses",
				APIFormat: string(llm.APIFormatOpenAIResponse),
				Headers:   make(http.Header),
				Body:      []byte(`{"model":"gpt-6-astra","input":[{"type":"message","role":"user","content":"preserve context","internal_chat_message_metadata_passthrough":{"content_item_kinds":[]}},{"type":"reasoning","id":"rs_native","encrypted_content":"keep-reasoning"},{"type":"compaction","id":"cmp_native","encrypted_content":"keep-compaction"},{"type":"function_call","id":"fc_native","call_id":"call_native","name":"exec","arguments":"{}"}]}`),
			}
			executor := &localCompactionRetryExecutor{failure: &httpclient.Error{
				StatusCode: http.StatusBadRequest, Body: []byte(scenario.errorBody),
			}}
			adapter := newRemoteCompactionAdapter(nil, nil, nil)
			stream, execution, err := adapter.startLocalCompactionStream(context.Background(), outbound, providerRequest, executor, applyResponsesRejectedStatusCompatibility(outbound))
			require.Nil(t, execution)
			require.Len(t, executor.bodies, scenario.wantCalls)
			if scenario.wantCalls == 1 {
				require.ErrorIs(t, err, executor.failure)
				require.Nil(t, stream)
				return
			}
			require.NoError(t, err)
			require.NoError(t, stream.Close())
			require.True(t, gjson.GetBytes(executor.bodies[0], "input.0.internal_chat_message_metadata_passthrough").Exists())
			require.False(t, gjson.GetBytes(executor.bodies[1], "input.0.internal_chat_message_metadata_passthrough").Exists())
			require.Equal(t, "preserve context", gjson.GetBytes(executor.bodies[1], "input.0.content").String())
			require.Equal(t, "keep-reasoning", gjson.GetBytes(executor.bodies[1], "input.1.encrypted_content").String())
			require.Equal(t, "keep-compaction", gjson.GetBytes(executor.bodies[1], "input.2.encrypted_content").String())
			require.Equal(t, "fc_native", gjson.GetBytes(executor.bodies[1], "input.3.id").String())
		})
	}
}

func TestResponsesRejectedMetadataLocalCompactionHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	outbound := newCodexResponsesPassThroughOutbound()
	executor := new(localCompactionRetryExecutor)
	adapter := newRemoteCompactionAdapter(nil, nil, nil)
	stream, execution, err := adapter.startLocalCompactionStream(ctx, outbound, outbound.state.RawProviderRequest, executor, applyResponsesRejectedStatusCompatibility(outbound))
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, stream)
	require.Nil(t, execution)
	require.Empty(t, executor.bodies)
}

type localCompactionRetryExecutor struct {
	bodies    [][]byte
	failure   error
	failures  []error
	events    []*httpclient.StreamEvent
	stream    streams.Stream[*httpclient.StreamEvent]
	onRequest func()
}

func (executor *localCompactionRetryExecutor) Do(context.Context, *httpclient.Request) (*httpclient.Response, error) {
	return nil, errors.New("unexpected non-streaming compaction request")
}

func (executor *localCompactionRetryExecutor) DoStream(_ context.Context, request *httpclient.Request) (streams.Stream[*httpclient.StreamEvent], error) {
	executor.bodies = append(executor.bodies, append([]byte(nil), request.Body...))
	if executor.onRequest != nil {
		executor.onRequest()
	}
	if len(executor.bodies) <= len(executor.failures) {
		return nil, executor.failures[len(executor.bodies)-1]
	}
	if len(executor.failures) == 0 && len(executor.bodies) == 1 && executor.failure != nil {
		return nil, executor.failure
	}
	if executor.stream != nil {
		return executor.stream, nil
	}
	return streams.SliceStream(executor.events), nil
}

func TestResponsesRejectedLocalCompactionRetriesTransientOpeningFailures(t *testing.T) {
	for _, scenario := range []struct {
		name       string
		failure    error
		failures   int
		retries    int
		disabled   bool
		wantCalls  int
		wantResult bool
	}{
		{"load error then success", bridgeOverloadError(), 1, 2, false, 2, true},
		{"load error exhausted", bridgeOverloadError(), 3, 2, false, 3, false},
		{"zero retry budget", bridgeOverloadError(), 1, 0, false, 1, false},
		{"disabled retries", bridgeOverloadError(), 1, 2, true, 1, false},
		{"opening EOF", io.ErrUnexpectedEOF, 1, 2, false, 2, true},
		{"ordinary validation", &httpclient.Error{StatusCode: 400, Body: []byte(`{"error":{"message":"invalid input"}}`)}, 1, 2, false, 1, false},
		{"authorization rejection", &httpclient.Error{StatusCode: 401}, 1, 2, false, 1, false},
		{"budget exhausted", &httpclient.Error{StatusCode: 402, Body: []byte(`{"error":{"message":"Budget pool quota has been exhausted"}}`)}, 1, 2, false, 1, false},
		{"permanent rate limit", &httpclient.Error{StatusCode: 429, Body: []byte(`{"error":{"code":"insufficient_quota","message":"quota exhausted"}}`)}, 1, 2, false, 1, false},
		{"long rate limit cooldown", &httpclient.Error{StatusCode: 503, Headers: http.Header{"Retry-After": {"120"}}}, 1, 2, false, 1, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			outbound := newCodexResponsesPassThroughOutbound()
			outbound.wrapped = new(responses.OutboundTransformer)
			outbound.state.RetryPolicyProvider = &mockRetryPolicyProvider{policy: &biz.RetryPolicy{
				Enabled: !scenario.disabled, MaxSingleChannelRetries: scenario.retries,
			}}
			providerRequest := outbound.state.RawProviderRequest
			providerRequest.Body = []byte(responsesResourceHistoryFixture)
			executor := new(localCompactionRetryExecutor)
			for range scenario.failures {
				executor.failures = append(executor.failures, scenario.failure)
			}
			adapter := newRemoteCompactionAdapter(nil, nil, nil)
			stream, _, err := adapter.startLocalCompactionStream(t.Context(), outbound, providerRequest, executor, applyResponsesRejectedStatusCompatibility(outbound))
			require.Len(t, executor.bodies, scenario.wantCalls)
			for _, body := range executor.bodies {
				require.Equal(t, []byte(responsesResourceHistoryFixture), body, "transient failures must not change conversation history")
			}
			if scenario.wantResult {
				require.NoError(t, err)
				require.NotNil(t, stream)
				require.NoError(t, stream.Close())
			} else {
				require.ErrorIs(t, err, scenario.failure)
				require.Nil(t, stream)
				if ExtractStatusCodeFromError(scenario.failure) == 500 {
					var responseErr *llm.ResponseError
					require.ErrorAs(t, err, &responseErr)
					require.Equal(t, "model capacity reached", responseErr.Detail.Message)
					require.Equal(t, http.StatusInternalServerError, responseErr.StatusCode)
				}
			}
		})
	}
}

func TestResponsesRejectedLocalCompactionTransientAndCompatibilityShareBudget(t *testing.T) {
	outbound := newCodexResponsesPassThroughOutbound()
	outbound.state.CurrentCandidate.Channel.ID = 94050
	outbound.state.RetryPolicyProvider = &mockRetryPolicyProvider{policy: &biz.RetryPolicy{
		Enabled: true, MaxSingleChannelRetries: 2,
	}}
	providerRequest := outbound.state.RawProviderRequest
	providerRequest.Body = []byte(responsesResourceHistoryFixture)
	executor := &localCompactionRetryExecutor{failures: []error{bridgeOverloadError(), resourceMismatchHTTPError()}}
	adapter := newRemoteCompactionAdapter(nil, nil, nil)
	stream, _, err := adapter.startLocalCompactionStream(t.Context(), outbound, providerRequest, executor, applyResponsesRejectedStatusCompatibility(outbound))
	require.NoError(t, err)
	require.NoError(t, stream.Close())
	require.Len(t, executor.bodies, 3)
	require.Equal(t, executor.bodies[0], executor.bodies[1])
	assertPortableIDsOnlyRemoved(t, executor.bodies[1], executor.bodies[2])
}

func TestResponsesRejectedLocalCompactionRetryWaitCanBeCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	outbound := newCodexResponsesPassThroughOutbound()
	outbound.state.RetryPolicyProvider = &mockRetryPolicyProvider{policy: &biz.RetryPolicy{
		Enabled: true, MaxSingleChannelRetries: 2, RetryDelayMs: 300000,
	}}
	executor := &localCompactionRetryExecutor{failure: bridgeOverloadError()}
	var timer *time.Timer
	executor.onRequest = func() { timer = time.AfterFunc(10*time.Millisecond, cancel) }
	t.Cleanup(func() {
		if timer != nil {
			timer.Stop()
		}
	})
	adapter := newRemoteCompactionAdapter(nil, nil, nil)
	stream, _, err := adapter.startLocalCompactionStream(ctx, outbound, outbound.state.RawProviderRequest, executor, applyResponsesRejectedStatusCompatibility(outbound))
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, stream)
	require.Len(t, executor.bodies, 1)
}

func TestResponsesRejectedLocalCompactionDoesNotRetryPartialStream(t *testing.T) {
	outbound := newCodexResponsesPassThroughOutbound()
	outbound.state.RetryPolicyProvider = &mockRetryPolicyProvider{policy: &biz.RetryPolicy{Enabled: true, MaxSingleChannelRetries: 2}}
	executor := &localCompactionRetryExecutor{stream: &mockStream{
		events: []*httpclient.StreamEvent{{Type: "response.output_text.delta", Data: []byte(`{"type":"response.output_text.delta","delta":"partial summary"}`)}},
		err:    io.ErrUnexpectedEOF,
	}}
	adapter := newRemoteCompactionAdapter(nil, nil, nil)
	stream, _, err := adapter.startLocalCompactionStream(t.Context(), outbound, outbound.state.RawProviderRequest, executor, applyResponsesRejectedStatusCompatibility(outbound))
	require.NoError(t, err)
	chunks, err := collectLocalCompactionBridgeStream(stream, nil)
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	require.Nil(t, chunks)
	require.NoError(t, stream.Close())
	require.Len(t, executor.bodies, 1)
}

func bridgeOverloadError() *httpclient.Error {
	return &httpclient.Error{StatusCode: http.StatusInternalServerError, Body: []byte(`{"error":{"type":"server_error","code":"model_overloaded","message":"model capacity reached"}}`)}
}

func TestResponsesRejectedReasoningLocalCompactionRemembersSuccessfulRecovery(t *testing.T) {
	ctx, outbound, middleware, scope := responsesReasoningRecoveryFixture(t)
	original := *outbound.state.RawProviderRequest
	outbound.state.RetryPolicyProvider = &mockRetryPolicyProvider{policy: &biz.RetryPolicy{
		Enabled: true, MaxSingleChannelRetries: 1,
	}}
	executor := &localCompactionRetryExecutor{
		failure: &httpclient.Error{StatusCode: http.StatusBadRequest, Body: []byte(`{"error":{"code":"invalid_encrypted_content"}}`)},
		events: []*httpclient.StreamEvent{{
			Type: "response.completed",
			Data: []byte(`{"type":"response.completed","response":{"status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"summary"}]}]}}`),
		}},
	}
	adapter := newRemoteCompactionAdapter(nil, nil, nil)
	stream, _, err := adapter.startLocalCompactionStream(ctx, outbound, outbound.state.RawProviderRequest, executor, middleware)
	require.NoError(t, err)
	require.Len(t, executor.bodies, 2)
	_, found := rememberedResponsesReasoningRule(scope, original.Body)
	require.False(t, found, "opening the stream alone must not confirm a recovery")
	for stream.Next() {
		_ = stream.Current()
	}
	require.NoError(t, stream.Err())
	require.NoError(t, stream.Close())
	_, found = rememberedResponsesReasoningRule(scope, original.Body)
	require.True(t, found)
}

func TestResponsesRejectedReasoningLocalCompactionSharesRetryBudget(test *testing.T) {
	for index, scenario := range []struct {
		name       string
		enabled    bool
		maxRetries int
		wantCalls  int
	}{
		{"metadata then reasoning recovery", true, 2, 3},
		{"one shared retry budget", true, 1, 2},
		{"zero retry budget", true, 0, 1},
		{"disabled retries", false, 2, 1},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			outbound := newCodexResponsesPassThroughOutbound()
			outbound.state.CurrentCandidate.Channel.ID = 94000 + index
			outbound.state.RetryPolicyProvider = &mockRetryPolicyProvider{policy: &biz.RetryPolicy{
				Enabled: scenario.enabled, MaxSingleChannelRetries: scenario.maxRetries,
			}}
			providerRequest := outbound.state.RawProviderRequest
			providerRequest.Body = []byte(responsesRejectedReasoningFixture)
			executor := &localCompactionRetryExecutor{failures: []error{
				&httpclient.Error{StatusCode: http.StatusBadRequest, Body: []byte(`{"error":{"code":"unknown_parameter","param":"input[0].internal_chat_message_metadata_passthrough.content_item_kinds"}}`)},
				&httpclient.Error{StatusCode: http.StatusBadRequest, Body: []byte(`{"error":{"code":"invalid_encrypted_content"}}`)},
			}}
			adapter := newRemoteCompactionAdapter(nil, nil, nil)
			stream, execution, err := adapter.startLocalCompactionStream(test.Context(), outbound, providerRequest, executor, applyResponsesRejectedStatusCompatibility(outbound))
			require.Nil(test, execution)
			require.Len(test, executor.bodies, scenario.wantCalls)
			if scenario.wantCalls < 3 {
				require.ErrorIs(test, err, executor.failures[scenario.wantCalls-1])
				require.Nil(test, stream)
				return
			}
			require.NoError(test, err)
			require.NoError(test, stream.Close())
			require.True(test, gjson.GetBytes(executor.bodies[0], "input.0.internal_chat_message_metadata_passthrough").Exists())
			require.False(test, gjson.GetBytes(executor.bodies[1], "input.0.internal_chat_message_metadata_passthrough").Exists())
			require.Equal(test, "rejected-with-summary", gjson.GetBytes(executor.bodies[1], "input.1.encrypted_content").String())
			require.Equal(test, "visible summary\n\nvisible rationale", gjson.GetBytes(executor.bodies[2], "input.1.content.0.text").String())
			require.False(test, gjson.GetBytes(executor.bodies[2], "input.2.id").Exists())
			require.Equal(test, "call_function", gjson.GetBytes(executor.bodies[2], "input.2.call_id").String())
			require.Equal(test, "keep function output", gjson.GetBytes(executor.bodies[2], "input.3.output").String())
			require.False(test, gjson.GetBytes(executor.bodies[2], "input.4.id").Exists())
			require.Equal(test, "call_custom", gjson.GetBytes(executor.bodies[2], "input.4.call_id").String())
			require.Empty(test, gjson.GetBytes(executor.bodies[2], "input.#(encrypted_content)#").Array())
		})
	}
}
