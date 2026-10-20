package orchestrator

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestUnifiedHistoryTokenAdmissionKeepsLargeVerbatimInstructions(t *testing.T) {
	ctx, _, state, adapter := unifiedHistoryFixture(t)
	state.CurrentCandidate.ContextTokens = 0
	text := strings.Repeat("Preserve original instructions, exact paths and completed findings. 保留原始指令与事实。\n", 12000)
	body, err := json.Marshal(map[string]any{"model": "gpt-6-astra", "input": []map[string]string{{"role": "developer", "content": text}, {"role": "user", "content": "Continue."}}})
	require.NoError(t, err)
	require.Greater(t, len(body), unifiedContextMaxBytes)
	executor := &unifiedConcurrentExecutor{}
	compiled, ok, err := adapter.compileUnifiedContext(ctx, body, state, executor)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, text, gjson.GetBytes(compiled, "input.0.content").String())
	require.Empty(t, executor.summaries)
	require.Less(t, unifiedContextSize(compiled), len(body)/2)
}

func TestUnifiedHistoryTokenAdmissionStillReservesOutput(t *testing.T) {
	ctx, _, state, adapter := unifiedHistoryFixture(t)
	body := []byte(`{"model":"gpt-6-astra","max_output_tokens":2000000,"input":[{"role":"user","content":"hello"}]}`)
	executor := &unifiedConcurrentExecutor{}
	_, ok, err := adapter.compileUnifiedContext(ctx, body, state, executor)
	require.Error(t, err)
	require.False(t, ok)
	require.Empty(t, executor.summaries)
}

func TestUnifiedHistoryTokenMeasureImagesAndConcurrency(t *testing.T) {
	measure := unifiedModelMeasure("gpt-6-astra")
	image, err := json.Marshal(map[string]any{"role": "user", "content": []map[string]string{{"type": "input_image", "image_url": "data:image/png;base64," + strings.Repeat("A", 200000)}}})
	require.NoError(t, err)
	require.GreaterOrEqual(t, measure.item(image), unifiedImageAllowance)
	require.Less(t, measure.item(image), unifiedImageAllowance+1000)
	for range 16 {
		t.Run("parallel", func(t *testing.T) {
			t.Parallel()
			raw := []byte(strings.Repeat("并发恢复 retains the same input.\n", 50))
			require.Equal(t, unifiedTextTokens(raw), unifiedTextTokens(raw))
		})
	}
}
