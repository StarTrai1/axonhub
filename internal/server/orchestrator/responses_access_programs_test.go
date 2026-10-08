package orchestrator

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

func TestResponsesSessionAccessProgramsUseCurrentTurn(t *testing.T) {
	ctx := shared.WithSessionScope(t.Context(), "api-key:program")
	store := newResponsesSessionStore()
	store.record(ctx,
		[]byte(`{"model":"gpt-6-sol","access_programs":{"cyber":"daybreak_blue"},"input":[{"role":"user","content":"first turn"}]}`),
		[]byte(`{"id":"resp_program","status":"completed","output":[{"type":"message","role":"assistant","content":"answer"}]}`))
	for _, program := range []string{"", `{}`, `{"cyber":"standard"}`, `{"cyber":"daybreak_blue"}`} {
		t.Run(program, func(t *testing.T) {
			payload := map[string]any{"model": "gpt-6.1-sol", "previous_response_id": "resp_program", "input": "continue"}
			if program != "" {
				payload["access_programs"] = json.RawMessage(program)
			}
			body, err := json.Marshal(payload)
			require.NoError(t, err)
			prepared, _ := store.prepare(ctx, body)
			require.Equal(t, "gpt-6.1-sol", gjson.GetBytes(prepared, "model").String())
			require.Equal(t, program, gjson.GetBytes(prepared, "access_programs").Raw)
			require.False(t, gjson.GetBytes(prepared, "previous_response_id").Exists())
			require.Len(t, gjson.GetBytes(prepared, "input").Array(), 3)
		})
	}
}

func TestResponsesCompactionAccessProgramsPreserved(t *testing.T) {
	for _, requestType := range []llm.RequestType{llm.RequestTypeChat, llm.RequestTypeCompact} {
		body := []byte(`{"model":"gpt-6.1-sol","access_programs":{"cyber":"standard"},"input":[{"role":"user","content":"original task"},{"type":"compaction_trigger"}]}`)
		prepared, err := buildLocalCompactionGenerationRequest(body, requestType)
		require.NoError(t, err)
		require.Equal(t, "standard", gjson.GetBytes(prepared, "access_programs.cyber").String())
		require.Equal(t, "gpt-6.1-sol", gjson.GetBytes(prepared, "model").String())
	}
	continuation := []byte(`{"model":"gpt-6.1-sol","access_programs":{"cyber":"standard"},"input":[{"type":"compaction","id":"cmp_program","encrypted_content":"synthetic-ciphertext"},{"role":"user","content":"continue"}]}`)
	restored, err := replaceRemoteCompactionWithLocalSummary(continuation, "verified source summary")
	require.NoError(t, err)
	require.Equal(t, "standard", gjson.GetBytes(restored, "access_programs.cyber").String())
	require.Equal(t, "gpt-6.1-sol", gjson.GetBytes(restored, "model").String())
	require.Contains(t, string(restored), "verified source summary")
}
