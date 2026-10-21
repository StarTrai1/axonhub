package orchestrator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
)

func TestCheckpointBudgetIgnoresTransportEscaping(t *testing.T) {
	ctx, _, state, _ := unifiedHistoryFixture(t)
	state.CurrentCandidate.ContextTokens = 32000
	state.checkpointSummaryGeneration = true
	value := map[string]any{
		"model": "gpt-6-astra",
		"input": []any{
			map[string]string{"role": "user", "content": strings.Repeat("<tag>&value</tag>\n", 1000)},
			map[string]any{"type": "function_call", "call_id": "retain-call", "name": "read", "arguments": `{"offset":9007199254740993}`},
			map[string]string{"type": "function_call_output", "call_id": "retain-call", "output": "exact result"},
		},
		"tools": []any{map[string]any{"type": "function", "name": "read", "parameters": map[string]any{"maximum": json.Number("9007199254740993")}}},
	}
	escaped, err := json.Marshal(value)
	require.NoError(t, err)
	var unescaped bytes.Buffer
	encoder := json.NewEncoder(&unescaped)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	require.NoError(t, encoder.Encode(value))
	_, budget := unifiedHistoryInputBudgets(escaped, state.CurrentCandidate, 0, 32000)
	require.Greater(t, unifiedContextSize(escaped), budget, "reproduce the former false rejection caused by JSON HTML escaping")
	count, err := checkpointContextSize(escaped)
	require.NoError(t, err)
	require.Less(t, count, budget)
	other, err := checkpointContextSize(unescaped.Bytes())
	require.NoError(t, err)
	require.Equal(t, count, other, "equivalent JSON must have the same admission result")
	for _, body := range [][]byte{escaped, unescaped.Bytes()} {
		original := append([]byte(nil), body...)
		request := &httpclient.Request{Body: body, JSONBody: body}
		got, err := admitCheckpointSummary(&PersistentOutboundTransformer{state: state}).OnOutboundRawRequest(ctx, request)
		require.NoError(t, err)
		require.Same(t, request, got)
		require.Equal(t, original, got.Body, "measurement must not rewrite the provider/cache prefix")
		require.Equal(t, original, got.JSONBody)
		require.Equal(t, "9007199254740993", gjson.GetBytes(got.Body, "tools.0.parameters.maximum").Raw)
	}
}

func TestCheckpointBudgetUsesFinalModelToolsAndOutput(t *testing.T) {
	ctx, _, state, _ := unifiedHistoryFixture(t)
	state.CurrentCandidate.ContextTokens = 16000
	state.checkpointSummaryGeneration = true
	admission := admitCheckpointSummary(&PersistentOutboundTransformer{state: state})
	body := []byte(`{"model":"gpt-6-astra","input":[{"role":"user","content":"small source"}]}`)
	_, err := admission.OnOutboundRawRequest(ctx, &httpclient.Request{Body: body})
	require.NoError(t, err)
	for _, field := range []string{"instructions", "tools.0.description"} {
		large, err := sjson.SetBytes(body, field, historyTestEntropy(64000, 64680))
		require.NoError(t, err)
		_, err = admission.OnOutboundRawRequest(ctx, &httpclient.Request{Body: large})
		require.ErrorContains(t, err, "input budget", "late additions must be included: %s", field)
	}
	largeOutput, err := sjson.SetBytes(body, "max_output_tokens", 2000000)
	require.NoError(t, err)
	_, err = admission.OnOutboundRawRequest(ctx, &httpclient.Request{Body: largeOutput})
	require.ErrorContains(t, err, "input budget")
	changed, err := sjson.SetBytes(body, "model", "gpt-6.1-sol")
	require.NoError(t, err)
	changed, err = sjson.SetBytes(changed, "input.0.content", strings.Repeat("complete exact observation ", 5000))
	require.NoError(t, err)
	_, err = admission.OnOutboundRawRequest(ctx, &httpclient.Request{Body: changed})
	require.NoError(t, err, "use the final model's catalog instead of the previous model's 16k window")
	state.checkpointSummaryGeneration = false
	_, err = admission.OnOutboundRawRequest(ctx, &httpclient.Request{Body: largeOutput})
	require.NoError(t, err, "ordinary requests and client memory work do not acquire a gateway summary budget")
}

func TestCheckpointLegacyEscapedSourceMigratesOnce(t *testing.T) {
	for _, raw := range []bool{false, true} {
		t.Run(fmt.Sprint(raw), func(t *testing.T) {
			ctx, _, owner, adapter := rejectedCompactionStorage(t)
			req := rejectedCompactionPipelineRequest(t, llm.APIFormatOpenAIResponse)
			ref, _, _, err := parseRemoteCompactionRequest(req.Body)
			require.NoError(t, err)
			observation := strings.Repeat("<tag>&value</tag>\n", 1000)
			source, err := json.Marshal(map[string]any{"model": "legacy-source-model", "input": []any{
				map[string]string{"role": "user", "content": "retain the original task and exact tool result"},
				map[string]string{"type": "custom_tool_call", "call_id": "source-call", "name": "inspect", "input": "<source>&exact"},
				map[string]string{"type": "custom_tool_call_output", "call_id": "source-call", "output": observation},
				map[string]string{"type": "compaction_trigger"},
			}})
			require.NoError(t, err)
			// Reproduce an existing installation's immutable legacy archive.
			require.NoError(t, adapter.saveHistorySnapshot(ctx, historyCheckpointAlias(ref), source, nil))
			executor := &rejectedCompactionBridgeExecutor{}
			configure := func(state *PersistenceState, outbound *PersistentOutboundTransformer) pipeline.Middleware {
				state.APIKey = owner
				state.ChannelModelsCandidates[0].ContextTokens = 32000
				return recoverRejectedRemoteCompaction(outbound, adapter, executor)
			}
			for range 2 {
				_, result, err := runRejectedReasoningPipeline(t, ctx, req, executor, "semantic-budget-key", raw, 2, configure)
				require.NoError(t, err)
				drainRejectedReasoningPipeline(t, result)
				adapter = newRemoteCompactionAdapter(adapter.requestService, nil, adapter.systemService)
			}
			require.Len(t, executor.summaries, 1, "a cold restart must reuse the completed portable summary")
			require.Equal(t, "gpt-6-astra", gjson.GetBytes(executor.summaries[0], "model").String())
			require.Equal(t, observation, gjson.GetBytes(executor.summaries[0], `input.#(type=="custom_tool_call_output").output`).String())
			require.Equal(t, "<source>&exact", gjson.GetBytes(executor.summaries[0], `input.#(type=="custom_tool_call").input`).String())
			require.Len(t, executor.continuations, 3)
			assertRejectedCompactionWindowPreserved(t, executor.continuations[0], executor.continuations[1])
			assertRejectedCompactionWindowPreserved(t, executor.continuations[0], executor.continuations[2])
		})
	}
}
