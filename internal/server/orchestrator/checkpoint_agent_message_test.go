package orchestrator

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

// Requests 70000/70002 had a native checkpoint followed by a locally sealed
// agents.spawn_agent message. The source prefix matched, but the completeness
// guard ran before the outbound recovery middleware could open the message.
func TestCheckpointGenerationRecoversAgentMessageBeforeSourceValidation(t *testing.T) {
	for _, placement := range []string{"current window", "retained source", "first compaction"} {
		t.Run(placement, func(t *testing.T) {
			ctx, _, state, adapter := unifiedHistoryFixture(t)
			require.NoError(t, adapter.systemService.SetSecretKey(ctx, "checkpoint-agent-test-secret"))
			aead, err := newResponsesAgentMessageCipher("checkpoint-agent-test-secret")
			require.NoError(t, err)
			aad, err := responsesAgentMessageAAD(state, "")
			require.NoError(t, err)
			const plaintext = "Keep every instruction.\n保留完整消息与 17.125。"
			sealed, err := sealResponsesAgentMessage(aead, aad, plaintext)
			require.NoError(t, err)
			args, err := sjson.Set(`{"task_name":"inspect","fork_turns":"none","message":"","seed":9007199254740993}`, "message", sealed)
			require.NoError(t, err)
			call, err := json.Marshal(map[string]any{"type": "function_call", "id": "fc_agent", "call_id": "call_agent", "namespace": "agents", "name": "spawn_agent", "arguments": args, "encrypted_function_args": []string{"message"}})
			require.NoError(t, err)
			user := json.RawMessage(`{"type":"message","id":"msg_user","role":"user","content":"original question"}`)
			result := json.RawMessage(`{"type":"function_call_output","call_id":"call_agent","output":"original tool result"}`)
			trigger := json.RawMessage(`{"type":"compaction_trigger"}`)
			ref := &remoteCompactionReference{ID: "cmp_agent_source", EncryptedContent: "native-checkpoint"}
			checkpoint := json.RawMessage(`{"type":"compaction","id":"cmp_agent_source","encrypted_content":"native-checkpoint"}`)
			prior := []json.RawMessage{user, json.RawMessage(`{"type":"message","role":"assistant","content":"retained exact evidence"}`), trigger}
			current := []json.RawMessage{user, checkpoint, call, result, trigger}
			if placement == "retained source" {
				prior = []json.RawMessage{user, call, result, trigger}
				current = []json.RawMessage{user, checkpoint, json.RawMessage(`{"role":"user","content":"latest correction"}`), trigger}
			}
			if placement == "first compaction" {
				current = []json.RawMessage{user, call, result, trigger}
			}
			source, err := json.Marshal(map[string]any{"model": "gpt-6.1-sol", "input": prior})
			require.NoError(t, err)
			require.NoError(t, adapter.saveHistorySnapshot(ctx, historyCheckpointWindowAlias(ref), source, nil))
			body, err := json.Marshal(map[string]any{"model": "gpt-6.1-sol", "stream": true, "input": current, "prompt_cache_key": "caller-key", "reasoning": map[string]string{"effort": "max"}})
			require.NoError(t, err)
			request := &httpclient.Request{APIFormat: string(llm.APIFormatOpenAIResponse), Body: body, Headers: make(http.Header)}
			parsed, err := responses.NewInboundTransformer().TransformRequest(ctx, request)
			require.NoError(t, err)
			parsed.RawRequest = request
			state.CurrentCandidate = nil // This happens before selecting an outbound.
			middleware := &remoteCompactionMiddleware{adapter: adapter, inbound: &PersistentInboundTransformer{state: state}}
			adapted, err := middleware.adaptLocalCompactionGeneration(ctx, parsed)
			require.NoError(t, err)
			require.NotNil(t, middleware.generation)
			wire := adapted.RawRequest.Body
			agent := gjson.GetBytes(wire, `input.#(name=="spawn_agent")`)
			require.Equal(t, plaintext, gjson.Get(agent.Get("arguments").String(), "message").String())
			require.Equal(t, "9007199254740993", gjson.Get(agent.Get("arguments").String(), "seed").Raw)
			require.False(t, agent.Get("encrypted_function_args").Exists())
			require.Equal(t, "call_agent", agent.Get("call_id").String())
			require.Contains(t, string(wire), "original tool result")
			require.NotContains(t, string(wire), "native-checkpoint")
			require.Equal(t, "caller-key", gjson.GetBytes(wire, "prompt_cache_key").String())
			require.Equal(t, "max", gjson.GetBytes(wire, "reasoning.effort").String())
			require.Equal(t, "none", gjson.GetBytes(wire, "tool_choice").String())
			require.Equal(t, body, request.Body, "original request remains sealed")
			stored, _, found, err := adapter.loadHistorySnapshot(ctx, historyCheckpointWindowAlias(ref))
			require.NoError(t, err)
			require.True(t, found)
			require.JSONEq(t, string(source), string(stored), "immutable source remains unchanged")
		})
	}
}

func TestCheckpointAgentRecoveryRejectsUnprovenPlaintext(t *testing.T) {
	for _, scenario := range []string{"tampered", "other project", "other key", "unknown seal", "unsupported marker", "orphan result"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, _, state, adapter := unifiedHistoryFixture(t)
			require.NoError(t, adapter.systemService.SetSecretKey(ctx, "checkpoint-agent-test-secret"))
			aead, err := newResponsesAgentMessageCipher("checkpoint-agent-test-secret")
			require.NoError(t, err)
			sealState := *state
			owner := *state.APIKey
			sealState.APIKey = &owner
			if scenario == "other project" { owner.ProjectID++ }
			if scenario == "other key" { owner.ID++ }
			aad, err := responsesAgentMessageAAD(&sealState, "")
			require.NoError(t, err)
			sealed, err := sealResponsesAgentMessage(aead, aad, "only authenticated original content")
			require.NoError(t, err)
			if scenario == "tampered" { sealed = responsesAgentMessagePrefix + "invalid" }
			if scenario == "unknown seal" { sealed = "opaque-provider-ciphertext" }
			args, err := json.Marshal(map[string]string{"message": sealed})
			require.NoError(t, err)
			marker := []string{"message"}
			if scenario == "unsupported marker" { marker = append(marker, "recipient") }
			callID := "call_agent"
			if scenario == "orphan result" { callID = "unknown_call" }
			body, err := json.Marshal(map[string]any{"model": "gpt-6.1-sol", "input": []any{
				map[string]string{"role":"user", "content":"retained question"},
				map[string]any{"type":"function_call", "call_id":"call_agent", "namespace":"agents", "name":"spawn_agent", "arguments":string(args), "encrypted_function_args":marker},
				map[string]string{"type":"function_call_output", "call_id":callID, "output":"result"},
				map[string]string{"type":"compaction_trigger"},
			}})
			require.NoError(t, err)
			// Use the same checkpoint completeness boundary as the failing requests.
			ref := &remoteCompactionReference{ID:"cmp_guard", EncryptedContent:"native"}
			source := []byte(`{"model":"gpt-6.1-sol","input":[{"role":"user","content":"retained question"},{"type":"compaction_trigger"}]}`)
			require.NoError(t, adapter.saveHistorySnapshot(ctx, historyCheckpointWindowAlias(ref), source, nil))
			var root map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(body, &root))
			var input []json.RawMessage
			require.NoError(t, json.Unmarshal(root["input"], &input))
			input = append(input[:1:1], append([]json.RawMessage{json.RawMessage(`{"type":"compaction","id":"cmp_guard","encrypted_content":"native"}`)}, input[1:]...)...)
			root["input"], err = json.Marshal(input)
			require.NoError(t, err)
			body, err = json.Marshal(root)
			require.NoError(t, err)
			restored, err := adapter.restoreCheckpointWindow(ctx, &remoteCompactionSource{body:body}, &PersistenceState{APIKey:&ent.APIKey{ID:state.APIKey.ID, ProjectID:state.APIKey.ProjectID}})
			require.Error(t, err)
			require.Nil(t, restored)
		})
	}
}
