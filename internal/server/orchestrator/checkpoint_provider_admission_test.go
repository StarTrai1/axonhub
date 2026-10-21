package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/streams"
)

func TestCheckpointLegacyEscapedSourceMigratesOnce(t *testing.T) {
	for _, raw := range []bool{false, true} {
		t.Run(fmt.Sprint(raw), func(t *testing.T) {
			ctx, _, owner, adapter := rejectedCompactionStorage(t)
			req := rejectedCompactionPipelineRequest(t, llm.APIFormatOpenAIResponse)
			ref, _, _, err := parseRemoteCompactionRequest(req.Body)
			require.NoError(t, err)
			observation := strings.Repeat("<tag>&value</tag>\nquoted code: \"result\" C:\\workspace\\file\n", 24000)
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
				_, result, err := runRejectedReasoningPipeline(t, ctx, req, executor, "provider-admission-key", raw, 2, configure)
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

func TestCheckpointProviderContextRejectionDoesNotRetry(t *testing.T) {
	ctx, _, state, adapter := unifiedHistoryFixture(t)
	state.CurrentCandidate.ContextTokens = 16000
	observation := historyTestEntropy(238000, 64764)
	body, err := json.Marshal(map[string]any{"model": "gpt-6-astra", "input": []any{
		map[string]string{"role": "user", "content": "preserve the complete tool result"},
		map[string]string{"type": "custom_tool_call", "name": "read", "call_id": "limit-call", "input": "exact source"},
		map[string]string{"type": "custom_tool_call_output", "call_id": "limit-call", "output": observation},
		map[string]string{"type": "compaction_trigger"},
	}})
	require.NoError(t, err)
	executor := &checkpointProviderLimitExecutor{}
	_, err = adapter.generateLocalSummary(ctx, "provider-context-limit", &remoteCompactionSource{body: body, headers: make(http.Header)}, state, executor)
	var upstream *httpclient.Error
	require.ErrorAs(t, err, &upstream)
	require.Equal(t, http.StatusBadRequest, upstream.StatusCode)
	require.Equal(t, "context_length_exceeded", gjson.GetBytes(upstream.Body, "error.code").String())
	require.Len(t, executor.bodies, 1, "a genuine provider limit must not cause the same summary to be resent")
	require.Equal(t, observation, gjson.GetBytes(executor.bodies[0], `input.#(type=="custom_tool_call_output").output`).String())
	require.Equal(t, "limit-call", gjson.GetBytes(executor.bodies[0], `input.#(type=="custom_tool_call").call_id`).String())
}

type checkpointProviderLimitExecutor struct {
	rejectedCompactionBridgeExecutor

	bodies [][]byte
}

func (e *checkpointProviderLimitExecutor) DoStream(_ context.Context, req *httpclient.Request) (streams.Stream[*httpclient.StreamEvent], error) {
	e.bodies = append(e.bodies, append([]byte(nil), req.Body...))
	return nil, &httpclient.Error{StatusCode: http.StatusBadRequest, Body: []byte(`{"error":{"type":"invalid_request_error","code":"context_length_exceeded","param":"input","message":"The rendered input exceeds this resource's context window"}}`)}
}
