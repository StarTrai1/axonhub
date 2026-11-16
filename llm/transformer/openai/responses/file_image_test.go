package responses

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm"
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

func TestCodex169FileAudioSurvivesResponsesConversion(t *testing.T) {
	for _, kind := range []string{"function_call_output", "custom_tool_call_output"} {
		body := []byte(`{"model":"gpt-6.1-sol","input":[{"role":"user","content":[{"type":"input_audio","file_id":"file_user"}]},{"type":"` + kind + `","call_id":"call_audio","output":[{"type":"input_text","text":"recording"},{"type":"input_audio","file_id":"file_tool"},{"type":"input_audio","audio_url":"data:audio/wav;base64,QQ=="}]}]}`)
		request, err := NewInboundTransformer().TransformRequest(t.Context(), &httpclient.Request{Body: body})
		require.NoError(t, err)
		outbound, err := NewOutboundTransformer("https://example.test/v1", "fake")
		require.NoError(t, err)
		wire, err := outbound.TransformRequest(t.Context(), request)
		require.NoError(t, err)
		require.Equal(t, "file_user", gjson.GetBytes(wire.Body, "input.0.content.0.file_id").String())
		require.Equal(t, "file_tool", gjson.GetBytes(wire.Body, "input.1.output.1.file_id").String())
		require.Equal(t, "data:audio/wav;base64,QQ==", gjson.GetBytes(wire.Body, "input.1.output.2.audio_url").String())
		require.False(t, gjson.GetBytes(wire.Body, "input.1.output.1.audio_url").Exists())
		chat, err := openai.NewOutboundTransformer("https://example.test/v1", "fake")
		require.NoError(t, err)
		_, err = chat.TransformRequest(t.Context(), request)
		require.ErrorContains(t, err, "audio file_id")
	}
}

func TestAudioReferencesHaveDistinctConversationAnchors(t *testing.T) {
	anchor := func(audio llm.InputAudio) string {
		return conversationAnchor([]llm.Message{{Role: "user", Content: llm.MessageContent{MultipleContent: []llm.MessageContentPart{{Type: "input_audio", InputAudio: &audio}}}}})
	}
	require.NotEqual(t, anchor(llm.InputAudio{FileID: "file_one"}), anchor(llm.InputAudio{FileID: "file_two"}))
	require.NotEqual(t, anchor(llm.InputAudio{URL: "data:audio/wav;base64,QQ=="}), anchor(llm.InputAudio{URL: "data:audio/wav;base64,Qg=="}))
}

func TestCodex169InlineAudioIsNotSilentlyDroppedByChat(t *testing.T) {
	body := []byte(`{"model":"gpt-6.1-sol","input":[{"role":"user","content":[{"type":"input_audio","audio_url":"data:audio/wav;base64,QQ=="}]}]}`)
	request, err := NewInboundTransformer().TransformRequest(t.Context(), &httpclient.Request{Body: body})
	require.NoError(t, err)
	chat, err := openai.NewOutboundTransformer("https://example.test/v1", "fake")
	require.NoError(t, err)
	_, err = chat.TransformRequest(t.Context(), request)
	require.ErrorContains(t, err, "audio URL")
}
