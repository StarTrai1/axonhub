package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/pipeline"
)

func TestNativeCompactionExactReplay(t *testing.T) {
	source := []byte(`{"model":"gpt-6-astra","input":[{"role":"user","content":"first"},{"type":"function_call","call_id":"call_1","name":"inspect","arguments":"{}"},{"type":"function_call_output","call_id":"call_1","output":"unabridged result"},{"role":"user","content":"retained"},{"type":"compaction_trigger"}]}`)
	body := []byte(`{"model":"gpt-6-astra","prompt_cache_key":"stable","reasoning":{"effort":"xhigh"},"input":[{"role":"user","content":"retained"},{"type":"compaction","id":"cmp_exact","encrypted_content":"native-exact"},{"role":"user","content":"continue"}]}`)
	ref, _, _, err := parseRemoteCompactionRequest(body)
	require.NoError(t, err)
	restored, ok, err := spliceNativeCompactionSource(body, source, ref, 65536)
	require.NoError(t, err)
	require.True(t, ok)
	_, original, err := decodeResponsesInput(source)
	require.NoError(t, err)
	_, input, err := decodeResponsesInput(restored)
	require.NoError(t, err)
	require.Len(t, input, 5)
	for i := range original[:4] {
		require.JSONEq(t, string(original[i]), string(input[i]))
	}
	require.Equal(t, "continue", gjson.GetBytes(input[4], "content").String())
	require.Equal(t, "stable", gjson.GetBytes(restored, "prompt_cache_key").String())
	require.Equal(t, "xhigh", gjson.GetBytes(restored, "reasoning.effort").String())
	require.Equal(t, 1, bytes.Count(restored, []byte(`"retained"`)))
	for _, change := range []struct {
		path  string
		value any
	}{
		{"input.3.content", "different branch"},
		{"input.4", map[string]any{"role": "user", "content": "retained"}},
		{"input.1.encrypted_function_args", []string{"arguments"}},
		{"input.2.call_id", "unmatched_call"},
		{"input.2.output", []map[string]string{{"type": "input_image", "image_url": "https://example.invalid/a.png"}}},
		{"model", "different-model"},
		{"instructions", "new hidden source instruction"},
	} {
		t.Run(change.path+"/"+string(mustReplayJSON(t, change.value)), func(t *testing.T) {
			changed, err := sjson.SetBytes(source, change.path, change.value)
			require.NoError(t, err)
			_, ok, err := spliceNativeCompactionSource(body, changed, ref, 65536)
			require.NoError(t, err)
			require.False(t, ok)
		})
	}
	_, ok, err = spliceNativeCompactionSource(body, source, ref, 64)
	require.NoError(t, err)
	require.False(t, ok)
}

func mustReplayJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	return encoded
}

func TestNativeCompactionExactReplayAuthenticatedChain(t *testing.T) {
	ctx, _, owner, adapter := rejectedCompactionStorage(t)
	state := &PersistenceState{APIKey: owner, CurrentCandidate: &ChannelModelsCandidate{ContextTokens: 128000}}
	ref := &remoteCompactionReference{ID: "cmp_exact", EncryptedContent: "native-exact"}
	first := &remoteCompactionReference{ID: "cmp_first", EncryptedContent: "native-first"}
	firstBody := []byte(`{"model":"gpt-6-astra","input":[{"role":"user","content":"first"},{"type":"compaction_trigger"}]}`)
	secondBody := []byte(`{"model":"gpt-6-astra","input":[{"type":"compaction","id":"cmp_first","encrypted_content":"native-first"},{"role":"user","content":"second"},{"type":"compaction_trigger"}]}`)
	require.NoError(t, adapter.retainNativeCompactionSource(ctx, first, state, &remoteCompactionSource{body: firstBody}))
	require.NoError(t, adapter.retainNativeCompactionSource(ctx, ref, state, &remoteCompactionSource{body: secondBody}))
	body := []byte(`{"model":"gpt-6-astra","input":[{"type":"compaction","id":"cmp_exact","encrypted_content":"native-exact"},{"role":"user","content":"continue"}]}`)
	ref, _, _, err := parseRemoteCompactionRequest(body)
	require.NoError(t, err)
	for range 2 {
		adapter = newRemoteCompactionAdapter(adapter.requestService, nil, adapter.systemService)
		got, ok, err := adapter.replayNativeCompaction(ctx, body, ref, state)
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, []string{"first", "second", "continue"}, []string{gjson.GetBytes(got, "input.0.content").String(), gjson.GetBytes(got, "input.1.content").String(), gjson.GetBytes(got, "input.2.content").String()})
	}
	foreign := *state
	foreign.APIKey = &ent.APIKey{ID: owner.ID + 1, ProjectID: owner.ProjectID}
	_, ok, err := adapter.replayNativeCompaction(ctx, body, ref, &foreign)
	require.NoError(t, err)
	require.False(t, ok)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, _, err = adapter.replayNativeCompaction(canceled, body, ref, state)
	require.ErrorIs(t, err, context.Canceled)
}

func TestResponsesCompactionExactReplayPipeline(t *testing.T) {
	for _, passThrough := range []bool{true, false} {
		t.Run(map[bool]string{true: "raw", false: "converted"}[passThrough], func(t *testing.T) {
			ctx, _, owner, adapter := rejectedCompactionStorage(t)
			req := rejectedCompactionPipelineRequest(t, llm.APIFormatOpenAIResponse)
			req.Body = []byte(`{"model":"gpt-6-astra","stream":true,"client_metadata":{"thread_id":"recovery-thread"},"input":[{"role":"user","content":"first"},{"type":"compaction","id":"cmp_exact","encrypted_content":"native-exact"},{"role":"user","content":"continue"}]}`)
			ref, _, _, err := parseRemoteCompactionRequest(req.Body)
			require.NoError(t, err)
			source := &remoteCompactionSource{body: []byte(`{"model":"gpt-6-astra","input":[{"role":"user","content":"first"},{"role":"assistant","content":"complete source detail"},{"type":"compaction_trigger"}]}`)}
			require.NoError(t, adapter.retainNativeCompactionSource(ctx, ref, &PersistenceState{APIKey: owner}, source))
			executor := &responsesReasoningPipelineExecutor{failures: []error{resourceMismatchHTTPError()}, events: rejectedReasoningCompactionEvents()}
			configure := func(state *PersistenceState, outbound *PersistentOutboundTransformer) pipeline.Middleware {
				state.APIKey = owner
				state.ChannelModelsCandidates[0].ContextTokens = 128000
				return recoverRejectedRemoteCompaction(outbound, adapter, executor)
			}
			_, result, err := runRejectedReasoningPipeline(t, ctx, req, executor, "replay-credential", passThrough, 1, configure)
			require.NoError(t, err)
			drainRejectedReasoningPipeline(t, result)
			require.Len(t, executor.requests, 2)
			require.NotContains(t, string(executor.requests[1].Body), "CONTEXT CHECKPOINT COMPACTION")
			require.Contains(t, string(executor.requests[1].Body), "complete source detail")
			require.NotContains(t, string(executor.requests[1].Body), "native-exact")
			require.Eventually(t, func() bool { return adapter.recoveries.Len() == 1 }, time.Second, time.Millisecond)
			warm := executor.requests[1].Body
			executor = &responsesReasoningPipelineExecutor{events: rejectedReasoningCompactionEvents()}
			_, result, err = runRejectedReasoningPipeline(t, ctx, req, executor, "replay-credential", passThrough, 1, configure)
			require.NoError(t, err)
			drainRejectedReasoningPipeline(t, result)
			require.Len(t, executor.requests, 1)
			require.JSONEq(t, string(warm), string(executor.requests[0].Body), "confirmed replay must keep a stable prefix")
		})
	}
}
