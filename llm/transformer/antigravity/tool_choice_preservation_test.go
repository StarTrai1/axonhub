package antigravity

import (
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/transformer/gemini"
)

func TestAntigravityPreservesExplicitToolChoice(t *testing.T) {
	for _, mode := range []string{"NONE", "ANY", "AUTO", "VALIDATED", ""} {
		t.Run(mode, func(t *testing.T) {
			source := &llm.Request{Model: "gemini-2.5-flash", Messages: []llm.Message{{Role: "user", Content: llm.MessageContent{Content: lo.ToPtr("hello")}}}}
			req := &gemini.GenerateContentRequest{
				Tools:      []*gemini.Tool{{FunctionDeclarations: []*gemini.FunctionDeclaration{{Name: "selected"}}}},
				ToolConfig: &gemini.ToolConfig{FunctionCallingConfig: &gemini.FunctionCallingConfig{
					Mode: mode, AllowedFunctionNames: []string{"selected"},
				}},
			}
			err := (&Transformer{}).patchGeminiRequest(t.Context(), req, source)
			require.NoError(t, err)
			want := mode
			if mode == "AUTO" || mode == "" {
				want = "VALIDATED"
			}
			require.Equal(t, want, req.ToolConfig.FunctionCallingConfig.Mode)
			require.Equal(t, []string{"selected"}, req.ToolConfig.FunctionCallingConfig.AllowedFunctionNames)
		})
	}
}
