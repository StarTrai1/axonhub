package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
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

func TestAzureHistoryCompatibilityDistinguishesToolDataFromProtocolParts(t *testing.T) {
	for _, scenario := range []struct {
		output string
		reject bool
	}{
		{`{"id":"business","encrypted_content":"business","file_id":"business"}`, false},
		{`{"type":"encrypted_content","encrypted_content":"business object"}`, false},
		{`[{"id":"business","encrypted_content":"business","file_id":"business"}]`, false},
		{`[{"type":"input_text","text":"opaque business text"}]`, false},
		{`[{"type":"encrypted_content","encrypted_content":"protocol cipher"}]`, true},
		{`[{"type":"item_reference","id":"external"}]`, true},
		{`[{"type":"input_file","file_id":"external"}]`, true},
	} {
		body := []byte(`{"input":[{"role":"user","content":"task"},{"type":"function_call","id":"fc_old","call_id":"call_pair","name":"exec","arguments":"{}"},{"type":"function_call_output","call_id":"call_pair","output":` + scenario.output + `}]}`)
		result, err := azureCompatibleResponsesHistory(body)
		if scenario.reject {
			require.Error(t, err)
		} else {
			require.NoError(t, err)
			require.Equal(t, scenario.output, gjson.GetBytes(result, "input.2.output").Raw)
		}
	}
}

func markAzureRecovery(t *testing.T, outbound *PersistentOutboundTransformer, request *httpclient.Request) {
	t.Helper()
	outbound.state.RawProviderRequest = request
	require.True(t, requestAzureHistoryRecovery(t.Context(), outbound, resourceMismatchHTTPError()))
}

func TestAzureRecoveryPreservesInitialRequestAndScopesRetry(t *testing.T) {
	outbound, request := portableHistoryOutbound(nil, nil)
	// Old database/cache JSON cannot revive a removed channel-wide policy.
	require.NoError(t, json.Unmarshal([]byte(`{"azureHistoryCompatibility":true}`), &outbound.GetCurrentChannel().Policies))

	middleware := applyAzureResponsesHistoryCompatibility(outbound)
	initial, err := middleware.OnOutboundRawRequest(t.Context(), request)
	require.NoError(t, err)
	require.Same(t, request, initial)
	markAzureRecovery(t, outbound, request)
	for _, change := range []string{"same", "url", "model", "credential"} {
		t.Run(change, func(t *testing.T) {
			retry := *request
			retry.Headers = request.Headers.Clone()
			retry.JSONBody = retry.Body
			switch change {
			case "url":
				retry.URL += "/other"
			case "model":
				retry.Body, err = sjson.SetBytes(retry.Body, "model", "other")
			case "credential":
				retry.Headers.Set("Authorization", "Bearer other")
			}
			require.NoError(t, err)
			result, err := middleware.OnOutboundRawRequest(t.Context(), &retry)
			require.NoError(t, err)
			if change == "same" {
				require.JSONEq(t, portableHistoryAfter, string(result.Body))
				require.Equal(t, result.Body, result.JSONBody)
				require.Empty(t, result.Headers.Get(codexTurnStateHeader))
				require.True(t, result.ResponsesStateless)
			} else {
				require.Same(t, &retry, result)
			}
		})
	}
	next, fresh := portableHistoryOutbound(nil, nil)
	result, err := applyAzureResponsesHistoryCompatibility(next).OnOutboundRawRequest(t.Context(), fresh)
	require.NoError(t, err)
	require.Same(t, fresh, result)
}

func TestAzureRecoveryRunsOnlyAfterResourceError(t *testing.T) {
	for _, format := range []llm.APIFormat{llm.APIFormatOpenAIResponse, llm.APIFormatOpenAIResponseWebSocket} {
		for _, repeat := range []bool{false, true} {
			t.Run(string(format)+fmt.Sprint(repeat), func(t *testing.T) {
				request := rejectedReasoningPipelineRequest(t, format)
				request.Body = []byte(portableHistoryBefore)
				executor := &responsesReasoningPipelineExecutor{failures: []error{resourceMismatchHTTPError()}, events: rejectedReasoningCompactionEvents()}
				if repeat {
					executor.failures = append(executor.failures, resourceMismatchHTTPError())
				}
				_, result, err := runRejectedReasoningPipeline(t, t.Context(), request, executor, "synthetic", true, 4,
					func(state *PersistenceState, outbound *PersistentOutboundTransformer) pipeline.Middleware {
						state.ChannelModelsCandidates[0].Channel.Credentials = objects.ChannelCredentials{APIKey: "synthetic"}
						return pipeline.OnRawRequest("relay-target", func(_ context.Context, req *httpclient.Request) (*httpclient.Request, error) {
							req.URL = "https://relay.example/v1/responses"
							return req, nil
						})
					},
					func(_ *PersistenceState, outbound *PersistentOutboundTransformer) pipeline.Middleware {
						return applyAzureResponsesHistoryCompatibility(outbound)
					})
				require.Len(t, executor.requests, 2, "one recovery even when the provider rejects it again")
				require.JSONEq(t, gjson.Get(portableHistoryBefore, "input").Raw, gjson.GetBytes(executor.requests[0].Body, "input").Raw)
				require.JSONEq(t, gjson.Get(portableHistoryAfter, "input").Raw, gjson.GetBytes(executor.requests[1].Body, "input").Raw)
				if repeat {
					require.Error(t, err)
					require.Nil(t, result)
				} else {
					require.NoError(t, err)
					drainRejectedReasoningPipeline(t, result)
				}
			})
		}
	}
}

func TestAzureRecoveryDoesNotAuthorizeOtherFailures(t *testing.T) {
	for _, scenario := range []string{"500", "unrelated", "oauth", "other-channel", "other-format", "canceled", "bad-param"} {
		t.Run(scenario, func(t *testing.T) {
			outbound, request := portableHistoryOutbound(nil, nil)
			outbound.state.RawProviderRequest = request
			failure := resourceMismatchHTTPError()
			ctx := t.Context()
			switch scenario {
			case "500":
				failure.StatusCode = 500
			case "unrelated":
				failure.Body = []byte(`{"error":{"message":"different Azure resource"}}`)
			case "oauth":
				outbound.GetCurrentChannel().Credentials = objects.ChannelCredentials{APIKey: `{"access_token":"synthetic"}`}
			case "other-channel":
				outbound.GetCurrentChannel().Type = entchannel.TypeOpenai
			case "other-format":
				request.APIFormat = string(llm.APIFormatOpenAIChatCompletion)
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "bad-param":
				failure.Body, _ = sjson.SetBytes(failure.Body, "error.param", "previous_response_id")
			}
			require.False(t, requestAzureHistoryRecovery(ctx, outbound, failure))
			require.False(t, usesAzureHistoryCompatibility(outbound))
		})
	}
}

func TestAzureRecoveryFailureDoesNotMutateRequest(t *testing.T) {
	outbound, request := portableHistoryOutbound(nil, nil)
	markAzureRecovery(t, outbound, request)
	request.Body = []byte(`{"model":"gpt-6-astra","previous_response_id":"missing","input":"next"}`)
	_, err := applyAzureResponsesHistoryCompatibility(outbound).OnOutboundRawRequest(context.Background(), request)
	var responseErr *llm.ResponseError
	require.ErrorAs(t, err, &responseErr)
	require.Equal(t, http.StatusBadRequest, responseErr.StatusCode)
	require.Equal(t, "azure_history_unavailable", responseErr.Detail.Code)
	require.True(t, json.Valid(request.Body))
	require.Equal(t, "missing", gjson.GetBytes(request.Body, "previous_response_id").String())
}

func TestAzureRecoveryMaterializesCheckpointOnlyAfterRejection(t *testing.T) {
	ctx, client, apiKey, adapter := rejectedCompactionStorage(t)
	seedRejectedCompactionSource(t, ctx, client, apiKey, "completed", "native-checkpoint-ciphertext")
	executor := &rejectedCompactionBridgeExecutor{}
	req := rejectedCompactionPipelineRequest(t, llm.APIFormatOpenAIResponse)
	_, result, err := runRejectedReasoningPipeline(t, ctx, req, executor, "native-bridge-credential", true, 2,
		func(state *PersistenceState, outbound *PersistentOutboundTransformer) pipeline.Middleware {
			state.APIKey = apiKey
			state.ChannelModelsCandidates[0].Channel.Credentials = objects.ChannelCredentials{APIKey: "native-bridge-credential"}
			return pipeline.OnRawRequest("relay-target", func(_ context.Context, req *httpclient.Request) (*httpclient.Request, error) {
				req.URL = "https://relay.example/v1/responses"
				return req, nil
			})
		},
		func(_ *PersistenceState, outbound *PersistentOutboundTransformer) pipeline.Middleware {
			return recoverRejectedRemoteCompaction(outbound, adapter, executor)
		},
		func(_ *PersistenceState, outbound *PersistentOutboundTransformer) pipeline.Middleware {
			return applyAzureResponsesHistoryCompatibility(outbound)
		})
	require.NoError(t, err)
	drainRejectedReasoningPipeline(t, result)
	require.Len(t, executor.continuations, 2)
	require.Len(t, executor.summaries, 1)
	require.True(t, gjson.GetBytes(executor.continuations[0], `input.#(type=="compaction")`).Exists())
	require.False(t, gjson.GetBytes(executor.continuations[1], `input.#(type=="compaction")`).Exists())
	require.Contains(t, string(executor.continuations[1]), "verified retained source summary")
}
