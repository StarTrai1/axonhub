package orchestrator

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	entchannel "github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

func TestAzureHistoryCompatibilityPreservesSemanticPayloads(t *testing.T) {
	body := []byte(`{"model":"gpt-6-astra","prompt_cache_key":"","include":["reasoning.encrypted_content","message.output_text"],"input":[
{"type":"message","id":"user_old","role":"user","content":"task"},
{"type":"reasoning","id":"rs_old","encrypted_content":"cipher","summary":[{"type":"summary_text","text":"visible summary"}]},
{"type":"function_call","id":"fc_old","call_id":"call_1","name":"tool","arguments":"{\"id\":9007199254740993,\"encrypted_content\":\"business data\"}"},
{"type":"function_call_output","id":"fco_old","call_id":"call_1","output":{"id":9007199254740993,"encrypted_content":"business result"}},
{"type":"custom_tool_call","id":"custom_old","call_id":"custom_1","name":"custom","input":"exact input"},
{"type":"custom_tool_call_output","id":"result_old","call_id":"custom_1","output":"exact output"}]}`)
	original := append([]byte(nil), body...)
	result, err := azureCompatibleResponsesHistory(body)
	require.NoError(t, err)
	require.Equal(t, original, body)
	for _, item := range gjson.GetBytes(result, "input").Array() {
		require.False(t, item.Get("id").Exists())
		require.False(t, item.Get("encrypted_content").Exists())
	}
	require.Equal(t, "visible summary", gjson.GetBytes(result, "input.1.content.0.text").String())
	for _, path := range []string{"input.2.arguments", "input.3.output", "input.4.input", "input.5.output", "prompt_cache_key"} {
		require.Equal(t, gjson.GetBytes(body, path).Raw, gjson.GetBytes(result, path).Raw, path)
	}
	for _, index := range []string{"2", "3", "4", "5"} {
		require.Equal(t, gjson.GetBytes(body, "input."+index+".call_id").String(), gjson.GetBytes(result, "input."+index+".call_id").String())
	}
	require.JSONEq(t, `["message.output_text"]`, gjson.GetBytes(result, "include").Raw)
	again, err := azureCompatibleResponsesHistory(result)
	require.NoError(t, err)
	require.Equal(t, result, again, "compatibility is idempotent across retries")
}

func TestAzureHistoryCompatibilityRejectsUnresolvedHistory(t *testing.T) {
	for _, body := range []string{
		`{"previous_response_id":"resp_unknown","input":"next"}`,
		`{"conversation":{"id":"conv_unknown"},"input":"next"}`,
		`{"input":[{"type":"item_reference","id":"missing"}]}`,
		`{"input":[{"role":"user","content":"task"},{"type":"compaction","id":"cmp_unknown","encrypted_content":"only history"}]}`,
		`{"input":[{"role":"user","content":"task"},{"type":"function_call_output","call_id":"missing","output":"result"}]}`,
		`{"input":[{"role":"user","content":[{"type":"input_file","file_id":"file_other"}]}]}`,
		`{"input":[{"role":"user","content":"task"},{"type":"agent_message","author":"a","recipient":"b","content":[{"type":"encrypted_content","encrypted_content":"only history"}]}]}`,
	} {
		_, err := azureCompatibleResponsesHistory([]byte(body))
		require.Error(t, err, body)
	}
	for _, body := range []string{`{"input":"new task","include":["reasoning.encrypted_content"]}`, `{"input":[{"role":"user","content":"new task"}]}`} {
		result, err := azureCompatibleResponsesHistory([]byte(body))
		require.NoError(t, err)
		require.False(t, gjson.GetBytes(result, "prompt_cache_key").Exists())
		require.False(t, gjson.GetBytes(result, "include").Exists())
	}
}

func TestAzureHistoryCompatibilityIsOptInAndChannelScoped(t *testing.T) {
	for _, scenario := range []string{"enabled", "disabled", "oauth", "other-channel", "other-format"} {
		t.Run(scenario, func(t *testing.T) {
			outbound, request := portableHistoryOutbound(nil, nil)
			channel := outbound.GetCurrentChannel()
			channel.Policies.AzureHistoryCompatibility = scenario != "disabled"
			if scenario == "oauth" {
				channel.Credentials = objects.ChannelCredentials{APIKey: `{"access_token":"synthetic"}`}
			}
			if scenario == "other-channel" { channel.Type = entchannel.TypeOpenai }
			if scenario == "other-format" { request.APIFormat = string(llm.APIFormatOpenAIChatCompletion) }
			original := append([]byte(nil), request.Body...)
			request.JSONBody = request.Body
			result, err := applyAzureResponsesHistoryCompatibility(outbound).OnOutboundRawRequest(t.Context(), request)
			require.NoError(t, err)
			require.Equal(t, original, request.Body, "the source request remains reusable on another channel")
			require.NotEmpty(t, request.Headers.Get(codexTurnStateHeader))
			if scenario == "enabled" {
				require.NotEqual(t, original, result.Body)
				require.Equal(t, result.Body, result.JSONBody)
				require.Empty(t, result.Headers.Get(codexTurnStateHeader))
				require.True(t, result.ResponsesStateless)
			} else {
				require.Same(t, request, result)
				require.False(t, result.ResponsesStateless)
			}
		})
	}
}

func TestAzureHistoryCompatibilityRunsBeforeFirstUpstreamAttempt(t *testing.T) {
	for _, format := range []llm.APIFormat{llm.APIFormatOpenAIResponse, llm.APIFormatOpenAIResponseWebSocket} {
		for _, passThrough := range []bool{true, false} {
			t.Run(string(format)+"/"+map[bool]string{true:"native",false:"converted"}[passThrough], func(t *testing.T) {
				request := rejectedReasoningPipelineRequest(t, format)
				request.Body = []byte(portableHistoryBefore)
				var err error
				request.Body, err = sjson.SetBytes(request.Body, "include", []string{"reasoning.encrypted_content"})
				require.NoError(t, err)
				executor := &responsesReasoningPipelineExecutor{events: rejectedReasoningCompactionEvents()}
				_, result, err := runRejectedReasoningPipeline(t, t.Context(), request, executor, "synthetic", passThrough, 0,
					func(state *PersistenceState, outbound *PersistentOutboundTransformer) pipeline.Middleware {
						state.ChannelModelsCandidates[0].Channel.Policies.AzureHistoryCompatibility = true
						return applyAzureResponsesHistoryCompatibility(outbound)
					})
				require.NoError(t, err)
				drainRejectedReasoningPipeline(t, result)
				require.Len(t, executor.requests, 1)
				require.JSONEq(t, gjson.Get(portableHistoryAfter, "input").Raw, gjson.GetBytes(executor.requests[0].Body, "input").Raw)
				require.False(t, gjson.GetBytes(executor.requests[0].Body, "include").Exists())
				require.True(t, executor.requests[0].ResponsesStateless)
			})
		}
	}
}

func TestAzureHistoryCompatibilityFailureDoesNotMutateRequest(t *testing.T) {
	outbound, request := portableHistoryOutbound(nil, nil)
	outbound.GetCurrentChannel().Policies.AzureHistoryCompatibility = true
	request.Body = []byte(`{"previous_response_id":"missing","input":"next"}`)
	_, err := applyAzureResponsesHistoryCompatibility(outbound).OnOutboundRawRequest(context.Background(), request)
	var responseErr *llm.ResponseError
	require.ErrorAs(t, err, &responseErr)
	require.Equal(t, http.StatusBadRequest, responseErr.StatusCode)
	require.Equal(t, "azure_history_unavailable", responseErr.Detail.Code)
	require.True(t, json.Valid(request.Body))
	require.Equal(t, "missing", gjson.GetBytes(request.Body, "previous_response_id").String())
}

func TestAzureHistoryCompatibilityPreparesCheckpointWithoutRejection(t *testing.T) {
	ctx, client, apiKey, adapter := rejectedCompactionStorage(t)
	seedRejectedCompactionSource(t, ctx, client, apiKey, "completed", "native-checkpoint-ciphertext")
	executor := &rejectedCompactionBridgeExecutor{}
	for range 2 {
		req := rejectedCompactionPipelineRequest(t, llm.APIFormatOpenAIResponse)
		_, result, err := runRejectedReasoningPipeline(t, ctx, req, executor, "native-bridge-credential", true, 0,
			func(state *PersistenceState, outbound *PersistentOutboundTransformer) pipeline.Middleware {
				state.APIKey = apiKey
				state.ChannelModelsCandidates[0].Channel.Policies.AzureHistoryCompatibility = true
				return recoverRejectedRemoteCompaction(outbound, adapter, executor)
			},
			func(_ *PersistenceState, outbound *PersistentOutboundTransformer) pipeline.Middleware {
				return applyAzureResponsesHistoryCompatibility(outbound)
			})
		require.NoError(t, err)
		drainRejectedReasoningPipeline(t, result)
	}
	require.Len(t, executor.continuations, 2, "each user turn reaches upstream once")
	require.Len(t, executor.summaries, 1, "reuse the retained checkpoint summary")
	for _, body := range executor.continuations {
		require.False(t, gjson.GetBytes(body, `input.#(type=="compaction")`).Exists())
		require.Contains(t, string(body), "verified retained source summary")
	}
}

func TestAzureHistoryCompatibilityRetainsNativeFullHistory(t *testing.T) {
	outbound, _ := portableHistoryOutbound(nil, nil)
	outbound.GetCurrentChannel().Policies.AzureHistoryCompatibility = true
	store := newResponsesSessionStore()
	ctx := shared.WithResponsesWebSocket(shared.WithSessionScope(t.Context(), "azure-native-owner"))
	body := []byte(`{"model":"gpt-6-astra","input":[{"role":"user","content":"first"}]}`)
	store.record(ctx, body, []byte(`{"id":"resp_first","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"answer"}]}]}`))
	c := newResponsesContinuation(store, []byte(`{"model":"gpt-6-astra","previous_response_id":"resp_first","input":"next"}`))
	outbound.state.ResponsesContinuation = c
	result, err := prepareResponsesAttemptBody(ctx, outbound, responsesAttemptTarget{
		origin: shared.ResponsesOrigin{ChannelID: outbound.GetCurrentChannel().ID, Format: string(llm.APIFormatOpenAIResponseWebSocket)},
		forwardsID: false,
	})
	require.NoError(t, err)
	require.True(t, c.complete)
	require.True(t, c.retain)
	require.False(t, gjson.GetBytes(result, "previous_response_id").Exists())
	require.Len(t, gjson.GetBytes(result, "input").Array(), 3)
	_, err = azureCompatibleResponsesHistory(result)
	require.NoError(t, err)
}
