package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
)

func TestUnifiedHistoryLegacyChainWithReinsertedContextAndImages(t *testing.T) {
	ctx, _, state, adapter := unifiedHistoryFixture(t)
	state.CurrentCandidate.ContextTokens = 0 // direct channel routing, no models table rows
	image := `{"type":"input_image","image_url":"data:image/png;base64,` + strings.Repeat("AAAA", 80000) + `"}`
	imageUser := `{"type":"message","id":"msg_source","role":"user","content":[{"type":"input_text","text":"calibration 17.125"},` + image + `]}`
	body := []byte(`{"model":"gpt-6-astra","input":[` + imageUser + `,{"type":"function_call","name":"inspect","call_id":"call_initial","arguments":"{}"},{"type":"function_call_output","call_id":"call_initial","output":"original exact calibration evidence"},{"type":"compaction_trigger"}]}`)
	for i := range 3 {
		ref := &remoteCompactionReference{ID: fmt.Sprintf("cmp_chain_%d", i), EncryptedContent: "native-cipher"}
		require.NoError(t, seedLegacyNativeCompactionSource(ctx, adapter, ref, state, &remoteCompactionSource{body: body}))
		body = []byte(fmt.Sprintf(`{"model":"gpt-6-astra","input":[%s,{"type":"message","id":"runtime_%d","role":"developer","content":"current runtime constraint %d"},{"type":"compaction","id":%q,"encrypted_content":%q},{"role":"user","content":"correction %d"},{"type":"compaction_trigger"}]}`, strings.ReplaceAll(imageUser, "msg_source", "msg_reassigned"), i, i, ref.ID, ref.EncryptedContent, i))
	}
	executor := &compactionEvidenceExecutor{}
	compiled, ok, err := adapter.compileUnifiedContext(ctx, body, state, executor)
	require.NoError(t, err)
	require.True(t, ok)
	require.Empty(t, executor.summaries, "complete inline images do not force a text summary")
	require.Contains(t, string(compiled), image)
	require.Equal(t, 1, strings.Count(string(compiled), "original exact calibration evidence"))
	require.Equal(t, 1, strings.Count(string(compiled), "data:image/png;base64,"))
	for i := range 3 {
		require.Contains(t, string(compiled), fmt.Sprintf("current runtime constraint %d", i))
		require.Contains(t, string(compiled), fmt.Sprintf("correction %d", i))
	}
	require.False(t, gjson.GetBytes(compiled, `input.#(type=="compaction")`).Exists())
	_, complete := responsesResourceHistorySupportsRecovery(compiled)
	require.True(t, complete)
}

func TestUnifiedHistorySplicePreservesDuplicateMessagesAndRejectsDivergence(t *testing.T) {
	source := []byte(`{"model":"gpt-6-astra","input":[{"role":"user","id":"first","content":"continue"},{"role":"assistant","content":"first evidence"},{"role":"user","id":"second","content":"continue"},{"type":"compaction_trigger"}]}`)
	body := []byte(`{"model":"gpt-6-astra","input":[{"role":"user","content":"continue"},{"role":"user","content":"continue"},{"type":"compaction","id":"cmp_splice","encrypted_content":"cipher"},{"role":"user","content":"new turn"}]}`)
	ref, _, _, err := parseRemoteCompactionRequest(body)
	require.NoError(t, err)
	got, ok, err := spliceUnifiedCompactionSource(body, source, ref)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 2, strings.Count(string(got), `"content":"continue"`))
	require.Contains(t, string(got), "first evidence")
	changed := []byte(strings.Replace(string(body), `{"role":"user","content":"continue"}`, `{"role":"user","id":"first","content":"different branch"}`, 1))
	ref, _, _, err = parseRemoteCompactionRequest(changed)
	require.NoError(t, err)
	_, ok, err = spliceUnifiedCompactionSource(changed, source, ref)
	require.NoError(t, err)
	require.False(t, ok, "conflicting content with the same ID must not be silently accepted")
}

func TestUnifiedHistoryPortableViewKeepsCallsWithoutHydratingOldIDs(t *testing.T) {
	input := []json.RawMessage{
		json.RawMessage(`{"type":"message","id":"msg_foreign","status":"completed","role":"user","content":"preserve original 17.125"}`),
		json.RawMessage(`{"type":"function_call","id":"fc_foreign","status":"completed","call_id":"call_original","name":"inspect","arguments":"{\"value\":17.125}"}`),
		json.RawMessage(`{"type":"function_call_output","id":"result_foreign","call_id":"call_original","output":"exact result"}`),
	}
	got, _, ok := unifiedPortableItems(input)
	require.True(t, ok)
	for _, item := range got {
		require.False(t, gjson.GetBytes(item, "id").Exists())
		require.False(t, gjson.GetBytes(item, "status").Exists())
	}
	require.Equal(t, "call_original", gjson.GetBytes(got[1], "call_id").String())
	require.Equal(t, "call_original", gjson.GetBytes(got[2], "call_id").String())
	require.Equal(t, gjson.GetBytes(input[1], "arguments").String(), gjson.GetBytes(got[1], "arguments").String())
	require.True(t, gjson.GetBytes(input[0], "id").Exists(), "archive identity remains unchanged")
}

func TestUnifiedHistoryUpdatedToolsPreservesSourceAndCurrentContract(t *testing.T) {
	source := []byte(`{"model":"gpt-6-astra","input":[{"type":"additional_tools","id":"tools","role":"developer","tools":[{"type":"custom","name":"old_tool"}]},{"role":"user","id":"user","content":"original question"},{"role":"assistant","content":"original evidence"},{"type":"compaction_trigger"}]}`)
	body := []byte(`{"model":"gpt-6-astra","input":[{"type":"additional_tools","id":"tools","role":"developer","tools":[{"type":"custom","name":"new_tool"}]},{"role":"user","id":"user","content":"original question"},{"type":"compaction","id":"cmp_tools","encrypted_content":"cipher"},{"role":"user","content":"new turn"}]}`)
	ref, _, _, err := parseRemoteCompactionRequest(body)
	require.NoError(t, err)
	got, ok, err := spliceUnifiedCompactionSource(body, source, ref)
	require.NoError(t, err)
	require.True(t, ok)
	items := gjson.GetBytes(got, "input").Array()
	require.Equal(t, "old_tool", items[0].Get("tools.0.name").String())
	require.Equal(t, "new_tool", items[1].Get("tools.0.name").String())
	require.Contains(t, string(got), "original evidence")
}

func TestUnifiedHistoryRepeatedToolRefreshFlattensSources(t *testing.T) {
	ctx, _, state, adapter := unifiedHistoryFixture(t)
	body := []byte(`{"model":"gpt-6-astra","input":[{"type":"additional_tools","role":"developer","id":"same_tool_id","tools":[]},{"role":"user","id":"original_user","content":"original exact 17.125"},{"type":"compaction_trigger"}]}`)
	for i := range 20 {
		ref := &remoteCompactionReference{ID: fmt.Sprintf("cmp_tools_%d", i), EncryptedContent: "native-cipher"}
		require.NoError(t, adapter.retainNativeCompactionSource(ctx, ref, state, &remoteCompactionSource{body: body}))
		stored, _, found, err := adapter.loadHistorySnapshot(ctx, historyCheckpointAlias(ref))
		require.NoError(t, err)
		require.True(t, found)
		require.False(t, gjson.GetBytes(stored, `input.#(type=="compaction")`).Exists())
		require.Contains(t, string(stored), "original exact 17.125")
		body = []byte(fmt.Sprintf(`{"model":"gpt-6-astra","input":[{"type":"additional_tools","role":"developer","id":"same_tool_id","tools":[{"type":"custom","name":"tool_%d"}]},{"role":"user","id":"original_user","content":"original exact 17.125"},{"role":"developer","id":"current_%d","content":"current runtime %d"},{"type":"compaction","id":%q,"encrypted_content":%q},{"role":"user","content":"new turn %d"},{"type":"compaction_trigger"}]}`, i, i, i, ref.ID, ref.EncryptedContent, i))
	}
	compiled, ok, err := adapter.compileUnifiedContext(ctx, body, state, &compactionEvidenceExecutor{})
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 1, strings.Count(string(compiled), "original exact 17.125"))
	for i := range 20 {
		require.Contains(t, string(compiled), fmt.Sprintf("new turn %d", i))
	}
}

func unifiedLongToolHistory(t *testing.T, calls int) []byte {
	t.Helper()
	items := []json.RawMessage{json.RawMessage(`{"role":"user","content":"preserve calibration exactly 17.125"}`)}
	for i := range calls {
		items = append(items, json.RawMessage(fmt.Sprintf(`{"type":"function_call","name":"inspect","call_id":"call_%d","arguments":"{}"}`, i)))
		output, err := json.Marshal(map[string]any{"type": "function_call_output", "call_id": fmt.Sprintf("call_%d", i), "output": fmt.Sprintf("original tool evidence %d: ", i) + strings.Repeat("abcdef ", 9000)})
		require.NoError(t, err)
		items = append(items, output)
	}
	items = append(items, json.RawMessage(`{"role":"user","content":"latest correction: use 17.250; continue"}`))
	body, err := json.Marshal(map[string]any{"model": "gpt-6-astra", "reasoning": map[string]string{"effort": "xhigh"}, "input": items})
	require.NoError(t, err)
	return body
}

func TestUnifiedHistoryLongSingleTurnKeepsPairsAndReusesSegments(t *testing.T) {
	ctx, _, state, adapter := unifiedHistoryFixture(t)
	state.CurrentCandidate.ContextTokens = 0
	executor := &unifiedConcurrentExecutor{}
	body := unifiedLongToolHistory(t, 24)
	first, ok, err := adapter.compileUnifiedContext(ctx, body, state, executor)
	require.NoError(t, err)
	require.True(t, ok)
	require.Contains(t, string(first), "17.125")
	require.Contains(t, string(first), "17.250")
	count := len(executor.summaries)
	require.Greater(t, count, 4, "long source needs multiple independent immutable segments")
	for _, summary := range executor.summaries {
		_, complete := responsesResourceHistorySupportsRecovery(summary)
		require.True(t, complete, "a source segment cannot split a tool call from its result")
		require.Contains(t, string(summary), unifiedSegmentPrompt)
		require.NotContains(t, string(summary), "verified retained source summary")
		require.Equal(t, "xhigh", gjson.GetBytes(summary, "reasoning.effort").String())
	}
	adapter = newRemoteCompactionAdapter(adapter.requestService, nil, adapter.systemService)
	second, ok, err := adapter.compileUnifiedContext(ctx, body, state, executor)
	require.NoError(t, err)
	require.True(t, ok)
	require.Len(t, executor.summaries, count)
	require.JSONEq(t, string(first), string(second))
	require.LessOrEqual(t, executor.max.Load(), int32(4))
}

type unifiedConcurrentExecutor struct {
	mu        sync.Mutex
	summaries [][]byte
	active    atomic.Int32
	max       atomic.Int32
	cancel    context.CancelFunc
	started   chan<- struct{}
	release   <-chan struct{}
}

func (e *unifiedConcurrentExecutor) Do(context.Context, *httpclient.Request) (*httpclient.Response, error) {
	return nil, fmt.Errorf("unexpected nonstream request")
}

func (e *unifiedConcurrentExecutor) DoStream(ctx context.Context, req *httpclient.Request) (streams.Stream[*httpclient.StreamEvent], error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	active := e.active.Add(1)
	defer e.active.Add(-1)
	for old := e.max.Load(); active > old; old = e.max.Load() {
		if e.max.CompareAndSwap(old, active) {
			break
		}
	}
	e.mu.Lock()
	e.summaries = append(e.summaries, append([]byte(nil), req.Body...))
	if e.cancel != nil {
		e.cancel()
		e.mu.Unlock()
		return nil, context.Canceled
	}
	e.mu.Unlock()
	if e.started != nil {
		e.started <- struct{}{}
		select {
		case <-e.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	contexts.WithChannelAPIKey(ctx, "segment-local-credential")
	return streams.SliceStream([]*httpclient.StreamEvent{{Type: "response.completed", Data: []byte(`{"type":"response.completed","response":{"id":"resp_segment","status":"completed","model":"gpt-6-astra","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"verified retained source summary"}]}]}}`)}}), nil
}

func TestUnifiedHistoryCanceledColdPlanStopsLaunchingSegments(t *testing.T) {
	ctx, _, state, adapter := unifiedHistoryFixture(t)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	executor := &unifiedConcurrentExecutor{cancel: cancel}
	_, _, err := adapter.compileUnifiedContext(ctx, unifiedLongToolHistory(t, 24), state, executor)
	require.ErrorIs(t, err, context.Canceled)
	require.LessOrEqual(t, len(executor.summaries), 4)
}
