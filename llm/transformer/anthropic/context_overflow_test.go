package anthropic

import (
	"github.com/looplj/axonhub/llm/auth"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestContextOverflowPreservesRecoverySignal(t *testing.T) {
	outbound, err := NewOutboundTransformerWithConfig(&Config{Type: PlatformDirect, BaseURL: "https://synthetic.invalid", APIKeyProvider: auth.NewStaticKeyProvider("synthetic")})
	require.NoError(t, err)
	errorBody := []byte(`{"type":"error","error":{"type":"invalid_request_error","message":"prompt is too long: 220000 tokens > 200000 maximum"}}`)
	require.Equal(t, "context_length_exceeded", outbound.TransformError(t.Context(), &httpclient.Error{StatusCode: 400, Body: errorBody}).Detail.Code)
	require.Equal(t, "context_length_exceeded", parseAnthropicStreamErrorEvent(&httpclient.StreamEvent{Type: "error", Data: errorBody}).Detail.Code)
	require.Equal(t, "custom", anthropicContextOverflowCode("custom", "invalid_request_error", "prompt is too long"))
	require.Empty(t, anthropicContextOverflowCode("", "authentication_error", "prompt is too long"))
	require.Equal(t, "length", *convertToLlmFinishReason(lo.ToPtr("model_context_window_exceeded")))
	stream := newOutboundStream(nil, PlatformDirect)
	response, err := stream.transformStreamChunk(&httpclient.StreamEvent{Type: "message_delta", Data: []byte(`{"type":"message_delta","delta":{"stop_reason":"model_context_window_exceeded"}}`)})
	require.NoError(t, err)
	require.Equal(t, "length", *response.Choices[0].FinishReason)
}
