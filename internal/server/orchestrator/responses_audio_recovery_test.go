package orchestrator

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodex169FileAudioCannotAuthorizeReadableHistoryRecovery(t *testing.T) {
	for _, kind := range []string{"function_call_output", "custom_tool_call_output"} {
		callKind := "function_call"
		if kind == "custom_tool_call_output" {
			callKind = "custom_tool_call"
		}
		body := []byte(`{"input":[{"role":"user","content":"listen"},{"type":"` + callKind + `","call_id":"call_audio","name":"listen","arguments":"{}","input":"hello"},{"type":"` + kind + `","call_id":"call_audio","output":[{"type":"input_audio","file_id":"file_private"}]}]}`)
		_, complete := responsesExplicitHistorySupportsRecovery(body)
		require.False(t, complete, "provider-owned media is not readable retained history")
	}
}
