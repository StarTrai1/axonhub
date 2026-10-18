package orchestrator

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/request"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/streams"
)

const deltaCompactionSource = `{"model":"gpt-6-astra","previous_response_id":"resp_delta","reasoning":{"effort":"xhigh"},"client_metadata":{"thread_id":"recovery-thread","x-codex-turn-metadata":"{\"request_kind\":\"compaction\",\"compaction\":{\"implementation\":\"responses_compaction_v2\"}}"},"input":[{"type":"custom_tool_call_output","call_id":"call_second","output":"second exact result"},{"type":"compaction_trigger"}]}`

func seedCompactionPredecessor(t *testing.T, ctx context.Context, client *ent.Client, owner *ent.APIKey, id, body, output string) *ent.Request {
	t.Helper()
	response := `{"id":"` + id + `","status":"completed","output":` + output + `}`
	row, err := client.Request.Create().SetProjectID(owner.ProjectID).SetAPIKeyID(owner.ID).
		SetModelID("gpt-6-astra").SetFormat(string(llm.APIFormatOpenAIResponseWebSocket)).
		SetStatus(request.StatusCompleted).SetExternalID(id).
		SetRequestBody([]byte(body)).SetResponseBody([]byte(response)).Save(ctx)
	require.NoError(t, err)
	return row
}

func seedCompactionPredecessorChain(t *testing.T, ctx context.Context, client *ent.Client, owner *ent.APIKey) {
	t.Helper()
	seedCompactionPredecessor(t, ctx, client, owner, "resp_root",
		`{"model":"gpt-6-astra","input":[{"role":"user","content":"original unabridged task"}]}`,
		`[{"type":"custom_tool_call","id":"ct_first","call_id":"call_first","name":"inspect","input":"first exact arguments"}]`)
	seedCompactionPredecessor(t, ctx, client, owner, "resp_delta",
		`{"model":"gpt-6-astra","previous_response_id":"resp_root","input":[{"type":"custom_tool_call_output","call_id":"call_first","output":"first exact result"}]}`,
		`[{"type":"reasoning","id":"rs_original","encrypted_content":"opaque-original-reasoning","summary":[]},{"type":"custom_tool_call","id":"ct_second","call_id":"call_second","name":"inspect","input":"second exact arguments"}]`)
}

func assertResolvedCompactionSource(t *testing.T, body []byte) {
	t.Helper()
	require.Empty(t, gjson.GetBytes(body, "previous_response_id").String(), "durable compaction sources must not contain connection-local references")
	items := gjson.GetBytes(body, "input").Array()
	require.Len(t, items, 7)
	require.Equal(t, "original unabridged task", items[0].Get("content").String())
	require.Equal(t, "first exact arguments", items[1].Get("input").String())
	require.Equal(t, "call_first", items[2].Get("call_id").String())
	require.Equal(t, "first exact result", items[2].Get("output").String())
	require.Equal(t, "opaque-original-reasoning", items[3].Get("encrypted_content").String())
	require.Equal(t, "second exact arguments", items[4].Get("input").String())
	require.Equal(t, "call_second", items[5].Get("call_id").String())
	require.Equal(t, "second exact result", items[5].Get("output").String())
	require.Equal(t, "compaction_trigger", items[6].Get("type").String())
	require.Equal(t, "xhigh", gjson.GetBytes(body, "reasoning.effort").String())
}

func TestResponsesRejectedNativeSourceDeltaBeforePublication(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "response", true: "stream"}[stream], func(t *testing.T) {
			ctx, client, owner, adapter := rejectedCompactionStorage(t)
			seedCompactionPredecessorChain(t, ctx, client, owner)
			state := &PersistenceState{APIKey: owner, RawProviderRequest: &httpclient.Request{Body: []byte(deltaCompactionSource), APIFormat: string(llm.APIFormatOpenAIResponseWebSocket)}}
			middleware := retainNativeCompactionSources(&PersistentOutboundTransformer{state: state}, adapter)
			if stream {
				event := &httpclient.StreamEvent{Data: []byte(`{"type":"response.output_item.done","item":{"type":"compaction","id":"cmp_durable","encrypted_content":"native-ciphertext"}}`)}
				wrapped, err := middleware.OnOutboundRawStream(ctx, streams.SliceStream([]*httpclient.StreamEvent{event}))
				require.NoError(t, err)
				require.True(t, wrapped.Next())
				require.Same(t, event, wrapped.Current())
				require.NoError(t, wrapped.Err())
				require.NoError(t, wrapped.Close())
			} else {
				_, err := middleware.OnOutboundRawResponse(ctx, &httpclient.Response{StatusCode: 200, Body: []byte(nativeSourceResponse)})
				require.NoError(t, err)
			}
			_, err := client.Request.Delete().Exec(ctx)
			require.NoError(t, err)
			restarted := newRemoteCompactionAdapter(nil, nil, biz.NewSystemService(biz.SystemServiceParams{Ent: client}))
			source, err := restarted.loadNativeCompactionSource(ctx, &remoteCompactionReference{ID: "cmp_durable", EncryptedContent: "native-ciphertext"}, state)
			require.NoError(t, err)
			require.NotNil(t, source)
			assertResolvedCompactionSource(t, source.body)
		})
	}
}

// Write the legacy wire representation directly, without the new retention
// normalization, to reproduce checkpoints already created by older releases.
func seedLegacyDeltaCompactionSource(t *testing.T, ctx context.Context, adapter *remoteCompactionAdapter, ref *remoteCompactionReference, state *PersistenceState) {
	t.Helper()
	encoded, err := json.Marshal(nativeCompactionSourceCheckpoint{Version: 1, Body: json.RawMessage(deltaCompactionSource)})
	require.NoError(t, err)
	compressor, err := zstd.NewWriter(nil)
	require.NoError(t, err)
	compressed := compressor.EncodeAll(encoded, nil)
	require.NoError(t, compressor.Close())
	aead, err := adapter.localCompactionCipher(ctx)
	require.NoError(t, err)
	aad, err := nativeCompactionSourceAAD(ref, state)
	require.NoError(t, err)
	sealed, err := sealNativeCompactionSource(aead, aad, compressed)
	require.NoError(t, err)
	require.NoError(t, adapter.systemService.SaveNativeCompactionSource(ctx, state.APIKey.ProjectID, state.APIKey.ID, remoteCompactionCacheKey(ref), sealed))
}

func TestResponsesRejectedNativeSourceLegacyDeltaRepair(t *testing.T) {
	ctx, client, owner, adapter := rejectedCompactionStorage(t)
	seedCompactionPredecessorChain(t, ctx, client, owner)
	state := &PersistenceState{APIKey: owner}
	ref := &remoteCompactionReference{ID: "cmp_legacy_delta", EncryptedContent: "legacy-source-cipher"}
	seedLegacyDeltaCompactionSource(t, ctx, adapter, ref, state)
	source, err := adapter.loadNativeCompactionSource(ctx, ref, state)
	require.NoError(t, err)
	assertResolvedCompactionSource(t, source.body)
	_, err = client.Request.Delete().Exec(ctx)
	require.NoError(t, err)
	restarted := newRemoteCompactionAdapter(nil, nil, biz.NewSystemService(biz.SystemServiceParams{Ent: client}))
	source, err = restarted.loadNativeCompactionSource(ctx, ref, state)
	require.NoError(t, err)
	assertResolvedCompactionSource(t, source.body)
}

func TestResponsesRejectedNativeSourceDeltaFailures(t *testing.T) {
	for _, mode := range []string{"missing", "wrong key", "wrong project", "incomplete", "wrong id", "cycle", "thread", "model", "error", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			ctx, client, owner, adapter := rejectedCompactionStorage(t)
			seedCompactionPredecessorChain(t, ctx, client, owner)
			row, err := client.Request.Query().Where(request.ExternalIDEQ("resp_root")).Only(ctx)
			require.NoError(t, err)
			switch mode {
			case "missing":
				require.NoError(t, client.Request.DeleteOne(row).Exec(ctx))
			case "wrong key", "wrong project":
				other := &ent.APIKey{ID: owner.ID, ProjectID: owner.ProjectID}
				if mode == "wrong key" {
					other.ID++
				} else {
					other.ProjectID++
				}
				require.NoError(t, client.Request.DeleteOne(row).Exec(ctx))
				seedCompactionPredecessor(t, ctx, client, other, "resp_root", string(row.RequestBody), gjson.GetBytes(row.ResponseBody, "output").Raw)
			case "incomplete", "wrong id", "error":
				field, value := "status", any("incomplete")
				if mode == "wrong id" {
					field, value = "id", "another-response"
				}
				if mode == "error" {
					field, value = "error", map[string]string{"message": "failed"}
				}
				body, err := sjson.SetBytes(row.ResponseBody, field, value)
				require.NoError(t, err)
				require.NoError(t, row.Update().SetResponseBody(body).Exec(ctx))
			case "cycle", "thread", "model":
				field, value := "previous_response_id", "resp_delta"
				if mode == "thread" {
					field, value = "client_metadata.thread_id", "unrelated-thread"
				}
				if mode == "model" {
					field, value = "model", "different-model"
				}
				body, err := sjson.SetBytes(row.RequestBody, field, value)
				require.NoError(t, err)
				require.NoError(t, client.Request.DeleteOne(row).Exec(ctx))
				seedCompactionPredecessor(t, ctx, client, owner, "resp_root", string(body), gjson.GetBytes(row.ResponseBody, "output").Raw)
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			source, err := adapter.resolveCompactionSourceContinuation(ctx, &remoteCompactionSource{body: []byte(deltaCompactionSource)}, &PersistenceState{APIKey: owner})
			require.Error(t, err)
			require.Nil(t, source)
		})
	}
}

func TestResponsesRejectedNativeSourceDeltaKeepsSessionCertification(t *testing.T) {
	ctx, client, owner, adapter := rejectedCompactionStorage(t)
	seedCompactionPredecessorChain(t, ctx, client, owner)
	_, _, found, err := adapter.requestService.LoadCompletedResponsesSession(ctx, "resp_delta")
	require.NoError(t, err)
	require.False(t, found, "a delta is still not a portable ordinary session snapshot")
	other := contexts.WithProjectID(contexts.WithAPIKey(ctx, &ent.APIKey{ID: owner.ID + 1, ProjectID: owner.ProjectID}), owner.ProjectID)
	_, _, found, err = adapter.requestService.LoadCompletedResponsesCompactionPredecessor(other, "resp_delta")
	require.NoError(t, err)
	require.False(t, found)
}

func TestNativeCompactionMigrationLegacyDeltaContinuation(t *testing.T) {
	ctx, client, owner, adapter := rejectedCompactionStorage(t)
	seedCompactionPredecessorChain(t, ctx, client, owner)
	req := rejectedCompactionPipelineRequest(t, llm.APIFormatOpenAIResponse)
	ref, _, _, err := parseRemoteCompactionRequest(req.Body)
	require.NoError(t, err)
	seedLegacyDeltaCompactionSource(t, ctx, adapter, ref, &PersistenceState{APIKey: owner})
	executor := &deltaMigrationExecutor{}
	for range 2 {
		_, result, err := runRejectedReasoningPipeline(t, ctx, req, executor, "delta-migration-credential", true, 2, func(state *PersistenceState, outbound *PersistentOutboundTransformer) pipeline.Middleware {
			state.APIKey = owner
			return recoverRejectedRemoteCompaction(outbound, adapter, executor)
		})
		require.NoError(t, err)
		drainRejectedReasoningPipeline(t, result)
	}
	require.Equal(t, 1, executor.nativeCalls, "the repaired source generates one reusable destination checkpoint")
	require.Empty(t, executor.summaries)
	assertResolvedCompactionSource(t, executor.nativeBody)
}

type deltaMigrationExecutor struct {
	rejectedCompactionBridgeExecutor
	nativeCalls int
	nativeBody  []byte
}

func (e *deltaMigrationExecutor) DoStream(ctx context.Context, req *httpclient.Request) (streams.Stream[*httpclient.StreamEvent], error) {
	if gjson.GetBytes(req.Body, `input.#(type=="compaction_trigger")`).Exists() {
		e.nativeCalls++
		e.nativeBody = append([]byte(nil), req.Body...)
		return streams.SliceStream(rejectedReasoningCompactionEvents()), nil
	}
	if gjson.GetBytes(req.Body, `input.#(id=="cmp_native")`).Exists() {
		return nil, rejectedNativeCheckpointError()
	}
	if gjson.GetBytes(req.Body, `input.#(id=="cmp_output")`).Exists() {
		return streams.SliceStream([]*httpclient.StreamEvent{{Type: "response.completed", Data: []byte(`{"type":"response.completed","response":{"id":"resp_delta_continued","object":"response","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"continued"}]}]}}`)}}), nil
	}
	return e.rejectedCompactionBridgeExecutor.DoStream(ctx, req)
}
