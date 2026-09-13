package openai

import (
	"encoding/json"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
)

func TestConvertedToolImagesFollowAllToolReplies(t *testing.T) {
	request := &llm.Request{
		Model:     "gpt-6-astra",
		APIFormat: llm.APIFormatAnthropicMessage,
		Messages: []llm.Message{
			{Role: "assistant", ToolCalls: []llm.ToolCall{
				{ID: "call_image", Type: "function", Function: llm.FunctionCall{Name: "capture", Arguments: "{}"}},
				{ID: "call_text", Type: "function", Function: llm.FunctionCall{Name: "inspect", Arguments: "{}"}},
			}},
			{Role: "tool", ToolCallID: lo.ToPtr("call_image"), Content: llm.MessageContent{MultipleContent: []llm.MessageContentPart{
				{Type: "text", Text: lo.ToPtr("capture result")},
				{Type: "image_url", ImageURL: &llm.ImageURL{URL: "data:image/png;base64,aW1hZ2U=", Detail: lo.ToPtr("high")}},
			}}},
			{Role: "tool", ToolCallID: lo.ToPtr("call_text"), Content: llm.MessageContent{Content: lo.ToPtr("inspection result")}},
			{Role: "user", Content: llm.MessageContent{Content: lo.ToPtr("continue")}},
		},
	}
	original, err := json.Marshal(request.Messages)
	require.NoError(t, err)
	sent := RequestFromLLM(t.Context(), request, ReasoningFieldAll)
	require.Len(t, sent.Messages, 5)
	require.Equal(t, []string{"assistant", "tool", "tool", "user", "user"}, lo.Map(sent.Messages, func(message Message, _ int) string { return message.Role }))
	require.Equal(t, "call_image", *sent.Messages[1].ToolCallID)
	require.Len(t, sent.Messages[1].Content.MultipleContent, 1)
	require.Equal(t, "capture result", *sent.Messages[1].Content.MultipleContent[0].Text)
	require.Equal(t, "call_text", *sent.Messages[2].ToolCallID)
	require.Equal(t, "inspection result", *sent.Messages[2].Content.Content)
	images := sent.Messages[3].Content.MultipleContent
	require.Len(t, images, 2)
	require.Contains(t, *images[0].Text, "call_image")
	require.Equal(t, "data:image/png;base64,aW1hZ2U=", images[1].ImageURL.URL)
	require.NotNil(t, images[1].ImageURL.Detail)
	require.Equal(t, "high", *images[1].ImageURL.Detail)
	require.Equal(t, "continue", *sent.Messages[4].Content.Content)
	after, err := json.Marshal(request.Messages)
	require.NoError(t, err)
	require.Equal(t, original, after)
}

func TestConvertedImageOnlyToolReplyStaysNonEmpty(t *testing.T) {
	request := &llm.Request{
		APIFormat: llm.APIFormatOpenAIResponse,
		Messages: []llm.Message{{Role: "tool", ToolCallID: lo.ToPtr("call_image"), Content: llm.MessageContent{MultipleContent: []llm.MessageContentPart{
			{Type: "image_url", ImageURL: &llm.ImageURL{URL: "https://example.com/result.png"}},
		}}}},
	}
	sent := RequestFromLLM(t.Context(), request, ReasoningFieldAll)
	require.Len(t, sent.Messages, 2)
	require.Equal(t, "tool", sent.Messages[0].Role)
	require.NotEmpty(t, *sent.Messages[0].Content.Content)
	require.Empty(t, sent.Messages[0].Content.MultipleContent)
	require.Equal(t, "user", sent.Messages[1].Role)
	require.Equal(t, "https://example.com/result.png", sent.Messages[1].Content.MultipleContent[1].ImageURL.URL)

	request.APIFormat = llm.APIFormatOpenAIChatCompletion
	native := RequestFromLLM(t.Context(), request, ReasoningFieldAll)
	require.Len(t, native.Messages, 1, "native Chat requests retain caller-controlled content")
	require.Equal(t, "image_url", native.Messages[0].Content.MultipleContent[0].Type)
}
