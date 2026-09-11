package gemini

import (
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/streams"
)

func TestEmptyFinishReasonDoesNotFinishGeminiStream(t *testing.T) {
	t.Parallel()

	chunks := make([]*llm.Response, 0, 3)
	for _, reason := range []*string{nil, lo.ToPtr(""), lo.ToPtr("stop")} {
		chunks = append(chunks, &llm.Response{
			ID: "chatcmpl_finish", Model: "gemini-test", Object: "chat.completion.chunk",
			Choices: []llm.Choice{{
				Index: 0,
				Delta: &llm.Message{
					Role: "assistant", Content: llm.MessageContent{Content: lo.ToPtr("text")},
				},
				FinishReason: reason,
			}},
		})
	}
	output, err := NewInboundTransformer().TransformStream(t.Context(), streams.SliceStream(chunks))
	require.NoError(t, err)
	events, err := streams.All(output)
	require.NoError(t, err)
	require.Len(t, events, 3)
	for _, event := range events[:2] {
		require.False(t, gjson.GetBytes(event.Data, "candidates.0.finishReason").Exists())
		require.Equal(t, "text", gjson.GetBytes(event.Data, "candidates.0.content.parts.0.text").String())
	}
	require.Equal(t, "STOP", gjson.GetBytes(events[2].Data, "candidates.0.finishReason").String())
}
