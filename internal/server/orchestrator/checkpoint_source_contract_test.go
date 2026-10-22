package orchestrator

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/pipeline"
)

// Old installations kept additional_tools in the archive. New windows retain
// the provider envelope, where tools may be defaulted, lifted or refreshed.
func checkpointContractSources(t *testing.T, contract string) ([]byte, []byte) {
	t.Helper()
	parent := []byte(`{"model":"gpt-6-astra","input":[
		{"type":"additional_tools","id":"tools","role":"developer","tools":[{"type":"custom","name":"inspect","description":"original tool contract"}]},
		{"type":"message","id":"original-user","role":"user","content":"original task: preserve 17.125 exactly"},
		{"type":"custom_tool_call","call_id":"parent-call","name":"inspect","input":"original exact input"},
		{"type":"custom_tool_call_output","call_id":"parent-call","output":"original exact result: 17.125"},
		{"type":"compaction_trigger"}
	]}`)
	current := []byte(`{"model":"gpt-6-astra","instructions":"","tools":[],"input":[
		{"type":"additional_tools","id":"tools","role":"developer","tools":[{"type":"custom","name":"inspect","description":"refreshed tool contract"}]},
		{"type":"message","id":"original-user","role":"user","content":"original task: preserve 17.125 exactly"},
		{"type":"message","role":"developer","content":"current runtime constraint"},
		{"type":"compaction","id":"cmp_parent","encrypted_content":"parent-cipher"},
		{"type":"function_call","call_id":"recent-call","name":"check","arguments":"{\"value\":17.250}"},
		{"type":"function_call_output","call_id":"recent-call","output":"latest correction: 17.250"},
		{"type":"compaction_trigger"}
	]}`)
	var err error
	switch contract {
	case "lifted tools":
		current, err = sjson.DeleteBytes(current, "input.0")
		require.NoError(t, err)
		current, err = sjson.SetRawBytes(current, "tools", []byte(`[{"type":"custom","name":"inspect","description":"refreshed tool contract"}]`))
		require.NoError(t, err)
	case "model and schema change":
		parent, err = sjson.SetBytes(parent, "model", "previous-model")
		require.NoError(t, err)
		parent, err = sjson.SetRawBytes(parent, "tools", []byte(`[{"type":"function","name":"check","parameters":{"type":"object","properties":{"old":{"type":"string"}}}}]`))
		require.NoError(t, err)
		current, err = sjson.SetRawBytes(current, "tools", []byte(`[{"type":"function","name":"check","parameters":{"type":"object","properties":{"value":{"type":"number"}}}}]`))
		require.NoError(t, err)
	}
	return parent, current
}

func TestCheckpointSourceContractMigrationSurvivesRestart(t *testing.T) {
	for _, contract := range []string{"empty tools", "lifted tools", "model and schema change"} {
		for _, raw := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/raw=%t", contract, raw), func(t *testing.T) {
				ctx, _, owner, adapter := rejectedCompactionStorage(t)
				req := rejectedCompactionPipelineRequest(t, llm.APIFormatOpenAIResponse)
				ref, _, _, err := parseRemoteCompactionRequest(req.Body)
				require.NoError(t, err)
				parent, current := checkpointContractSources(t, contract)
				ancestor, _, _, err := parseRemoteCompactionRequest(current)
				require.NoError(t, err)
				require.NoError(t, adapter.saveHistorySnapshot(ctx, historyCheckpointAlias(ancestor), parent, nil))
				require.NoError(t, adapter.saveHistorySnapshot(ctx, historyCheckpointWindowAlias(ref), current, nil))

				restored, err := adapter.restoreCheckpointWindow(ctx, &remoteCompactionSource{body: current}, &PersistenceState{APIKey: owner})
				require.NoError(t, err)
				before, _, err := decodeResponsesInput(current)
				require.NoError(t, err)
				after, _, err := decodeResponsesInput(restored.body)
				require.NoError(t, err)
				delete(before, "input")
				delete(after, "input")
				require.Equal(t, before, after, "history restoration must retain every current request option")
				executor := &rejectedCompactionBridgeExecutor{}
				if contract == "empty tools" {
					executor.rejection = rejectedNativeCheckpointError()
				}
				configure := func(state *PersistenceState, outbound *PersistentOutboundTransformer) pipeline.Middleware {
					state.APIKey = owner
					return recoverRejectedRemoteCompaction(outbound, adapter, executor)
				}
				for range 2 {
					_, result, err := runRejectedReasoningPipeline(t, ctx, req, executor, "contract-migration-key", raw, 2, configure)
					require.NoError(t, err)
					drainRejectedReasoningPipeline(t, result)
					adapter = newRemoteCompactionAdapter(adapter.requestService, nil, adapter.systemService)
				}
				require.Len(t, executor.summaries, 1, "nested migration must generate once and reuse its persisted summary after restart")
				summary := executor.summaries[0]
				require.Equal(t, "gpt-6-astra", gjson.GetBytes(summary, "model").String())
				require.False(t, gjson.GetBytes(summary, `input.#(type=="compaction")`).Exists())
				require.Equal(t, 1, strings.Count(string(summary), "original task: preserve 17.125 exactly"))
				require.Equal(t, "original exact input", gjson.GetBytes(summary, `input.#(type=="custom_tool_call").input`).String())
				require.Equal(t, "original exact result: 17.125", gjson.GetBytes(summary, `input.#(type=="custom_tool_call_output").output`).String())
				require.Equal(t, `{"value":17.250}`, gjson.GetBytes(summary, `input.#(type=="function_call").arguments`).String())
				require.Equal(t, "latest correction: 17.250", gjson.GetBytes(summary, `input.#(type=="function_call_output").output`).String())
				require.Contains(t, string(summary), "current runtime constraint")
				require.Len(t, executor.continuations, 3)
				assertRejectedCompactionWindowPreserved(t, executor.continuations[0], executor.continuations[1])
				assertRejectedCompactionWindowPreserved(t, executor.continuations[0], executor.continuations[2])
				stored, _, found, err := adapter.loadHistorySnapshot(ctx, historyCheckpointAlias(ancestor))
				require.NoError(t, err)
				require.True(t, found)
				require.JSONEq(t, string(parent), string(stored), "the authenticated archive remains immutable")
			})
		}
	}
}

func TestCheckpointSourceContractKeepsHistoryGuards(t *testing.T) {
	for _, invalid := range []string{"checkpoint id", "checkpoint ciphertext", "changed retained message", "missing trigger", "orphan output", "changed instructions"} {
		t.Run(invalid, func(t *testing.T) {
			parent, current := checkpointContractSources(t, "model and schema change")
			ref, _, _, err := parseRemoteCompactionRequest(current)
			require.NoError(t, err)
			switch invalid {
			case "checkpoint id":
				ref.ID = "another-checkpoint"
			case "checkpoint ciphertext":
				ref.EncryptedContent = "another-ciphertext"
			case "changed retained message":
				current, err = sjson.SetBytes(current, "input.1.content", "different branch under the same message ID")
			case "missing trigger":
				parent, err = sjson.DeleteBytes(parent, "input.4")
			case "orphan output":
				parent, err = sjson.DeleteBytes(parent, "input.2")
			case "changed instructions":
				parent, err = sjson.SetBytes(parent, "instructions", "historical instructions must not disappear")
			}
			require.NoError(t, err)
			got, complete, err := spliceUnifiedCompactionSource(current, parent, ref)
			require.NoError(t, err)
			require.False(t, complete)
			require.Equal(t, current, got)
			require.True(t, json.Valid(got))
		})
	}
}
