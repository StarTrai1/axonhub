package orchestrator

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func summaryCacheSegment(t *testing.T, input string) unifiedHistorySegment {
	t.Helper()
	contract := map[string]json.RawMessage{
		"model": json.RawMessage(`"gpt-6-astra"`), "destination_model": json.RawMessage(`"gpt-6-astra"`),
		"reasoning": json.RawMessage(`{"effort":"xhigh"}`), "input": json.RawMessage(input),
	}
	segment := unifiedHistorySegment{}
	require.NoError(t, setUnifiedSummaryKeys(&segment, contract))
	return segment
}

func TestUnifiedHistorySummaryCacheImportsOldItemOrderWithoutGeneration(t *testing.T) {
	ctx, _, state, adapter := unifiedHistoryFixture(t)
	old := summaryCacheSegment(t, `[{"call_id":"call_exact","input":"literal {\"b\":2,\"a\":1}","name":"inspect","type":"custom_tool_call"},{"type":"custom_tool_call_output","call_id":"call_exact","output":[{"type":"input_text","text":"retain 9007199254740993"}]},{"type":"function_call","name":"stats","call_id":"function_exact","arguments":"{}"},{"type":"function_call_output","call_id":"function_exact","output":"count 7"},{"type":"compaction_trigger"}]`)
	current := summaryCacheSegment(t, `[{"type":"custom_tool_call","call_id":"call_exact","name":"inspect","input":"literal {\"b\":2,\"a\":1}"},{"type":"custom_tool_call_output","call_id":"call_exact","output":[{"type":"input_text","text":"retain 9007199254740993"}]},{"type":"function_call","name":"stats","call_id":"function_exact","arguments":"{}"},{"type":"function_call_output","call_id":"function_exact","output":"count 7"},{"type":"compaction_trigger"}]`)
	require.Equal(t, old.cacheAlias, current.cacheAlias)
	require.NotEqual(t, old.legacyKeys[0], current.legacyKeys[0], "reproduces the legacy false cache miss")
	oldKey := old.legacyKeys[0]
	sealed, err := adapter.sealHistoryRecord(ctx, oldKey, []byte("old completed factual notes; exact value 9007199254740993"))
	require.NoError(t, err)
	require.NoError(t, adapter.systemService.SaveUnifiedHistory(ctx, map[string]string{oldKey: sealed}))
	executor := &unifiedConcurrentExecutor{}
	segments := []unifiedHistorySegment{current}
	missing, supported, err := adapter.hydrateUnifiedSegments(ctx, segments, state, executor, nil)
	require.NoError(t, err)
	require.True(t, supported)
	require.Zero(t, missing)
	require.Empty(t, executor.summaries)
	require.Equal(t, oldKey, segments[0].key, "keep the old rendered source label")
	require.Contains(t, segments[0].summary, "9007199254740993")
	adapter = newRemoteCompactionAdapter(nil, nil, adapter.systemService)
	thirdOrder := summaryCacheSegment(t, `[{"name":"inspect","type":"custom_tool_call","input":"literal {\"b\":2,\"a\":1}","call_id":"call_exact"},{"output":[{"type":"input_text","text":"retain 9007199254740993"}],"call_id":"call_exact","type":"custom_tool_call_output"},{"type":"function_call","name":"stats","call_id":"function_exact","arguments":"{}"},{"type":"function_call_output","call_id":"function_exact","output":"count 7"},{"type":"compaction_trigger"}]`)
	segments = []unifiedHistorySegment{thirdOrder}
	missing, supported, err = adapter.hydrateUnifiedSegments(ctx, segments, state, executor, nil)
	require.NoError(t, err)
	require.True(t, supported)
	require.Zero(t, missing)
	require.Empty(t, executor.summaries)
	require.Equal(t, oldKey, segments[0].key, "canonical alias survives process restart and a third serialization")
}

func TestUnifiedHistoryCanonicalIdentityPreservesContent(t *testing.T) {
	first, err := canonicalHistoryJSON([]byte(`{"n":9007199254740993,"array":[2,1],"arguments":"{\"b\":2,\"a\":1}","nested":{"b":2,"a":1}}`))
	require.NoError(t, err)
	second, err := canonicalHistoryJSON([]byte(`{"nested":{"a":1,"b":2},"arguments":"{\"b\":2,\"a\":1}","array":[2,1],"n":9007199254740993}`))
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.Contains(t, string(first), "9007199254740993")
	for _, changed := range []string{
		`{"n":9007199254740992,"array":[2,1],"arguments":"{\"b\":2,\"a\":1}","nested":{"b":2,"a":1}}`,
		`{"n":9007199254740993,"array":[1,2],"arguments":"{\"b\":2,\"a\":1}","nested":{"b":2,"a":1}}`,
		`{"n":9007199254740993,"array":[2,1],"arguments":"{\"a\":1,\"b\":2}","nested":{"b":2,"a":1}}`,
	} {
		other, err := canonicalHistoryJSON([]byte(changed))
		require.NoError(t, err)
		require.NotEqual(t, first, other)
	}
}
