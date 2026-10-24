package orchestrator

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/oauth"
	"github.com/looplj/axonhub/llm/transformer/openai/codex"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

func TestNativeResponsesProbeRetainsCodexContractThroughRelayFinalization(t *testing.T) {
	for _, model := range []string{"gpt-6.1-sol", "gpt-6-sol", "gpt-6-luna", "gpt-6-astra", "provider/gpt-6.1-sol"} {
		t.Run(model, func(t *testing.T) {
			inbound, request, err := (&TestChannelOrchestrator{}).buildNativeChannelTestInput(t.Context(), model, false, "Answer briefly", "Reply OK", false, llm.APIFormatOpenAIResponse)
			require.NoError(t, err)
			require.NotEmpty(t, request.Headers.Get(codex.SessionHeaderHyphen))
			require.NotEmpty(t, request.Headers.Get(codex.TurnMetadataHeader))
			require.Empty(t, request.Headers.Get(responses.ResponsesLiteHeader))
			provider, err := codex.NewOutboundTransformer(codex.Params{BaseURL: "https://relay.example/v1", TokenProvider: oauth.NewStaticTokenProvider(&oauth.OAuthCredentials{AccessToken: "synthetic"})})
			require.NoError(t, err)
			parsed, err := inbound.TransformRequest(t.Context(), request)
			require.NoError(t, err)
			outgoing, err := provider.TransformRequest(t.Context(), parsed)
			require.NoError(t, err)
			// The native probe uses body passthrough in production. Exercise the
			// finalizer on those bytes, after the normal header transformation.
			outgoing.Body = request.Body
			wire := provider.FinalizeTransportRequest(outgoing)
			require.Equal(t, model, gjson.GetBytes(wire.Body, "model").String())
			require.Equal(t, "low", gjson.GetBytes(wire.Body, "reasoning.effort").String())
			require.Equal(t, "low", gjson.GetBytes(wire.Body, "text.verbosity").String())
			require.Equal(t, "auto", gjson.GetBytes(wire.Body, "tool_choice").String())
			require.Equal(t, "[]", gjson.GetBytes(wire.Body, "tools").Raw)
			require.False(t, gjson.GetBytes(wire.Body, "parallel_tool_calls").Bool())
			require.True(t, gjson.GetBytes(wire.Body, "stream").Bool())
			require.False(t, gjson.GetBytes(wire.Body, "store").Bool())
			require.Equal(t, "Answer briefly", gjson.GetBytes(wire.Body, "instructions").String())
			require.Equal(t, "Reply OK", gjson.GetBytes(wire.Body, "input.0.content.0.text").String())
			require.NotEmpty(t, gjson.GetBytes(wire.Body, "prompt_cache_key").String())
			require.False(t, gjson.GetBytes(wire.Body, "max_output_tokens").Exists())
		})
	}
}
