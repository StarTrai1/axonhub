package codex

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

func TestCodexOutboundPreservesResponseExclusionsWithoutGenericPassthrough(t *testing.T) {
	for _, baseURL := range []string{"https://chatgpt.com/backend-api/codex#", "https://relay.example/v1"} {
		t.Run(baseURL, func(t *testing.T) {
			inbound := memoryConsolidationRequest()
			inbound.Headers.Add(ExcludeHeader, "tools")
			inbound.Headers.Add(ExcludeHeader, "usage.attribution")
			request, err := responses.NewInboundTransformer().TransformRequest(t.Context(), inbound)
			require.NoError(t, err)
			request.RawRequest = inbound
			outbound := newTestCodexOutboundForBaseURL(t, baseURL)
			providerRequest, err := outbound.TransformRequest(t.Context(), request)
			require.NoError(t, err)
			providerRequest = outbound.FinalizeTransportRequest(providerRequest)
			require.Equal(t, "tools,usage.attribution", providerRequest.Headers.Get(ExcludeHeader))
			require.Equal(t, []string{"tools", "usage.attribution"}, inbound.Headers.Values(ExcludeHeader))
		})
	}
}
