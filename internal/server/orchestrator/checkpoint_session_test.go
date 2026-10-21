package orchestrator

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
)

func TestCheckpointPortableSessionSurvivesRestartAndSeparatesThreads(t *testing.T) {
	ctx, _, state, adapter := unifiedHistoryFixture(t)
	state.RawRequest = rejectedCompactionPipelineRequest(t, llm.APIFormatOpenAIResponse)
	state.CurrentCandidate.Channel.UpdatedAt = time.Now()
	portable, err := adapter.usesPortableCheckpoint(ctx, state, state.CurrentCandidate)
	require.NoError(t, err)
	require.False(t, portable)
	require.NoError(t, adapter.requirePortableCheckpoint(ctx, state))
	adapter = newRemoteCompactionAdapter(nil, nil, adapter.systemService)
	portable, err = adapter.usesPortableCheckpoint(ctx, state, state.CurrentCandidate)
	require.NoError(t, err)
	require.True(t, portable)
	state.RawRequest.Headers.Set("Thread-Id", "independent-thread")
	portable, err = adapter.usesPortableCheckpoint(ctx, state, state.CurrentCandidate)
	require.NoError(t, err)
	require.False(t, portable)
	state.RawRequest.Headers.Set("Thread-Id", "recovery-thread")
	state.CurrentCandidate.Channel.UpdatedAt = state.CurrentCandidate.Channel.UpdatedAt.Add(time.Second)
	portable, err = adapter.usesPortableCheckpoint(ctx, state, state.CurrentCandidate)
	require.NoError(t, err)
	require.False(t, portable, "an operator's channel change establishes a new contract")
}

func TestCheckpointMigrationKeepsPortableSummaryInsteadOfItsArchive(t *testing.T) {
	ctx, _, state, adapter := unifiedHistoryFixture(t)
	ref, err := newLocalCompactionReference()
	require.NoError(t, err)
	cipher, err := adapter.localCompactionCipher(ctx)
	require.NoError(t, err)
	aad, err := localCompactionAssociatedData(ref, state)
	require.NoError(t, err)
	generation := &localCompactionGeneration{ref: ref, summaryCipher: cipher, associatedData: aad}
	require.NoError(t, generation.sealSummary("keep the verified checkpoint and pending correction 17.250"))
	archive := []byte(`{"model":"gpt-6-astra","input":[{"role":"user","content":"old transcript must not reappear"},{"type":"compaction_trigger"}]}`)
	require.NoError(t, adapter.retainNativeCompactionSource(ctx, ref, state, &remoteCompactionSource{body: archive}))
	body, err := json.Marshal(map[string]any{"model": "gpt-6-astra", "input": []any{
		map[string]string{"type": "compaction", "id": ref.ID, "encrypted_content": ref.EncryptedContent},
		map[string]string{"type": "message", "role": "user", "content": "latest correction 17.375"},
		map[string]string{"type": "compaction_trigger"},
	}})
	require.NoError(t, err)
	source, err := adapter.restoreCheckpointWindow(ctx, &remoteCompactionSource{body: body}, state)
	require.NoError(t, err)
	require.Contains(t, string(source.body), "17.250")
	require.Contains(t, string(source.body), "17.375")
	require.NotContains(t, string(source.body), "old transcript must not reappear")
	stored, err := adapter.loadNativeCompactionSource(ctx, ref, state)
	require.NoError(t, err)
	require.JSONEq(t, string(archive), string(stored.body))
}

func TestCheckpointResourceRecoveryGeneratesOnceAndKeepsShortWindow(t *testing.T) {
	ctx, _, state, adapter := unifiedHistoryFixture(t)
	state.RawRequest = rejectedCompactionPipelineRequest(t, llm.APIFormatOpenAIResponse)
	state.RawProviderRequest = cloneRawRequest(state.RawRequest)
	state.RawProviderRequest.URL = "https://example.invalid/v1/responses"
	state.RawProviderRequest.APIFormat = string(llm.APIFormatOpenAIResponse)
	state.RawProviderRequest.Headers.Set("Authorization", "Bearer synthetic-checkpoint-key")
	ref, _, _, err := parseRemoteCompactionRequest(state.RawProviderRequest.Body)
	require.NoError(t, err)
	text := strings.Repeat("completed tool observation ", 8000)
	source, err := json.Marshal(map[string]any{"model": "gpt-6-astra", "input": []any{
		map[string]string{"type": "message", "role": "user", "content": "preserve the active task"},
		map[string]string{"type": "message", "role": "assistant", "content": text},
		map[string]string{"type": "compaction_trigger"},
	}})
	require.NoError(t, err)
	require.NoError(t, adapter.retainNativeCompactionSource(ctx, ref, state, &remoteCompactionSource{body: source}))
	executor := &rejectedCompactionBridgeExecutor{}
	middleware := &responsesCompactionRecoveryMiddleware{adapter: adapter, executor: executor, outbound: &PersistentOutboundTransformer{state: state}}
	original := append([]byte(nil), state.RawProviderRequest.Body...)
	middleware.OnOutboundRawError(ctx, resourceMismatchHTTPError())
	got, err := middleware.OnOutboundRawRequest(ctx, state.RawProviderRequest)
	require.NoError(t, err)
	require.Len(t, executor.summaries, 1)
	require.Empty(t, executor.continuations, "preparation must not retry the rejected native state")
	assertRejectedCompactionWindowPreserved(t, original, got.Body)
	require.Less(t, len(got.Body), len(source)/10, "successful recovery must preserve a compact window")
	require.False(t, gjson.GetBytes(got.Body, `input.#(type=="compaction")`).Exists())
	first := append([]byte(nil), got.Body...)
	adapter = newRemoteCompactionAdapter(adapter.requestService, nil, adapter.systemService)
	state.RawProviderRequest.Body = original
	middleware = &responsesCompactionRecoveryMiddleware{adapter: adapter, executor: executor, outbound: &PersistentOutboundTransformer{state: state}}
	got, err = middleware.OnOutboundRawRequest(ctx, state.RawProviderRequest)
	require.NoError(t, err)
	require.Len(t, executor.summaries, 1, "a restart must reuse the durable checkpoint summary")
	require.JSONEq(t, string(first), string(got.Body))
	_, err = middleware.OnOutboundRawResponse(ctx, &httpclient.Response{StatusCode: http.StatusOK, Body: []byte(`{"status":"completed","output":[]}`)})
	require.NoError(t, err)
}
