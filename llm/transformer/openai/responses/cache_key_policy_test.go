package responses

import (
	"encoding/json"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

func TestNativeResponsesCacheKeyOmission(t *testing.T) {
	ctx := shared.WithSessionID(t.Context(), "session-independent-of-cache")
	outbound, err := NewOutboundTransformer("https://synthetic.invalid", "synthetic")
	require.NoError(t, err)
	for _, format := range []llm.APIFormat{llm.APIFormatOpenAIResponse, llm.APIFormatOpenAIResponseWebSocket, llm.APIFormatOpenAIChatCompletion} {
		t.Run(string(format), func(t *testing.T) {
			req := &llm.Request{Model: "synthetic", APIFormat: format, Messages: []llm.Message{{Role:"user", Content:llm.MessageContent{Content:lo.ToPtr("hello")}}}}
			wire, err := outbound.TransformRequest(ctx, req)
			require.NoError(t, err)
			var payload Request
			require.NoError(t, json.Unmarshal(wire.Body, &payload))
			if IsResponsesFormat(format) { require.Nil(t, payload.PromptCacheKey) } else { require.NotNil(t, payload.PromptCacheKey) }
			req.PromptCacheKey = lo.ToPtr("")
			wire, err = outbound.TransformRequest(ctx, req)
			require.NoError(t, err)
			require.NoError(t, json.Unmarshal(wire.Body, &payload))
			require.NotNil(t, payload.PromptCacheKey)
			require.Empty(t, *payload.PromptCacheKey)
		})
	}
}
