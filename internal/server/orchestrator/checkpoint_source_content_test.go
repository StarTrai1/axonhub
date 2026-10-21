package orchestrator

import (
	"encoding/json"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestCheckpointSourceKeepsLargeVerbatimInstructions(t *testing.T) {
	ctx, _, state, _ := unifiedHistoryFixture(t)
	state.CurrentCandidate.ContextTokens = 0
	text := strings.Repeat("Preserve original instructions, exact paths and completed findings. 保留原始指令与事实。\n", 12000)
	body, err := json.Marshal(map[string]any{"model": "gpt-6-astra", "input": []map[string]string{{"role": "developer", "content": text}, {"role": "user", "content": "Continue."}}})
	require.NoError(t, err)
	require.Greater(t, len(body), 1<<20)
	source, err := prepareCheckpointSummarySource(ctx, &remoteCompactionSource{body: body}, state)
	require.NoError(t, err)
	require.Equal(t, text, gjson.GetBytes(source.body, "input.0.content").String())
}

func TestCheckpointSourceKeepsLargeToolInteraction(t *testing.T) {
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
