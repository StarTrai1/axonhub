package antigravity

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/transformer/gemini"
)

func TestAntigravityTrailingReasoningBoundary(t *testing.T) {
	const user = `{"role":"user","parts":[{"text":"hello"}]}`
	const thought = `{"role":"model","parts":[{"text":"thinking","thought":true,"thoughtSignature":"sig"}]}`
	for _, tc := range []struct { name, model, contents string; count int }{
		{"trailing thoughts", "gemini-3.8-flash", user+","+thought+","+thought, 1},
		{"single turn", "gemini-3.8-flash", thought, 1},
		{"earlier signed thought", "gemini-3.8-flash", user+","+thought+","+user+","+thought, 3},
		{"visible text", "gemini-3.8-flash", user+`,{"role":"model","parts":[{"text":"thinking","thought":true},{"text":"answer"}]}`, 2},
		{"tool call", "gemini-3.8-flash", user+`,{"role":"model","parts":[{"thought":true,"functionCall":{"name":"lookup","args":{}}}]}`, 2},
		{"media", "gemini-3.8-flash", user+`,{"role":"model","parts":[{"thought":true,"inlineData":{"mimeType":"image/png","data":"aW1n"}}]}`, 2},
		{"claude untouched by boundary", "claude-sonnet-4-6", user+","+thought, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var request gemini.GenerateContentRequest
			require.NoError(t, json.Unmarshal([]byte(`{"contents":[`+tc.contents+`]}`), &request))
			adapter := &Transformer{}
			require.NoError(t, adapter.patchGeminiRequest(t.Context(), &request, &llm.Request{Model: tc.model}))
			require.Len(t, request.Contents, tc.count)
			if tc.name == "earlier signed thought" {
				require.Equal(t, "sig", request.Contents[1].Parts[0].ThoughtSignature)
			}
		})
	}
}
