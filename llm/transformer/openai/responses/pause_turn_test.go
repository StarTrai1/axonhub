package responses

import (
	"encoding/json"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/streams"
)

func TestPauseTurnIsIncompleteWithoutInventedReason(t *testing.T) {
	adapter := NewInboundTransformer()
	response := &llm.Response{
		ID: "resp_pause", Model: "claude-test", Object: "chat.completion",
		Choices: []llm.Choice{{Index: 0, FinishReason: lo.ToPtr("pause_turn"), Message: &llm.Message{
			Role: "assistant", Content: llm.MessageContent{Content: lo.ToPtr("server tool paused")},
		}}},
	}
	wire, err := adapter.TransformResponse(t.Context(), response)
	require.NoError(t, err)
	require.Equal(t, "incomplete", gjson.GetBytes(wire.Body, "status").String())
	require.Empty(t, gjson.GetBytes(wire.Body, "incomplete_details.reason").String())
	response.Object = "chat.completion.chunk"
	response.Choices[0].Delta = response.Choices[0].Message
	response.Choices[0].Message = nil
	stream, err := adapter.TransformStream(t.Context(), streams.SliceStream([]*llm.Response{response, llm.DoneResponse}))
	require.NoError(t, err)
	defer stream.Close()
	found := false
	for stream.Next() {
		event := stream.Current()
		if event.Type == "response.incomplete" {
			var parsed StreamEvent
			require.NoError(t, json.Unmarshal(event.Data, &parsed))
			require.Equal(t, "incomplete", gjson.GetBytes(event.Data, "response.status").String())
			require.Empty(t, gjson.GetBytes(event.Data, "response.incomplete_details.reason").String())
			found = true
		}
		require.NotEqual(t, "response.completed", event.Type)
	}
	require.NoError(t, stream.Err())
	require.True(t, found)
}
