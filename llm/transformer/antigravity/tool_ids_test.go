package antigravity

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/transformer/gemini"
)

func TestClaudeToolIDsPreserveResultPairingAcrossCollisions(t *testing.T) {
	for _, model := range []string{"claude-sonnet-4-6", "gemini-3-pro"} {
		t.Run(model, func(t *testing.T) {
			ids := []string{"call.a", "call/a", "call_a", "call_a_1", "call.a"}
			req := &gemini.GenerateContentRequest{}
			for _, id := range ids {
				req.Contents = append(req.Contents,
					&gemini.Content{Role: "model", Parts: []*gemini.Part{{FunctionCall: &gemini.FunctionCall{ID: id, Name: "lookup"}}}},
					&gemini.Content{Role: "user", Parts: []*gemini.Part{{FunctionResponse: &gemini.FunctionResponse{ID: id, Name: "lookup", Response: map[string]any{"original": id}}}}},
				)
			}
			transformer := &Transformer{}
			require.NoError(t, transformer.patchGeminiRequest(context.Background(), req, &llm.Request{Model: model}))
			seen := make(map[string]string)
			for i, original := range ids {
				call := req.Contents[i*2].Parts[0].FunctionCall
				result := req.Contents[i*2+1].Parts[0].FunctionResponse
				require.Equal(t, call.ID, result.ID)
				require.Equal(t, original, result.Response["original"])
				if model == "gemini-3-pro" || !invalidClaudeToolIDChars.MatchString(original) {
					require.Equal(t, original, call.ID)
				} else {
					require.NotRegexp(t, invalidClaudeToolIDChars, call.ID)
				}
				if previous, ok := seen[call.ID]; ok {
					require.Equal(t, original, previous)
				}
				seen[call.ID] = original
			}
		})
	}
}
