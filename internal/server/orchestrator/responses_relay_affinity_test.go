package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

func relayAffinityFailure(status int, message, route string) *httpclient.Error {
	body, _ := json.Marshal(map[string]any{"error": map[string]string{"type": "server_error", "message": message}})
	return &httpclient.Error{StatusCode: status, Body: body, Headers: http.Header{"X-New-Api-Routed-Channel-Id": {route}}}
}

func relayAffinityOverload() *httpclient.Error {
	return relayAffinityFailure(http.StatusInternalServerError,
		"当前模型 gpt-6-astra 负载已经达到上限，请稍后重试 (request id: synthetic-overload)", "(11,11),(22,22)")
}

const relayProvisionedThroughputMessage = "Requests have exceeded the throughput limit on your Provisioned-Managed deployment. If you continue to exceed your limit, consider increasing the number of provisioned throughput units deployed."

func relayAffinityRequest(t *testing.T) *httpclient.Request {
	t.Helper()
	request := rejectedReasoningPipelineRequest(t, llm.APIFormatOpenAIResponse)
	request.Body = []byte(responsesResourceHistoryFixture)
	thread := request.Headers.Get("Thread-Id")
	metadata := map[string]string{
		"thread_id": thread, "session_id": "shared-session", "turn_id": "original-turn", "root_turn_id": "original-root",
		"x-codex-window-id": thread + ":4", "x-codex-installation-id": "original-installation",
	}
	turn, err := json.Marshal(map[string]any{"thread_id": thread, "session_id": "shared-session", "window_id": thread + ":4", "turn_id": "original-turn", "root_turn_id": "original-root", "window_number": 4})
	require.NoError(t, err)
	metadata["x-codex-turn-metadata"] = string(turn)
	request.Body, err = sjson.SetBytes(request.Body, "client_metadata", metadata)
	require.NoError(t, err)
	request.Headers.Set("X-Codex-Turn-Metadata", string(turn))
	request.Headers.Set("X-Codex-Window-Id", thread+":4")
	request.Headers.Set("Conversation_id", thread)
	request.Headers.Set("X-Codex-Turn-State", "opaque-old-turn")
	return request
}

func TestResponsesCapacityDoesNotRotateOrResend(t *testing.T) {
	for _, raw := range []bool{false, true} {
		for _, failure := range []*httpclient.Error{
			relayAffinityOverload(),
			relayAffinityFailure(429, relayProvisionedThroughputMessage, "(11,11),(22,22)"),
			relayAffinityFailure(500, "We're currently experiencing high demand, which may cause temporary errors", "(11,11),(22,22)"),
		} {
			for _, retries := range []int{0, 1, 5} {
				t.Run(fmt.Sprintf("raw=%t/status=%d/retries=%d", raw, failure.StatusCode, retries), func(t *testing.T) {
					request := relayAffinityRequest(t)
					original, headers := append([]byte(nil), request.Body...), request.Headers.Clone()
					executor := &responsesReasoningPipelineExecutor{failures: []error{failure}, events: rejectedReasoningCompactionEvents()}
					_, result, err := runRejectedReasoningPipeline(t, t.Context(), request, executor, t.Name(), raw, retries,
						func(*PersistenceState, *PersistentOutboundTransformer) pipeline.Middleware {
							return &relayReplayTestDestination{}
						})
					require.Error(t, err)
					require.Nil(t, result)
					require.Equal(t, failure.StatusCode, ExtractStatusCodeFromError(err))
					require.Len(t, executor.requests, 1)
					require.Equal(t, original, request.Body)
					require.Equal(t, headers, request.Headers)
					require.Equal(t, headers.Get("Thread-Id"), executor.requests[0].Headers.Get("Thread-Id"))
					require.JSONEq(t, gjson.GetBytes(original, "input").Raw, gjson.GetBytes(executor.requests[0].Body, "input").Raw)
				})
			}
		}
	}
}

func TestResponsesCapacityStopsLocalCompaction(t *testing.T) {
	outbound := newCodexResponsesPassThroughOutbound()
	outbound.wrapped = new(responses.OutboundTransformer)
	outbound.state.RetryPolicyProvider = &mockRetryPolicyProvider{policy: &biz.RetryPolicy{Enabled: true, MaxSingleChannelRetries: 4}}
	request := relayAffinityRequest(t)
	outbound.state.RawRequest = request
	executor := &responsesReasoningPipelineExecutor{failures: []error{relayAffinityOverload()}, events: rejectedReasoningCompactionEvents()}
	adapter := newRemoteCompactionAdapter(nil, nil, nil)
	stream, _, err := adapter.startLocalCompactionStream(t.Context(), outbound, request, executor, applyResponsesRejectedStatusCompatibility(outbound))
	require.Error(t, err)
	require.Nil(t, stream)
	require.Len(t, executor.requests, 1)
}

func TestResponsesCapacityHTTPDoesNotResend(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		failure := relayAffinityOverload()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-New-Api-Routed-Channel-Id", "(11,11),(22,22)")
		w.WriteHeader(failure.StatusCode)
		_, _ = w.Write(failure.Body)
	}))
	defer server.Close()
	executor := httpclient.NewHttpClientWithProxy(&httpclient.ProxyConfig{Type: httpclient.ProxyTypeDisabled})
	_, result, err := runRejectedReasoningPipeline(t, t.Context(), relayAffinityRequest(t), executor, "capacity-http", true, 5,
		func(*PersistenceState, *PersistentOutboundTransformer) pipeline.Middleware {
			return pipeline.OnRawRequest("test-capacity-destination", func(_ context.Context, request *httpclient.Request) (*httpclient.Request, error) {
				request.URL = server.URL + "/v1/responses"
				return request, nil
			})
		})
	require.Error(t, err)
	require.Nil(t, result)
	require.Equal(t, int32(1), calls.Load())
}
