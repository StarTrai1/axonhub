package responses

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer/openai"
)

func TestCodex156FileImageSurvivesResponsesConversion(t *testing.T) {
	body := []byte(`{"model":"gpt-6-sol","input":[{"role":"user","content":[{"type":"input_text","text":"describe"},{"type":"input_image","file_id":"file_123","detail":"original"}]},{"type":"function_call_output","call_id":"call_image","output":[{"type":"input_image","file_id":"file_456","detail":"high"}]}]}`)
	request, err := NewInboundTransformer().TransformRequest(t.Context(), &httpclient.Request{Body: body})
	require.NoError(t, err)
	outbound, err := NewOutboundTransformer("https://example.test/v1", "fake")
	require.NoError(t, err)
	wire, err := outbound.TransformRequest(t.Context(), request)
	require.NoError(t, err)
	require.Equal(t, "file_123", gjson.GetBytes(wire.Body, "input.0.content.1.file_id").String())
	require.Equal(t, "original", gjson.GetBytes(wire.Body, "input.0.content.1.detail").String())
	require.False(t, gjson.GetBytes(wire.Body, "input.0.content.1.image_url").Exists())
	require.Equal(t, "file_456", gjson.GetBytes(wire.Body, "input.1.output.0.file_id").String())
	chat, err := openai.NewOutboundTransformer("https://example.test/v1", "fake")
	require.NoError(t, err)
	_, err = chat.TransformRequest(t.Context(), request)
	require.ErrorContains(t, err, "image file_id")
}
