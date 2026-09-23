package responses

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer/openai"
)

func TestResponsesVideoInputSurvivesProtocolConversion(t *testing.T) {
	for _, video := range []string{
		`{"type":"input_video","video_url":"https://example.test/video.mp4","processing":"agentic"}`,
		`{"type":"input_video","video_url":{"url":"https://example.test/video.mp4","processing":"agentic"}}`,
	} {
		body := []byte(`{"model":"video-model","input":[{"role":"user","content":[{"type":"input_text","text":"describe"},` + video + `]}]}`)
		request, err := NewInboundTransformer().TransformRequest(t.Context(), &httpclient.Request{Body: body})
		require.NoError(t, err)
		outbound, err := openai.NewOutboundTransformer("https://example.test/v1", "fake")
		require.NoError(t, err)
		wire, err := outbound.TransformRequest(t.Context(), request)
		require.NoError(t, err)
		require.Equal(t, "https://example.test/video.mp4", gjson.GetBytes(wire.Body, "messages.0.content.1.video_url.url").String())
		require.Equal(t, "agentic", gjson.GetBytes(wire.Body, "messages.0.content.1.video_url.processing").String())
		responsesOutbound, err := NewOutboundTransformer("https://example.test/v1", "fake")
		require.NoError(t, err)
		wire, err = responsesOutbound.TransformRequest(t.Context(), request)
		require.NoError(t, err)
		require.Equal(t, "https://example.test/video.mp4", gjson.GetBytes(wire.Body, "input.0.content.1.video_url").String())
		require.Equal(t, "agentic", gjson.GetBytes(wire.Body, "input.0.content.1.processing").String())
	}
}
