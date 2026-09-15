package orchestrator

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/ent/request"
	"github.com/looplj/axonhub/internal/ent/requestexecution"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/streams"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

const responsesCompactionHistoryFixture = `{
	"model":"gpt-6-astra","stream":true,"store":false,
	"client_metadata":{"thread_id":"recovery-thread"},
	"reasoning":{"effort":"high","context":"all_turns"},
	"prompt_cache_key":"retained-cache-key",
	"input":[
		{"type":"additional_tools","id":"at_retained","role":"system","tools":[]},
		{"type":"message","id":"msg_before","role":"user","content":[{"type":"input_text","text":"retained before checkpoint"}]},
		{"type":"compaction","id":"cmp_native","encrypted_content":"native-checkpoint-ciphertext"},
		{"type":"message","id":"msg_after","role":"assistant","phase":"commentary","content":[{"type":"output_text","text":"retained after checkpoint"}]},
		{"type":"custom_tool_call","id":"ctc_retained","call_id":"call_retained","name":"custom","input":"complete tool input"},
		{"type":"custom_tool_call_output","id":"ctco_retained","call_id":"call_retained","output":[{"type":"input_text","text":"complete tool result"}]},
		{"type":"message","id":"msg_continue","role":"user","content":"continue the task"}
	]
}`

func rejectedCompactionPipelineRequest(t *testing.T, format llm.APIFormat) *httpclient.Request {
	t.Helper()
	req := rejectedReasoningPipelineRequest(t, format)
	req.Body = []byte(responsesCompactionHistoryFixture)
	req.Headers.Set("Thread-Id", "recovery-thread")
	req.Headers.Set("X-Codex-Window-Id", "recovery-thread:1")
	if format == llm.APIFormatOpenAIResponseCompact {
		var err error
		req.Body, err = sjson.DeleteBytes(req.Body, "stream")
		require.NoError(t, err)
	}
	return req
}

func TestResponsesRejectedResourcePreservesOpaqueCompaction(t *testing.T) {
	req := rejectedCompactionPipelineRequest(t, llm.APIFormatOpenAIResponse)
	original := append([]byte(nil), req.Body...)
	executor := &responsesReasoningPipelineExecutor{
		failures: []error{resourceMismatchHTTPError()}, events: rejectedReasoningCompactionEvents(),
	}
	_, result, err := runRejectedReasoningPipeline(t, t.Context(), req, executor, "compaction-id-credential", true, 1)
	require.NoError(t, err)
	drainRejectedReasoningPipeline(t, result)
	require.Len(t, executor.requests, 2)
	assertPortableIDsOnlyRemoved(t, executor.requests[0].Body, executor.requests[1].Body)
	require.Equal(t, gjson.GetBytes(original, "input.2").Raw, gjson.GetBytes(executor.requests[1].Body, "input.2").Raw)
	require.Equal(t, original, req.Body)
	_, accepted := responsesRejectedReasoningRule(executor.requests[1].Body, "")
	require.False(t, accepted, "preserving a checkpoint does not permit reasoning deletion")
}

func TestResponsesRejectedCompactionPipelineUsesRetainedSummary(t *testing.T) {
	for _, format := range []llm.APIFormat{llm.APIFormatOpenAIResponse, llm.APIFormatOpenAIResponseCompact} {
		for _, passThrough := range []bool{true, false} {
			name := string(format) + "/converted"
			if passThrough {
				name = string(format) + "/pass-through"
			}
			t.Run(name, func(t *testing.T) {
				ctx := shared.WithSessionScope(t.Context(), "owner-"+t.Name())
				apiKey := &ent.APIKey{ID: 201, ProjectID: 202}
				req := rejectedCompactionPipelineRequest(t, format)
				original := append([]byte(nil), req.Body...)
				ref, _, _, err := parseRemoteCompactionRequest(req.Body)
				require.NoError(t, err)
				adapter := newRemoteCompactionAdapter(nil, nil, nil)
				adapter.summaries.SetDefault(remoteCompactionOwnerCacheKey(&PersistenceState{APIKey: apiKey}, remoteCompactionCacheKey(ref)), "verified retained source summary")
				executor := &responsesReasoningPipelineExecutor{
					failures: []error{resourceMismatchHTTPError(), resourceMismatchHTTPError()},
					events:   rejectedReasoningCompactionEvents(),
					response: &httpclient.Response{
						StatusCode: http.StatusOK,
						Headers:    http.Header{"Content-Type": {"application/json"}},
						Body:       []byte(`{"id":"cmp_result","object":"response.compaction","created_at":1,"model":"gpt-6-astra","output":[{"type":"message","role":"user","content":[{"type":"input_text","text":"canonical retained item"}]},{"type":"compaction","id":"cmp_next","encrypted_content":"canonical-opaque-state"}]}`),
					},
				}
				_, result, err := runRejectedReasoningPipeline(t, ctx, req, executor, "compaction-recovery-credential", passThrough, 2,
					func(state *PersistenceState, outbound *PersistentOutboundTransformer) pipeline.Middleware {
						state.APIKey = apiKey
						return recoverRejectedRemoteCompaction(outbound, adapter, executor)
					})
				require.NoError(t, err)
				require.Len(t, executor.requests, 3)
				assertPortableIDsOnlyRemoved(t, executor.requests[0].Body, executor.requests[1].Body)
				assertRejectedCompactionWindowPreserved(t, executor.requests[1].Body, executor.requests[2].Body)
				require.Equal(t, original, req.Body)
				if result.Stream {
					drainRejectedReasoningPipeline(t, result)
				} else {
					require.JSONEq(t, gjson.GetBytes(executor.response.Body, "output").Raw, gjson.GetBytes(result.Response.Body, "output").Raw)
				}
			})
		}
	}
}

func assertRejectedCompactionWindowPreserved(t *testing.T, before, after []byte) {
	t.Helper()
	prior := gjson.GetBytes(before, "input").Array()
	adapted := gjson.GetBytes(after, "input").Array()
	require.Len(t, adapted, len(prior))
	for index, item := range prior {
		if item.Get("type").String() == "compaction" {
			require.Equal(t, "message", adapted[index].Get("type").String())
			require.Contains(t, adapted[index].Get("content.0.text").String(), "verified retained source summary")
			continue
		}
		require.JSONEq(t, item.Raw, adapted[index].Raw, "retained item %d", index)
	}
	for _, field := range []string{"model", "reasoning", "prompt_cache_key", "client_metadata", "store"} {
		require.JSONEq(t, gjson.GetBytes(before, field).Raw, gjson.GetBytes(after, field).Raw, field)
	}
}

func TestResponsesRejectedCompactionHonorsBudgetAndMissingHistory(t *testing.T) {
	for _, retries := range []int{0, 1, 2} {
		t.Run(strconv.Itoa(retries)+" retries", func(t *testing.T) {
			req := rejectedCompactionPipelineRequest(t, llm.APIFormatOpenAIResponse)
			original := append([]byte(nil), req.Body...)
			adapter := newRemoteCompactionAdapter(nil, nil, nil)
			executor := &responsesReasoningPipelineExecutor{failures: []error{resourceMismatchHTTPError(), resourceMismatchHTTPError()}}
			_, result, err := runRejectedReasoningPipeline(t, shared.WithSessionScope(t.Context(), "budget-owner"), req, executor, "budget-credential", true, retries,
				func(state *PersistenceState, outbound *PersistentOutboundTransformer) pipeline.Middleware {
					state.APIKey = &ent.APIKey{ID: 203, ProjectID: 204}
					return recoverRejectedRemoteCompaction(outbound, adapter, executor)
				})
			require.Error(t, err)
			require.Nil(t, result)
			require.Len(t, executor.requests, min(retries+1, 2))
			require.Equal(t, original, req.Body)
			for _, attempt := range executor.requests {
				require.Equal(t, "native-checkpoint-ciphertext", gjson.GetBytes(attempt.Body, "input.2.encrypted_content").String())
			}
			if retries == 2 {
				require.ErrorContains(t, err, "request history is unavailable")
			}
		})
	}
}

func TestResponsesRejectedCompactionRecoveryScope(t *testing.T) {
	for _, changed := range []string{"credential", "url", "model", "thread", "window", "owner", "api key", "project", "ciphertext", "channel"} {
		t.Run(changed, func(t *testing.T) {
			ctx := shared.WithSessionScope(t.Context(), "scope-owner")
			outbound := newCodexResponsesPassThroughOutbound()
			outbound.state.APIKey = &ent.APIKey{ID: 205, ProjectID: 206}
			req := rejectedCompactionPipelineRequest(t, llm.APIFormatOpenAIResponse)
			req.URL = "https://example.invalid/v1/responses"
			req.Headers.Set("Authorization", "Bearer scope-credential")
			outbound.state.RawRequest = cloneRawRequest(req)
			stripped, _, err := stripResponsesRejectedStatus(req.Body, []responsesRejectedStatusRule{{index: -1, field: "id"}})
			require.NoError(t, err)
			req.Body = stripped
			outbound.state.RawProviderRequest = req
			adapter := newRemoteCompactionAdapter(nil, nil, nil)
			middleware := recoverRejectedRemoteCompaction(outbound, adapter, &responsesReasoningPipelineExecutor{})
			middleware.OnOutboundRawError(ctx, resourceMismatchHTTPError())
			require.True(t, hasResponsesRejectedStatusCompatibilityRetry(outbound.state, outbound.GetCurrentChannel().ID))
			switch changed {
			case "credential":
				req.Headers.Set("Authorization", "Bearer other-credential")
			case "url":
				req.URL = "https://another.invalid/v1/responses"
			case "model":
				req.Body, err = sjson.SetBytes(req.Body, "model", "gpt-5.6-sol")
			case "thread":
				req.Headers.Set("Thread-Id", "other-thread")
				req.Body, err = sjson.SetBytes(req.Body, "client_metadata.thread_id", "other-thread")
			case "window":
				req.Headers.Set("X-Codex-Window-Id", "recovery-thread:2")
			case "owner":
				ctx = shared.WithSessionScope(ctx, "other-owner")
			case "api key":
				outbound.state.APIKey = &ent.APIKey{ID: 207, ProjectID: 206}
			case "project":
				outbound.state.APIKey = &ent.APIKey{ID: 205, ProjectID: 208}
			case "ciphertext":
				req.Body, err = sjson.SetBytes(req.Body, "input.2.encrypted_content", "substituted-ciphertext")
			case "channel":
				outbound.state.CurrentCandidate.Channel.ID++
			}
			require.NoError(t, err)
			before := append([]byte(nil), req.Body...)
			got, err := middleware.OnOutboundRawRequest(ctx, req)
			require.NoError(t, err)
			require.Equal(t, before, got.Body)
		})
	}
}

func TestResponsesRejectedCompactionDoesNotRetryPartialStream(t *testing.T) {
	req := rejectedCompactionPipelineRequest(t, llm.APIFormatOpenAIResponse)
	executor := &responsesReasoningPipelineExecutor{events: []*httpclient.StreamEvent{
		{Type: "response.created", Data: []byte(`{"type":"response.created","response":{"id":"resp_partial","status":"in_progress","output":[]}}`)},
		{Type: "response.output_item.added", Data: []byte(`{"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"msg_partial","role":"assistant","content":[]}}`)},
		{Type: "response.content_part.added", Data: []byte(`{"type":"response.content_part.added","output_index":0,"content_index":0,"part":{"type":"output_text","text":""}}`)},
		{Type: "response.output_text.delta", Data: []byte(`{"type":"response.output_text.delta","output_index":0,"content_index":0,"delta":"visible output"}`)},
		{Type: "error", Data: []byte(`{"type":"error","status_code":400,"error":{"type":"invalid_request_error","message":"` + responsesResourceMismatchMessage + `"}}`)},
	}}
	_, result, err := runRejectedReasoningPipeline(t, shared.WithSessionScope(t.Context(), "partial-owner"), req, executor, "partial-credential", true, 2,
		func(state *PersistenceState, outbound *PersistentOutboundTransformer) pipeline.Middleware {
			state.APIKey = &ent.APIKey{ID: 209, ProjectID: 210}
			return recoverRejectedRemoteCompaction(outbound, newRemoteCompactionAdapter(nil, nil, nil), executor)
		})
	require.NoError(t, err)
	require.NotNil(t, result)
	for result.EventStream.Next() {
		_ = result.EventStream.Current()
	}
	require.Error(t, result.EventStream.Err())
	_ = result.EventStream.Close()
	require.Len(t, executor.requests, 1, "a returned partial response must never restart through compaction recovery")
}

func TestResponsesRejectedCompactionPreparationDoesNotRepeatBridgeBudget(t *testing.T) {
	outbound := newCodexResponsesPassThroughOutbound()
	require.True(t, outbound.CanRetry(bridgeOverloadError()))
	require.False(t, outbound.CanRetry(&remoteCompactionPreparationError{cause: bridgeOverloadError()}))
}

func TestResponsesRejectedCompactionSourceRequiresExactSuccessfulReference(t *testing.T) {
	for _, scenario := range []string{"matching", "different ciphertext", "failed execution", "other owner"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, client, apiKey, adapter := rejectedCompactionStorage(t)
			status := requestexecution.StatusCompleted
			ciphertext := "native-checkpoint-ciphertext"
			if scenario == "different ciphertext" {
				ciphertext = "different-checkpoint-ciphertext"
			}
			if scenario == "failed execution" {
				status = requestexecution.StatusFailed
			}
			seedRejectedCompactionSource(t, ctx, client, apiKey, status, ciphertext)
			if scenario == "other owner" {
				apiKey = &ent.APIKey{ID: apiKey.ID + 1, ProjectID: apiKey.ProjectID}
			}
			ref, _, _, err := parseRemoteCompactionRequest([]byte(responsesCompactionHistoryFixture))
			require.NoError(t, err)
			summary, source, err := adapter.findStoredSummaryOrSource(ctx, remoteCompactionCacheKey(ref), ref, "recovery-thread", &PersistenceState{APIKey: apiKey})
			require.NoError(t, err)
			require.Empty(t, summary)
			if scenario == "matching" {
				require.NotNil(t, source)
				require.Equal(t, "complete source history", gjson.GetBytes(source.body, "input.0.content").String())
			} else {
				require.Nil(t, source)
			}
		})
	}
}

func rejectedCompactionStorage(t *testing.T) (context.Context, *ent.Client, *ent.APIKey, *remoteCompactionAdapter) {
	t.Helper()
	client := enttest.NewEntClient(t, "sqlite3", "file:rejected-native-compaction?mode=memory&_fk=0")
	t.Cleanup(func() { client.Close() })
	ctx := authz.WithTestBypass(ent.NewContext(t.Context(), client))
	apiKey := &ent.APIKey{ID: 211, ProjectID: 212}
	ctx = contexts.WithProjectID(contexts.WithAPIKey(ctx, apiKey), apiKey.ProjectID)
	ctx = shared.WithSessionScope(ctx, "owner-"+t.Name())
	_, err := client.DataStorage.Create().SetName("primary").SetDescription("synthetic storage").SetPrimary(true).SetType("database").SetSettings(new(objects.DataStorageSettings)).Save(ctx)
	require.NoError(t, err)
	service := createTestRequestService(t, client)
	system := biz.NewSystemService(biz.SystemServiceParams{Ent: client})
	return ctx, client, apiKey, newRemoteCompactionAdapter(service, nil, system)
}

func seedRejectedCompactionSource(t *testing.T, ctx context.Context, client *ent.Client, apiKey *ent.APIKey, status requestexecution.Status, ciphertext string) {
	t.Helper()
	source := []byte(`{"model":"gpt-6-astra","client_metadata":{"thread_id":"recovery-thread"},"input":[{"type":"message","role":"user","content":"complete source history"},{"type":"compaction_trigger"}]}`)
	stored, err := client.Request.Create().SetProjectID(apiKey.ProjectID).SetAPIKeyID(apiKey.ID).
		SetModelID("gpt-6-astra").SetFormat(string(llm.APIFormatOpenAIResponse)).SetStatus(request.StatusCompleted).
		SetRequestHeaders(objects.JSONRawMessage(`{"Thread-Id":["recovery-thread"]}`)).SetRequestBody(source).Save(ctx)
	require.NoError(t, err)
	output, err := json.Marshal(map[string]any{"status": "completed", "output": []map[string]string{{"type": "compaction", "id": "cmp_native", "encrypted_content": ciphertext}}})
	require.NoError(t, err)
	_, err = client.RequestExecution.Create().SetRequestID(stored.ID).SetProjectID(apiKey.ProjectID).
		SetModelID("gpt-6-astra").SetFormat(string(llm.APIFormatOpenAIResponse)).SetStatus(status).
		SetRequestBody(source).SetResponseBody(output).Save(ctx)
	require.NoError(t, err)
}

func TestResponsesRejectedCompactionGeneratesFromStoredHistory(t *testing.T) {
	ctx, client, apiKey, adapter := rejectedCompactionStorage(t)
	seedRejectedCompactionSource(t, ctx, client, apiKey, requestexecution.StatusCompleted, "native-checkpoint-ciphertext")
	contexts.WithChannelAPIKey(ctx, "parent-context-credential")
	executor := &rejectedCompactionBridgeExecutor{}
	req := rejectedCompactionPipelineRequest(t, llm.APIFormatOpenAIResponse)
	_, result, err := runRejectedReasoningPipeline(t, ctx, req, executor, "native-bridge-credential", true, 2,
		func(state *PersistenceState, outbound *PersistentOutboundTransformer) pipeline.Middleware {
			state.APIKey = apiKey
			return recoverRejectedRemoteCompaction(outbound, adapter, executor)
		})
	require.NoError(t, err)
	drainRejectedReasoningPipeline(t, result)
	require.Len(t, executor.continuations, 3)
	require.Len(t, executor.summaries, 1)
	assertRejectedCompactionWindowPreserved(t, executor.continuations[1], executor.continuations[2])
	require.Equal(t, "complete source history", gjson.GetBytes(executor.summaries[0], "input.0.content").String())
	key, ok := contexts.GetChannelAPIKey(ctx)
	require.True(t, ok)
	require.NotEqual(t, "bridge-context-only", key)
	stored, err := client.Request.Query().Where(request.StatusEQ(request.StatusCompleted)).All(ctx)
	require.NoError(t, err)
	require.Len(t, stored, 2, "source and bridge summary must both remain available")
	for _, row := range stored {
		require.Equal(t, apiKey.ID, row.APIKeyID)
		require.Equal(t, apiKey.ProjectID, row.ProjectID)
	}
}

type rejectedCompactionBridgeExecutor struct {
	continuations [][]byte
	summaries     [][]byte
}

func (e *rejectedCompactionBridgeExecutor) Do(context.Context, *httpclient.Request) (*httpclient.Response, error) {
	return nil, resourceMismatchHTTPError()
}

func (e *rejectedCompactionBridgeExecutor) DoStream(ctx context.Context, req *httpclient.Request) (streams.Stream[*httpclient.StreamEvent], error) {
	isSummary := false
	for _, item := range gjson.GetBytes(req.Body, "input").Array() {
		isSummary = isSummary || item.Get("content.0.text").String() == localCompactionPrompt
	}
	if isSummary {
		e.summaries = append(e.summaries, append([]byte(nil), req.Body...))
		contexts.WithChannelAPIKey(ctx, "bridge-context-only")
		return streams.SliceStream([]*httpclient.StreamEvent{
			{Type: "response.created", Data: []byte(`{"type":"response.created","response":{"id":"resp_bridge","object":"response","status":"in_progress","model":"gpt-6-astra","output":[]}}`)},
			{Type: "response.output_item.added", Data: []byte(`{"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"msg_bridge","role":"assistant","status":"in_progress","content":[]}}`)},
			{Type: "response.output_item.done", Data: []byte(`{"type":"response.output_item.done","output_index":0,"item":{"type":"message","id":"msg_bridge","role":"assistant","status":"completed","content":[{"type":"output_text","text":"verified retained source summary"}]}}`)},
			{Type: "response.completed", Data: []byte(`{"type":"response.completed","response":{"id":"resp_bridge","object":"response","status":"completed","model":"gpt-6-astra","output":[{"type":"message","id":"msg_bridge","role":"assistant","content":[{"type":"output_text","text":"verified retained source summary"}]}]}}`)},
		}), nil
	}
	e.continuations = append(e.continuations, append([]byte(nil), req.Body...))
	if gjson.GetBytes(req.Body, `input.#(type=="compaction")`).Exists() {
		return nil, resourceMismatchHTTPError()
	}
	return streams.SliceStream(rejectedReasoningCompactionEvents()), nil
}
