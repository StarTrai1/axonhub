package deepseek

import (
	"encoding/json"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
)

func TestDeveloperInstructionsKeepTheirHistoryPosition(t *testing.T) {
	for _, scenario := range []struct {
		name    string
		content llm.MessageContent
		role    string
		text    string
	}{
		{"string", llm.MessageContent{Content: lo.ToPtr("new instruction")}, "system", "new instruction"},
		{
			"text parts",
			llm.MessageContent{MultipleContent: []llm.MessageContentPart{{Type: "text", Text: lo.ToPtr("first")}, {Type: "text", Text: lo.ToPtr("second")}}},
			"system",
			"first\nsecond",
		},
		{
			"mixed content stays intact",
			llm.MessageContent{MultipleContent: []llm.MessageContentPart{
				{Type: "text", Text: lo.ToPtr("keep the image")},
				{Type: "image_url", ImageURL: &llm.ImageURL{URL: "https://example.com/image.png"}},
			}},
			"developer",
			"",
		},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			req := &llm.Request{
				Model:           "deepseek-v4-pro",
				ReasoningEffort: "high",
				Messages: []llm.Message{
					{Role: "user", Content: llm.MessageContent{Content: lo.ToPtr("earlier question")}},
					{Role: "assistant", Content: llm.MessageContent{Content: lo.ToPtr("earlier answer")}, ReasoningContent: lo.ToPtr("retained reasoning")},
					{Role: "developer", Content: scenario.content},
					{Role: "user", Content: llm.MessageContent{Content: lo.ToPtr("next question")}},
				},
			}
			before, err := json.Marshal(req)
			require.NoError(t, err)
			outbound, err := NewOutboundTransformer("https://api.deepseek.com", "test-key")
			require.NoError(t, err)
			wire, err := outbound.TransformRequest(t.Context(), req)
			require.NoError(t, err)
			var body Request
			require.NoError(t, json.Unmarshal(wire.Body, &body))
			require.Len(t, body.Messages, 4)
			require.Equal(t, "user", body.Messages[0].Role)
			require.Equal(t, "assistant", body.Messages[1].Role)
			require.Equal(t, "retained reasoning", *body.Messages[1].ReasoningContent)
			require.Equal(t, scenario.role, body.Messages[2].Role)
			require.Equal(t, "user", body.Messages[3].Role)
			if scenario.role == "system" {
				require.NotNil(t, body.Messages[2].Content.Content)
				require.Equal(t, scenario.text, *body.Messages[2].Content.Content)
			} else {
				require.Len(t, body.Messages[2].Content.MultipleContent, 2)
				require.Equal(t, "https://example.com/image.png", body.Messages[2].Content.MultipleContent[1].ImageURL.URL)
			}
			after, err := json.Marshal(req)
			require.NoError(t, err)
			require.JSONEq(t, string(before), string(after))
		})
	}
}
