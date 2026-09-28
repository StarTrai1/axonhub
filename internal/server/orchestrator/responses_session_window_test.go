package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm/transformer/shared"
)

func TestResponsesSessionContextWindowRollover(t *testing.T) {
	for _, persisted := range []bool{false, true} {
		for _, tc := range []struct {
			name     string
			metadata string
			wantRoot bool
		}{
			{"new direct window", `{"x-codex-window-id":"thread:2"}`, true},
			{"new turn window", `{"x-codex-turn-metadata":"{\"window_id\":\"thread:2\"}"}`, true},
			{"same window", `{"x-codex-window-id":"thread:1"}`, false},
			{"unknown window", `{}`, false},
		} {
			t.Run(fmt.Sprintf("%s/persisted=%t", tc.name, persisted), func(t *testing.T) {
				ctx := shared.WithSessionScope(t.Context(), "key:window-test")
				previous := []byte(`{"client_metadata":{"x-codex-turn-metadata":"{\"window_id\":\"thread:1\"}"},"input":[{"role":"user","content":"old history"}]}`)
				completed := []byte(`{"id":"resp_old_window","status":"completed","output":[{"type":"message","role":"assistant","content":"old answer"}]}`)
				store := newResponsesSessionStore()
				if persisted {
					store = newResponsesSessionStore(func(context.Context, string) ([]byte, []byte, bool, error) {
						return previous, completed, true, nil
					})
				} else {
					store.record(ctx, previous, completed)
				}
				body := []byte(fmt.Sprintf(`{"model":"gpt-6-sol","client_metadata":%s,"previous_response_id":"resp_old_window","input":[{"role":"user","content":"current context"}]}`, tc.metadata))
				prepared, _ := store.prepare(ctx, body)
				var payload struct {
					Previous string            `json:"previous_response_id"`
					Input    []json.RawMessage `json:"input"`
				}
				require.NoError(t, json.Unmarshal(prepared, &payload))
				require.Empty(t, payload.Previous)
				if tc.wantRoot {
					require.Len(t, payload.Input, 1)
					require.NotContains(t, string(prepared), "old history")
					require.NotContains(t, string(prepared), "old answer")
				} else {
					require.Len(t, payload.Input, 3)
				}
				require.Contains(t, string(prepared), "current context")
			})
		}
	}
}
