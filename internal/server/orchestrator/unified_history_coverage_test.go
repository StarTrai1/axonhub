package orchestrator

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
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
