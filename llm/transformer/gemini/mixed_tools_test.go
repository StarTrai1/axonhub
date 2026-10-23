package gemini

import (
	"github.com/looplj/axonhub/llm"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestMixedHostedAndClientToolsDeclareInvocations(t *testing.T) {
	function := llm.Tool{Type: "function", Function: llm.Function{Name: "local"}}
	hosted := llm.Tool{Type: llm.ToolTypeWebSearch}
	for _, tools := range [][]llm.Tool{{function}, {hosted}, {function, hosted}} {
		req := convertLLMToGeminiRequest(&llm.Request{Tools: tools})
		got := req.ToolConfig != nil && req.ToolConfig.IncludeServerSideToolInvocations
		require.Equal(t, len(tools) == 2, got)
	}
}
