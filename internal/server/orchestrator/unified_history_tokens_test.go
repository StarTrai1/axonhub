package orchestrator

import (
	"encoding/json"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestUnifiedHistoryTokenAdmissionKeepsLargeVerbatimInstructions(t *testing.T) {
	ctx, _, state, _ := unifiedHistoryFixture(t)
	state.CurrentCandidate.ContextTokens = 0
	text := strings.Repeat("Preserve original instructions, exact paths and completed findings. 保留原始指令与事实。\n", 12000)
	body, err := json.Marshal(map[string]any{"model": "gpt-6-astra", "input": []map[string]string{{"role": "developer", "content": text}, {"role": "user", "content": "Continue."}}})
	require.NoError(t, err)
	require.Greater(t, len(body), unifiedContextMaxTokens)
	source, err := prepareCheckpointSummarySource(ctx, &remoteCompactionSource{body: body}, state)
	require.NoError(t, err)
	require.Equal(t, text, gjson.GetBytes(source.body, "input.0.content").String())
	require.Less(t, unifiedContextSize(source.body), len(body)/2)
}

func TestUnifiedHistoryTokenAdmissionStillReservesOutput(t *testing.T) {
	ctx, _, state, _ := unifiedHistoryFixture(t)
	body := []byte(`{"model":"gpt-6-astra","max_output_tokens":2000000,"input":[{"role":"user","content":"hello"}]}`)
	source, err := prepareCheckpointSummarySource(ctx, &remoteCompactionSource{body: body}, state)
	require.NoError(t, err)
	require.ErrorContains(t, validateCheckpointSummaryRequest(source.body, state), "input budget")
}

func TestUnifiedHistoryLargeInteractionUsesModelBudget(t *testing.T) {
	ctx, _, state, _ := unifiedHistoryFixture(t)
	output, err := json.Marshal(map[string]string{"type": "function_call_output", "call_id": "large-call", "output": historyTestEntropy(238000, 64029)})
	require.NoError(t, err)
	items := []json.RawMessage{
		json.RawMessage(`{"role":"user","content":"retain the complete interaction"}`),
		json.RawMessage(`{"type":"function_call","call_id":"large-call","name":"read","arguments":"{}"}`),
		output,
		json.RawMessage(`{"role":"user","content":"continue"}`),
	}
	body, err := json.Marshal(map[string]any{"model": "gpt-6-astra", "input": items})
	require.NoError(t, err)
	source, err := prepareCheckpointSummarySource(ctx, &remoteCompactionSource{body: body}, state)
	require.NoError(t, err)
	require.Equal(t, "large-call", gjson.GetBytes(source.body, "input.1.call_id").String())
	require.JSONEq(t, string(output), gjson.GetBytes(source.body, "input.2").Raw)
}

func TestUnifiedHistoryTokenMeasureImagesAndConcurrency(t *testing.T) {
	t.Parallel()
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

// High-entropy fixture records exercise actual context pressure. Repeated
// letters compress into very few BPE tokens despite large serialized payloads.
func historyTestEntropy(size int, seed uint64) string {
	random := rand.New(rand.NewPCG(seed, seed+1))
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789+/"
	text := make([]byte, size)
	for i := range text {
		text[i] = alphabet[random.IntN(len(alphabet))]
	}
	return string(text)
}
