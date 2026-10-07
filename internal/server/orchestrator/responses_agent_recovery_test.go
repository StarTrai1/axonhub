package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/streams"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

const preservedEncryptedAgentMessage = `{"type":"agent_message","id":"am_private","author":"/root/worker","recipient":"/root","content":[{"type":"input_text","text":"visible delivery metadata"},{"type":"encrypted_content","encrypted_content":"opaque-agent-result"}],"internal_chat_message_metadata_passthrough":{"turn_id":"agent-turn"}}`
const preservedPlainAgentMessage = `{"type":"agent_message","id":"am_public","author":"/root/worker","recipient":"/root","content":[{"type":"input_text","text":"visible agent result"}]}`

func agentRecoveryRequest(t *testing.T, format llm.APIFormat) *httpclient.Request {
	t.Helper()
	req := rejectedReasoningPipelineRequest(t, format)
	items := []json.RawMessage{
		json.RawMessage(`{"type":"additional_tools","id":"at_original","role":"system","tools":[]}`),
		json.RawMessage(`{"type":"compaction","id":"cmp_native","encrypted_content":"native-checkpoint-ciphertext"}`),
	}
	for _, item := range gjson.GetBytes([]byte(responsesRejectedReasoningFixture), "input").Array() {
		items = append(items, json.RawMessage(item.Raw))
	}
	items = append(items, json.RawMessage(preservedEncryptedAgentMessage), json.RawMessage(preservedPlainAgentMessage))
	var err error
	req.Body, err = sjson.SetBytes(req.Body, "input", items)
	require.NoError(t, err)
	req.Body, err = sjson.SetBytes(req.Body, "client_metadata.thread_id", req.Headers.Get("Thread-Id"))
	require.NoError(t, err)
	return req
}

func TestResponsesRejectedAgentHistoryGuards(t *testing.T) {
	body := agentRecoveryRequest(t, llm.APIFormatOpenAIResponse).Body
	for _, tc := range []struct {
		name, code, message, param string
		path                       string
		value                      any
		want                       bool
	}{
		{name: "official websocket whole input", code: "invalid_encrypted_content", message: rejectedReasoningMessage, param: "input", want: true},
		{name: "relay whole input", code: "thinking_signature_invalid", message: rejectedReasoningMessage, param: "input", want: true},
		{name: "exact index", code: "invalid_encrypted_content", message: rejectedReasoningMessage, param: "input[3].encrypted_content", want: true},
		{name: "exact message without param", code: "invalid_encrypted_content", message: rejectedReasoningMessage, want: true},
		{name: "generic whole input", code: "invalid_encrypted_content", param: "input"},
		{name: "generic without param", code: "invalid_encrypted_content"},
		{name: "conflicting valid reasoning index", code: "invalid_encrypted_content", message: rejectedReasoningMessage, param: "input[8].encrypted_content"},
		{name: "unknown named item", code: "invalid_encrypted_content", message: "The encrypted content for item rs_missing could not be verified. Reason: Encrypted content could not be decrypted or parsed.", param: "input"},
		{name: "external response", code: "invalid_encrypted_content", message: rejectedReasoningMessage, param: "input", path: "previous_response_id", value: "resp_missing"},
		{name: "incomplete tools", code: "invalid_encrypted_content", message: rejectedReasoningMessage, param: "input", path: "input.5.call_id", value: "missing-call"},
		{name: "encrypted tool result", code: "invalid_encrypted_content", message: rejectedReasoningMessage, param: "input", path: "input.5.output", value: []map[string]string{{"type": "encrypted_content", "encrypted_content": "opaque-tool"}}},
		{name: "agent missing recipient", code: "invalid_encrypted_content", message: rejectedReasoningMessage, param: "input", path: "input.11.recipient", value: ""},
		{name: "agent unknown state", code: "invalid_encrypted_content", message: rejectedReasoningMessage, param: "input", path: "input.11.content.1.type", value: "future_opaque_state"},
		{name: "agent malformed ciphertext", code: "invalid_encrypted_content", message: rejectedReasoningMessage, param: "input", path: "input.11.content.1.encrypted_content", value: 123},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := body
			if tc.path != "" {
				var err error
				input, err = sjson.SetBytes(input, tc.path, tc.value)
				require.NoError(t, err)
			}
			rule, ok := responsesRejectedStatusRuleForDetails(input, tc.code, tc.message, tc.param)
			require.Equal(t, tc.want, ok)
			if ok {
				after, changed, err := stripResponsesRejectedStatus(input, []responsesRejectedStatusRule{rule})
				require.NoError(t, err)
				require.True(t, changed)
				require.Contains(t, string(after), preservedEncryptedAgentMessage)
				require.Contains(t, string(after), preservedPlainAgentMessage)
				require.Equal(t, gjson.GetBytes(input, "input.1").Raw, gjson.GetBytes(after, "input.1").Raw)
			}
		})
	}
	_, safe := responsesResourceHistorySupportsRecovery(body)
	require.False(t, safe, "generic resource mismatches retain the strict guard")
}

func TestResponsesRejectedAgentMessagePreservesReasoningDependencies(t *testing.T) {
	body := agentRecoveryRequest(t, llm.APIFormatOpenAIResponse).Body
	var items []json.RawMessage
	for index, item := range gjson.GetBytes(body, "input").Array() {
		if index == 4 {
			items = append(items, json.RawMessage(preservedEncryptedAgentMessage))
		}
		if item.Get("id").String() != "am_private" {
			items = append(items, json.RawMessage(item.Raw))
		}
	}
	body, err := sjson.SetBytes(body, "input", items)
	require.NoError(t, err)
	rule, ok := responsesRejectedStatusRuleForDetails(body, "invalid_encrypted_content", rejectedReasoningMessage, "input")
	require.True(t, ok)
	after, changed, err := stripResponsesRejectedStatus(body, []responsesRejectedStatusRule{rule})
	require.NoError(t, err)
	require.True(t, changed)
	require.Contains(t, string(after), preservedEncryptedAgentMessage)
	require.False(t, gjson.GetBytes(after, `input.#(type=="function_call").id`).Exists())
	require.Equal(t, "call_function", gjson.GetBytes(after, `input.#(type=="function_call").call_id`).String())
}

// Errors arriving inside a Responses stream must use the same recovery as an
// HTTP rejection, before output is delivered. The real WebSocket loopback test
// below additionally exercises the transport's error normalization.
type agentRecoveryStreamExecutor struct {
	responsesReasoningPipelineExecutor
	inBand bool
}

func (e *agentRecoveryStreamExecutor) DoStream(ctx context.Context, req *httpclient.Request) (streams.Stream[*httpclient.StreamEvent], error) {
	if !e.inBand {
		return e.responsesReasoningPipelineExecutor.DoStream(ctx, req)
	}
	if err := e.capture(req); err != nil {
		raw := err.(*httpclient.Error)
		data, setErr := sjson.SetBytes(raw.Body, "type", "error")
		if setErr != nil {
			return nil, setErr
		}
		data, setErr = sjson.SetBytes(data, "status", 400)
		if setErr != nil {
			return nil, setErr
		}
		return streams.SliceStream([]*httpclient.StreamEvent{{Data: data}}), nil
	}
	return streams.SliceStream(e.events), nil
}

func agentRecoveryError(message string) error {
	return &httpclient.Error{StatusCode: http.StatusBadRequest, Body: []byte(`{"error":{"type":"invalid_request_error","code":"invalid_encrypted_content","param":"input","message":"` + message + `"}}`)}
}

func TestResponsesRejectedAgentHistoryPipeline(t *testing.T) {
	for _, format := range []llm.APIFormat{llm.APIFormatOpenAIResponse, llm.APIFormatOpenAIResponseCompact} {
		for _, raw := range []bool{true, false} {
			for _, scenario := range []string{"checkpoint then reasoning", "reasoning then checkpoint", "expanded local history"} {
				t.Run(fmt.Sprintf("%s/raw=%t/%s", format, raw, scenario), func(t *testing.T) {
					ctx := shared.WithSessionScope(t.Context(), t.Name())
					req := agentRecoveryRequest(t, format)
					adapter := newRemoteCompactionAdapter(nil, nil, nil)
					apiKey := &ent.APIKey{ID: 501, ProjectID: 502}
					ref, _, _, err := parseRemoteCompactionRequest(req.Body)
					require.NoError(t, err)
					adapter.summaries.SetDefault(remoteCompactionOwnerCacheKey(&PersistenceState{APIKey: apiKey}, remoteCompactionCacheKey(ref)), "authenticated retained summary")
					failures := []error{agentRecoveryError(rejectedNativeCompactionMessage), agentRecoveryError(rejectedReasoningMessage)}
					if scenario == "reasoning then checkpoint" {
						failures[0], failures[1] = failures[1], failures[0]
					}
					if scenario == "expanded local history" {
						req.Body, err = replaceRemoteCompactionWithLocalSummary(req.Body, "authenticated retained summary")
						require.NoError(t, err)
						failures = []error{agentRecoveryError(rejectedReasoningMessage)}
					}
					original := append([]byte(nil), req.Body...)
					executor := &agentRecoveryStreamExecutor{responsesReasoningPipelineExecutor: responsesReasoningPipelineExecutor{
						failures: failures, events: rejectedReasoningCompactionEvents(),
						response: &httpclient.Response{StatusCode: 200, Body: []byte(`{"object":"response.compaction","output":[{"type":"compaction","id":"cmp_new","encrypted_content":"new-target-checkpoint"}]}`)},
					}, inBand: true}
					configure := func(state *PersistenceState, outbound *PersistentOutboundTransformer) pipeline.Middleware {
						state.APIKey = apiKey
						state.ChannelModelsCandidates[0].Channel.Policies.RemoteCompaction = objects.RemoteCompactionPolicyNative
						return recoverRejectedRemoteCompaction(outbound, adapter, executor)
					}
					state, result, err := runRejectedReasoningPipeline(t, ctx, req, executor, "synthetic-target", raw, len(failures), configure)
					require.NoError(t, err)
					if result.Stream {
						drainRejectedReasoningPipeline(t, result)
					}
					require.Len(t, executor.requests, len(failures)+1)
					for _, attempt := range executor.requests {
						require.Contains(t, string(attempt.Body), preservedEncryptedAgentMessage)
						require.Contains(t, string(attempt.Body), preservedPlainAgentMessage)
						require.Equal(t, executor.requests[0].URL, attempt.URL)
						require.Equal(t, executor.requests[0].Auth, attempt.Auth)
					}
					last := executor.requests[len(executor.requests)-1].Body
					require.Empty(t, encryptedResponsesReasoningHashes(last))
					require.Contains(t, string(last), "authenticated retained summary")
					require.Contains(t, string(last), "keep function output")
					require.Contains(t, string(last), "keep custom output")
					require.Equal(t, original, req.Body)
					scope, ok := responsesReasoningScope(ctx, state.CurrentCandidate.Channel, executor.requests[0])
					require.True(t, ok)
					t.Cleanup(func() { responsesReasoningRecoveries.Remove(scope) })
					require.Eventually(t, func() bool { _, found := rememberedResponsesReasoningRule(scope, original); return found }, time.Second, time.Millisecond)
					req.Body, err = sjson.SetRawBytes(original, "input.-1", []byte(`{"type":"reasoning","id":"rs_fresh","encrypted_content":"fresh-target-reasoning"}`))
					require.NoError(t, err)
					next := &responsesReasoningPipelineExecutor{events: executor.events, response: executor.response}
					_, result, err = runRejectedReasoningPipeline(t, ctx, req, next, "synthetic-target", raw, 0, configure)
					require.NoError(t, err)
					if result.Stream {
						drainRejectedReasoningPipeline(t, result)
					}
					require.Len(t, next.requests, 1)
					require.Len(t, encryptedResponsesReasoningHashes(next.requests[0].Body), 1)
					require.Contains(t, string(next.requests[0].Body), preservedEncryptedAgentMessage)
				})
			}
		}
	}
}
