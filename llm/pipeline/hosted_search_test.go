package pipeline

import (
	"testing"

	"github.com/looplj/axonhub/llm"
	"github.com/stretchr/testify/require"
)

func TestTerminalHostedSearchIsNotEmpty(t *testing.T) {
	for _, status := range []string{"completed", "failed", "incomplete", "in_progress", ""} {
		response := &llm.Response{TransformerMetadata: map[string]any{"openai_responses_web_search_calls": []map[string]string{{"id": "ws_1", "type": "web_search_call", "status": status}}}}
		require.Equal(t, status == "completed" || status == "failed" || status == "incomplete", hasResponseContent(response))
	}
}
