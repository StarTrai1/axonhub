package shared

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReadCodexMemoryTask(t *testing.T) {
	for _, tc := range []struct {
		name, turn, subagent string
		want CodexMemoryTask
	}{
		{"extract takes precedence", `{"request_kind":"memory","thread_source":"memory_consolidation"}`, "", CodexMemoryExtract},
		{"consolidation", `{"request_kind":"turn","thread_source":"memory_consolidation"}`, "", CodexMemoryConsolidate},
		{"consolidation prewarm", `{"request_kind":"prewarm","thread_source":"memory_consolidation"}`, "", CodexMemoryConsolidate},
		{"consolidation compaction", `{"request_kind":"compaction","thread_source":"memory_consolidation"}`, "", CodexMemoryConsolidate},
		{"subagent marker", `{"request_kind":"turn"}`, "memory_consolidation", CodexMemoryConsolidate},
		{"ordinary medium", `{"request_kind":"turn","thread_source":"user","reasoning_effort":"medium"}`, "", CodexMemoryNone},
		{"ordinary compaction", `{"request_kind":"compaction","thread_source":"user"}`, "", CodexMemoryNone},
		{"other subagent", `{"request_kind":"turn","thread_source":"subagent"}`, "reviewer", CodexMemoryNone},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(fmt.Sprintf(`{"model":"gpt-6.1-terra","reasoning":{"effort":"medium"},"input":"memory_consolidation","client_metadata":{"x-codex-turn-metadata":%q,"x-openai-subagent":%q}}`, tc.turn, tc.subagent))
			before := append([]byte(nil), body...)
			task, known := ReadCodexMemoryTask(http.Header{"X-Codex-Turn-Metadata": {`{"request_kind":"prewarm"}`}, "X-Openai-Subagent": {"memory_consolidation"}}, body)
			require.True(t, known)
			require.Equal(t, tc.want, task)
			require.Equal(t, before, body)
			task, known = ReadCodexMemoryTask(http.Header{"X-Codex-Turn-Metadata": {tc.turn}, "X-Openai-Subagent": {tc.subagent}}, nil)
			require.True(t, known)
			require.Equal(t, tc.want, task)
		})
	}
	task, known := ReadCodexMemoryTask(nil, []byte(`{"model":"gpt-6.1-terra","reasoning":{"effort":"medium"},"input":"request_kind=memory"}`))
	require.False(t, known)
	require.Equal(t, CodexMemoryNone, task)
}
