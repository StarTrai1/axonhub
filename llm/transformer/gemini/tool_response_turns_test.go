package gemini

import (
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
)

func TestGeminiToolMediaTurnsKeepParallelResultsAndHistoryPrefix(t *testing.T) {
	for _, model := range []string{"gemini-2.5-pro", "gemini-3-pro-preview"} {
		t.Run(model, func(t *testing.T) {
			req := &llm.Request{Model: model, Messages: []llm.Message{
				{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "call_a", Type: "function", Function: llm.FunctionCall{Name: "lookup", Arguments: `{}`}}, {ID: "call_b", Type: "function", Function: llm.FunctionCall{Name: "lookup", Arguments: `{}`}}}},
				{Role: "tool", ToolCallID: lo.ToPtr("call_a"), Content: llm.MessageContent{MultipleContent: []llm.MessageContentPart{{Type: "text", Text: lo.ToPtr("result a")}, {Type: "image_url", ImageURL: &llm.ImageURL{URL: "https://example.com/image.png"}}}}},
				{Role: "tool", ToolCallID: lo.ToPtr("call_b"), Content: llm.MessageContent{Content: lo.ToPtr("result b")}},
			}}
			before := convertLLMToGeminiRequest(req)
			require.Len(t, before.Contents, 3)
			require.Len(t, before.Contents[1].Parts, 2)
			require.Equal(t, "call_a", before.Contents[1].Parts[0].FunctionResponse.ID)
			require.Equal(t, "call_b", before.Contents[1].Parts[1].FunctionResponse.ID)
			require.Equal(t, "https://example.com/image.png", before.Contents[2].Parts[0].FileData.FileURI)
			req.Messages = append(req.Messages, llm.Message{Role: "user", Content: llm.MessageContent{Content: lo.ToPtr("continue")}})
			after := convertLLMToGeminiRequest(req)
			require.Equal(t, before.Contents, after.Contents[:3])
		})
	}
}
