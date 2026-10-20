package orchestrator

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
"github.com/looplj/axonhub/internal/ent/system"
)

func TestUnifiedHistoryCoverageSurvivesToolsAndReplyOptions(t *testing.T) {
	ctx, _, state, adapter := unifiedHistoryFixture(t)
	body := unifiedLongToolHistory(t, 24)
	body, err := sjson.SetBytes(body, "reasoning.effort", "xhigh")
	require.NoError(t, err)
	executor := &unifiedConcurrentExecutor{}
	_, ok, err := adapter.compileUnifiedContext(ctx, body, state, executor)
	require.NoError(t, err)
	require.True(t, ok)
	generated := len(executor.summaries)
	require.Greater(t, generated, 1)
	envelope, items, err := decodeResponsesInput(body)
	require.NoError(t, err)
	tool, err := json.Marshal(map[string]any{"type":"additional_tools", "role":"developer", "tools": []map[string]string{{"type":"custom", "name":"new_tool", "description":strings.Repeat("new tool contract ", 2500)}}})
	require.NoError(t, err)
	items = append([]json.RawMessage{tool}, items...)
	require.NoError(t, setResponseEnvelopeInput(envelope, items))
	envelope["reasoning"] = json.RawMessage(`{"effort":"high"}`)
	envelope["text"] = json.RawMessage(`{"verbosity":"low"}`)
	changed, err := json.Marshal(envelope)
	require.NoError(t, err)
	first, ok, err := adapter.compileUnifiedContext(ctx, changed, state, executor)
	require.NoError(t, err)
	require.True(t, ok)
	require.Len(t, executor.summaries, generated, "all old ranges should survive an inserted tool definition and reply settings")
	require.Contains(t, string(first), "new_tool")
	require.Equal(t, "high", gjson.GetBytes(first, "reasoning.effort").String())
	restarted := newRemoteCompactionAdapter(nil, nil, adapter.systemService)
	second, ok, err := restarted.compileUnifiedContext(ctx, changed, state, executor)
	require.NoError(t, err)
	require.True(t, ok)
	require.JSONEq(t, string(first), string(second))
	require.Len(t, executor.summaries, generated)
}

func TestUnifiedHistorySummaryAliasIgnoresReplyEffortNotFacts(t *testing.T) {
	ctx, _, state, adapter := unifiedHistoryFixture(t)
	contract := map[string]json.RawMessage{"model":json.RawMessage(`"gpt-6-astra"`), "reasoning":json.RawMessage(`{"effort":"xhigh"}`), "input":json.RawMessage(`[{"role":"assistant","content":"exact calibration 17.125"},{"type":"compaction_trigger"}]`)}
	old := unifiedHistorySegment{}
	require.NoError(t, setUnifiedSummaryKeys(&old, contract))
	sealed, err := adapter.sealHistoryRecord(ctx, old.key, []byte("Original calibration 17.125"))
	require.NoError(t, err)
	require.NoError(t, adapter.systemService.SaveUnifiedHistory(ctx, map[string]string{old.key:sealed}))
	contract["reasoning"] = json.RawMessage(`{"effort":"high"}`)
	current := unifiedHistorySegment{}
	require.NoError(t, setUnifiedSummaryKeys(&current, contract))
	require.Equal(t, old.cacheAlias, current.cacheAlias)
	segments := []unifiedHistorySegment{current}
	missing, ok, err := adapter.hydrateUnifiedSegments(ctx, segments, state, &unifiedConcurrentExecutor{}, nil)
	require.NoError(t, err)
	require.True(t, ok)
	require.Zero(t, missing)
	require.Equal(t, old.key, segments[0].key)
	contract["input"] = json.RawMessage(`[{"role":"assistant","content":"different branch calibration 17.250"},{"type":"compaction_trigger"}]`)
	other := unifiedHistorySegment{}
	require.NoError(t, setUnifiedSummaryKeys(&other, contract))
	require.NotEqual(t, current.cacheAlias, other.cacheAlias)
}

func TestUnifiedHistoryCoverageImportsLegacyCheckpointSummaries(t *testing.T) {
 ctx, client, state, adapter := unifiedHistoryFixture(t)
 original := unifiedLongToolHistory(t, 24)
 executor := &unifiedConcurrentExecutor{}
 _, ready, err := adapter.compileUnifiedContext(ctx, original, state, executor)
 require.NoError(t, err)
 require.True(t, ready)
 prior := len(executor.summaries)
 // Simulate a pre-coverage installation: retain only the old paid notes.
 _, err = client.System.Delete().Where(system.Or(system.KeyContains(":coverage:"), system.KeyContains(":summaryref:"))).Exec(ctx)
 require.NoError(t, err)
 envelope, input, err := decodeResponsesInput(original)
 require.NoError(t, err)
 require.NoError(t, setResponseEnvelopeInput(envelope, append(input, json.RawMessage(`{"type":"compaction_trigger"}`))))
 source, err := json.Marshal(envelope)
 require.NoError(t, err)
 ref := &remoteCompactionReference{ID:"cmp_legacy_coverage", EncryptedContent:"old-native-state"}
 require.NoError(t, adapter.saveHistorySnapshot(ctx, historyCheckpointAlias(ref), source, nil))
 body := []byte(`{"model":"gpt-6-astra","reasoning":{"effort":"high"},"input":[{"type":"additional_tools","role":"developer","tools":[{"type":"custom","name":"new_tool"}]},{"role":"user","content":"preserve calibration exactly 17.125"},{"type":"compaction","id":"cmp_legacy_coverage","encrypted_content":"old-native-state"},{"role":"user","content":"next correction 18.125"}]}`)
 restarted := newRemoteCompactionAdapter(nil, nil, adapter.systemService)
 compiled, ready, err := restarted.compileUnifiedContext(ctx, body, state, executor)
 require.NoError(t, err)
 require.True(t, ready)
 require.LessOrEqual(t, len(executor.summaries)-prior, 1, "newly closed tail may need a summary; old completed ranges do not")
 require.Contains(t, string(compiled), "next correction 18.125")
 require.Contains(t, string(compiled), "new_tool")
}

func TestUnifiedHistoryCoverageRejectsEditedEvidence(t *testing.T) {
 ctx, _, state, adapter := unifiedHistoryFixture(t)
 body := unifiedLongToolHistory(t, 24)
 _, ready, err := adapter.compileUnifiedContext(ctx, body, state, &unifiedConcurrentExecutor{})
 require.NoError(t, err)
 require.True(t, ready)
 body, err = sjson.SetBytes(body, "input.2.output", "different branch exact value 99.875")
 require.NoError(t, err)
 envelope, items, err := decodeResponsesInput(body)
 require.NoError(t, err)
 items, _, portable := unifiedPortableItems(items)
 require.True(t, portable)
 matches, err := adapter.loadUnifiedCoverage(ctx, envelope, items, state, state.CurrentCandidate)
 require.NoError(t, err)
 require.NotEmpty(t, matches, "unmodified later ranges remain reusable")
 for start, segment := range matches {
 require.False(t, start <= 2 && start + len(segment.items) > 2, "an edited original must invalidate every covering summary")
 }
}
