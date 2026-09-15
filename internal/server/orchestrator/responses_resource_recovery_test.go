package orchestrator

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	entchannel "github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

const responsesResourceMismatchMessage = "The requested item was created under a different Azure OpenAI resource. Use the same resource that created the item to access it."

const responsesResourceHistoryFixture = `{
	"model":"gpt-6-astra","stream":true,"store":false,
	"reasoning":{"effort":"high","context":"all_turns"},
	"prompt_cache_key":"preserve-cache-key","include":["reasoning.encrypted_content"],
	"input":[
		{"type":"additional_tools","id":"at_source","role":"system","tools":[]},
		{"type":"message","id":"msg_user","role":"user","content":[{"type":"input_text","text":"preserve the complete task"}],"internal_chat_message_metadata_passthrough":{"content_item_kinds":[]}},
		{"type":"reasoning","id":"rs_source","encrypted_content":"preserve-native-reasoning","summary":[{"type":"summary_text","text":"visible reasoning summary"}]},
		{"type":"message","id":"msg_source","role":"assistant","phase":"commentary","content":[{"type":"output_text","text":"prior explanation"}]},
		{"type":"function_call","id":"fc_source","call_id":"call_function","name":"exec","namespace":"tools","arguments":"{\"command\":\"synthetic\"}"},
		{"type":"function_call_output","id":"fco_source","call_id":"call_function","output":"complete function result"},
		{"type":"custom_tool_call","id":"ctc_source","call_id":"call_custom","name":"custom","input":"complete custom input"},
		{"type":"custom_tool_call_output","id":"ctco_source","call_id":"call_custom","output":[{"type":"input_text","text":"complete custom result"}]},
		{"type":"compaction_trigger"}
	]
}`

func TestResponsesRejectedResourceErrorGuards(t *testing.T) {
	for _, scenario := range []struct {
		name    string
		status  int
		code    string
		message string
		param   string
		want    bool
	}{
		{name: "native resource rejection", status: 400, message: responsesResourceMismatchMessage, want: true},
		{name: "relay rejection", status: 400, message: "OpenAI Responses bad request: " + strings.ReplaceAll(responsesResourceMismatchMessage, "Azure", "***") + " [trace_id=synthetic-trace]", want: true},
		{name: "generic bad request code", status: 400, code: "bad_request", message: responsesResourceMismatchMessage, want: true},
		{name: "generic invalid request code", status: 400, code: "invalid_request_error", message: responsesResourceMismatchMessage, want: true},
		{name: "indexed message", status: 400, message: responsesResourceMismatchMessage, param: "input[3].id", want: true},
		{name: "server error", status: 500, message: responsesResourceMismatchMessage},
		{name: "unrelated code", status: 400, code: "permission_denied", message: responsesResourceMismatchMessage},
		{name: "incomplete resource mention", status: 400, message: "item belongs to a different Azure resource"},
		{name: "extra error detail", status: 400, message: responsesResourceMismatchMessage + " Another error occurred."},
		{name: "unrelated parameter", status: 400, message: responsesResourceMismatchMessage, param: "previous_response_id"},
		{name: "reasoning ID is opaque", status: 400, message: responsesResourceMismatchMessage, param: "input[2].id"},
		{name: "out of range", status: 400, message: responsesResourceMismatchMessage, param: "input[99].id"},
		{name: "nested ID", status: 400, message: responsesResourceMismatchMessage, param: "input[3].content.id"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			failure := &llm.ResponseError{StatusCode: scenario.status, Detail: llm.ErrorDetail{
				Code: scenario.code, Message: scenario.message, Param: scenario.param,
			}}
			rule, accepted := responsesRejectedStatusRuleFromError(failure, []byte(responsesResourceHistoryFixture))
			require.Equal(t, scenario.want, accepted)
			if accepted {
				require.Equal(t, "id", rule.fieldName())
				require.False(t, rule.dropItem)
			}
		})
	}
}

func TestResponsesRejectedResourceHistoryGuards(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		path   string
		value  any
		remove bool
	}{
		{name: "previous response reference", path: "previous_response_id", value: "resp_other"},
		{name: "conversation reference", path: "conversation", value: map[string]string{"id": "conv_other"}},
		{name: "no user history", path: "input.1.role", value: "assistant"},
		{name: "item reference", path: "input.8", value: map[string]string{"type": "item_reference", "id": "msg_other"}},
		{name: "native compaction", path: "input.8", value: map[string]string{"type": "compaction", "id": "cmp_other", "encrypted_content": "keep-native-compaction"}},
		{name: "unknown state", path: "input.8", value: map[string]string{"type": "future_state", "id": "future_other"}},
		{name: "ID-only message", path: "input.3.content", remove: true},
		{name: "ID-only call", path: "input.4.arguments", remove: true},
		{name: "ID-only result", path: "input.5.output", remove: true},
		{name: "orphan tool result", path: "input.5.call_id", value: "call_missing"},
		{name: "encrypted tool arguments", path: "input.4.encrypted_function_args", value: []string{"opaque"}},
		{name: "encrypted tool result", path: "input.7.output", value: []map[string]string{{"type": "encrypted_content", "encrypted_content": "opaque"}}},
		{name: "stored input file", path: "input.1.content", value: []map[string]string{{"type": "input_file", "file_id": "file_other"}}},
		{name: "malformed ID", path: "input.3.id", value: 12},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			body := []byte(responsesResourceHistoryFixture)
			var err error
			if scenario.remove {
				body, err = sjson.DeleteBytes(body, scenario.path)
			} else {
				body, err = sjson.SetBytes(body, scenario.path, scenario.value)
			}
			require.NoError(t, err)
			_, accepted := responsesRejectedResourceRule(body, "", responsesResourceMismatchMessage, "")
			require.False(t, accepted)
			_, changed, err := stripResponsesRejectedStatus(body, []responsesRejectedStatusRule{{index: -1, field: "id"}})
			require.Error(t, err, "a rule learned earlier in this request must recheck materialized history")
			require.False(t, changed)
		})
	}
}

func TestResponsesRejectedResourcePipelinePreservesHistory(t *testing.T) {
	for _, scenario := range []struct {
		name        string
		noReasoning bool
		passThrough bool
	}{
		{"no reasoning with pass-through", true, true},
		{"no reasoning with transformed stream", true, false},
		{"native reasoning with pass-through", false, true},
		{"native reasoning with transformed stream", false, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			request := rejectedReasoningPipelineRequest(t, llm.APIFormatOpenAIResponse)
			request.Body = []byte(responsesResourceHistoryFixture)
			if scenario.noReasoning {
				var err error
				request.Body, err = sjson.DeleteBytes(request.Body, "input.2")
				require.NoError(t, err)
			}
			original := append([]byte(nil), request.Body...)
			executor := &responsesReasoningPipelineExecutor{
				failures: []error{resourceMismatchHTTPError()}, events: rejectedReasoningCompactionEvents(),
			}
			_, result, err := runRejectedReasoningPipeline(t, t.Context(), request, executor, "resource-credential", scenario.passThrough, 1)
			require.NoError(t, err)
			events := drainRejectedReasoningPipeline(t, result)
			require.Len(t, executor.requests, 2)
			require.JSONEq(t, gjson.GetBytes(original, "input").Raw, gjson.GetBytes(executor.requests[0].Body, "input").Raw)
			assertPortableIDsOnlyRemoved(t, executor.requests[0].Body, executor.requests[1].Body)
			require.Equal(t, original, request.Body)
			require.Equal(t, executor.requests[0].Headers.Get("Authorization"), executor.requests[1].Headers.Get("Authorization"))
			require.Equal(t, "new-native-compaction", gjson.Get(events["response.completed"], "response.output.0.encrypted_content").String())
		})
	}
}

func TestResponsesRejectedResourcePipelineSharesReasoningRetryBudget(t *testing.T) {
	for _, scenario := range []struct {
		name       string
		retries    int
		second     error
		wantCalls  int
		wantResult bool
	}{
		{"later resource rejection", 2, resourceMismatchHTTPError(), 3, true},
		{"later decryption rejection", 2, &httpclient.Error{StatusCode: 400, Body: []byte(`{"error":{"code":"invalid_encrypted_content"}}`)}, 3, true},
		{"one shared retry", 1, resourceMismatchHTTPError(), 2, false},
		{"no retry budget", 0, resourceMismatchHTTPError(), 1, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			request := rejectedReasoningPipelineRequest(t, llm.APIFormatOpenAIResponse)
			request.Body = []byte(responsesResourceHistoryFixture)
			executor := &responsesReasoningPipelineExecutor{
				failures: []error{resourceMismatchHTTPError(), scenario.second}, events: rejectedReasoningCompactionEvents(),
			}
			ctx := shared.WithSessionScope(t.Context(), "api_key:resource-recovery:project:1")
			state, result, err := runRejectedReasoningPipeline(t, ctx, request, executor, "resource-reasoning-credential", true, scenario.retries)
			require.Len(t, executor.requests, scenario.wantCalls)
			if scenario.wantCalls > 1 {
				assertPortableIDsOnlyRemoved(t, executor.requests[0].Body, executor.requests[1].Body)
			}
			if !scenario.wantResult {
				require.Error(t, err)
				require.Nil(t, result)
				return
			}
			require.NoError(t, err)
			drainRejectedReasoningPipeline(t, result)
			require.Empty(t, gjson.GetBytes(executor.requests[2].Body, `input.#(encrypted_content)#`).Array())
			require.Equal(t, "visible reasoning summary", gjson.GetBytes(executor.requests[2].Body, "input.2.content.0.text").String())
			for _, path := range []string{"input.0", "input.1", "input.3", "input.4", "input.5", "input.6", "input.7", "input.8", "reasoning", "prompt_cache_key"} {
				require.JSONEq(t, gjson.GetBytes(executor.requests[1].Body, path).Raw, gjson.GetBytes(executor.requests[2].Body, path).Raw, path)
			}
			scope, ok := responsesReasoningScope(ctx, state.CurrentCandidate.Channel, executor.requests[0])
			require.True(t, ok)
			t.Cleanup(func() { responsesReasoningRecoveries.Remove(scope) })
		})
	}
}

func TestResponsesRejectedResourcePipelineStandaloneCompact(t *testing.T) {
	request := rejectedReasoningPipelineRequest(t, llm.APIFormatOpenAIResponseCompact)
	var err error
	request.Body, err = sjson.DeleteBytes([]byte(responsesResourceHistoryFixture), "stream")
	require.NoError(t, err)
	request.Body, err = sjson.DeleteBytes(request.Body, "input.8")
	require.NoError(t, err)
	executor := &responsesReasoningPipelineExecutor{
		failures: []error{resourceMismatchHTTPError()},
		response: &httpclient.Response{
			StatusCode: http.StatusOK,
			Headers:    http.Header{"Content-Type": {"application/json"}},
			Body:       []byte(`{"id":"cmp_response","object":"response.compaction","created_at":1,"model":"gpt-6-astra","output":[{"type":"message","role":"user","content":[{"type":"input_text","text":"retained user instruction"}]},{"type":"compaction","id":"cmp_output","encrypted_content":"new-native-compaction"}]}`),
		},
	}
	_, result, err := runRejectedReasoningPipeline(t, t.Context(), request, executor, "resource-compact-credential", true, 1)
	require.NoError(t, err)
	require.False(t, result.Stream)
	require.Len(t, executor.requests, 2)
	assertPortableIDsOnlyRemoved(t, executor.requests[0].Body, executor.requests[1].Body)
	require.JSONEq(t, gjson.GetBytes(executor.response.Body, "output").Raw, gjson.GetBytes(result.Response.Body, "output").Raw, "the canonical compacted window must be returned intact")
}

func TestResponsesRejectedResourceRuleStaysWithinRequestAndDestination(t *testing.T) {
	outbound := newCodexResponsesPassThroughOutbound()
	original := &httpclient.Request{
		URL: "https://example.invalid/v1/responses", APIFormat: string(llm.APIFormatOpenAIResponse),
		Headers: http.Header{"Authorization": {"Bearer synthetic-original"}}, Body: []byte(responsesResourceHistoryFixture),
	}
	outbound.state.RawProviderRequest = original
	middleware := applyResponsesRejectedStatusCompatibility(outbound)
	middleware.OnOutboundRawError(t.Context(), resourceMismatchHTTPError())
	require.True(t, hasResponsesRejectedStatusCompatibilityRetry(outbound.state, outbound.GetCurrentChannel().ID))
	for _, isolation := range []string{"credential", "url", "model"} {
		t.Run(isolation, func(t *testing.T) {
			retry := *original
			retry.Headers = original.Headers.Clone()
			switch isolation {
			case "credential":
				retry.Headers.Set("Authorization", "Bearer synthetic-other")
			case "url":
				retry.URL = "https://another.invalid/v1/responses"
			case "model":
				var err error
				retry.Body, err = sjson.SetBytes(original.Body, "model", "gpt-5.6-sol")
				require.NoError(t, err)
			}
			before := append([]byte(nil), retry.Body...)
			got, err := middleware.OnOutboundRawRequest(t.Context(), &retry)
			require.NoError(t, err)
			require.Equal(t, before, got.Body)
		})
	}
	retry := *original
	got, err := middleware.OnOutboundRawRequest(t.Context(), &retry)
	require.NoError(t, err)
	assertPortableIDsOnlyRemoved(t, original.Body, got.Body)
	next := newCodexResponsesPassThroughOutbound()
	copyOfOriginal := *original
	got, err = applyResponsesRejectedStatusCompatibility(next).OnOutboundRawRequest(t.Context(), &copyOfOriginal)
	require.NoError(t, err)
	require.Equal(t, original.Body, got.Body, "resource mismatch must not become a global ID-removal capability")
	next.state.RawProviderRequest = original
	next.state.CurrentCandidate.Channel.Type = entchannel.TypeOpenaiResponses
	applyResponsesRejectedStatusCompatibility(next).OnOutboundRawError(t.Context(), resourceMismatchHTTPError())
	require.False(t, hasResponsesRejectedStatusCompatibilityRetry(next.state, next.GetCurrentChannel().ID))
}

func resourceMismatchHTTPError() *httpclient.Error {
	return &httpclient.Error{StatusCode: http.StatusBadRequest, Body: []byte(`{"error":{"type":"invalid_request_error","message":"` + responsesResourceMismatchMessage + `"}}`)}
}

func assertPortableIDsOnlyRemoved(t *testing.T, before, after []byte) {
	t.Helper()
	var expected map[string]any
	require.NoError(t, json.Unmarshal(before, &expected))
	for _, value := range expected["input"].([]any) {
		item := value.(map[string]any)
		if item["type"] != "reasoning" {
			delete(item, "id")
		}
	}
	body, err := json.Marshal(expected)
	require.NoError(t, err)
	require.JSONEq(t, string(body), string(after), "all explicit content, call IDs, ciphertext, controls, and top-level fields must survive")
}
