package codex

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

func TestCodexFastAliasKeepsGPT6PassThroughConstraints(t *testing.T) {
	outbound := &OutboundTransformer{}
	for _, model := range []string{"gpt-6-sol-fast", "gpt-6-luna-fast"} {
		for _, effort := range []string{"none", "high", "minimal"} {
			request := &llm.Request{
				Model:      model,
				APIFormat:  llm.APIFormatOpenAIResponse,
				RawRequest: &httpclient.Request{
					Body: []byte(fmt.Sprintf(`{"model":%q,"reasoning":{"effort":%q},"temperature":0.5}`, model, effort)),
				},
			}
			require.Equal(t, effort == "none", outbound.AllowPassThroughBody(t.Context(), request, nil), model+"/"+effort)
		}
	}
}

func TestCodexFastAliasAfterPassThrough(t *testing.T) {
	for _, model := range []string{"gpt-6-sol", "gpt-6-luna"} {
		for _, transport := range []string{responses.TransportHTTP, responses.TransportWebSocket} {
			for _, tier := range []string{"", "flex"} {
				t.Run(model+"/"+transport+"/"+tier, func(t *testing.T) {
					outbound := &OutboundTransformer{official: true, transport: transport}
					body := []byte(fmt.Sprintf(`{"model":%q,"service_tier":%q,"input":[{"type":"configuration_update","reasoning":{"effort":"high"}}],"client_metadata":{"custom":"kept"},"unknown_extension":{"enabled":true}}`, model+"-fast", tier))
					request := &httpclient.Request{Body: body, Headers: http.Header{"X-Codex-Routing-Hint": {"model=old"}}}
					result := outbound.FinalizeTransportRequest(request)
					wantTier := tier
					if wantTier == "" {
						wantTier = "priority"
					}
					require.Equal(t, model, gjson.GetBytes(result.Body, "model").String())
					require.Equal(t, wantTier, gjson.GetBytes(result.Body, "service_tier").String())
					require.Equal(t, "model="+model+";tier="+wantTier, result.Headers.Get(RoutingHintHeader))
					require.Equal(t, "high", gjson.GetBytes(result.Body, "input.0.reasoning.effort").String())
					require.Equal(t, "kept", gjson.GetBytes(result.Body, "client_metadata.custom").String())
					require.True(t, gjson.GetBytes(result.Body, "unknown_extension.enabled").Bool())
					require.Equal(t, body, request.Body)
					require.Equal(t, "model=old", request.Headers.Get(RoutingHintHeader))
					require.Equal(t, result, outbound.FinalizeTransportRequest(result))
				})
			}
		}
	}
}

func TestCodexFastAliasPreservesRelayHeadersAndUnknownModels(t *testing.T) {
	outbound := &OutboundTransformer{}
	request := &httpclient.Request{Body: []byte(`{"model":"gpt-6-sol-fast"}`), Headers: http.Header{"X-Request-Id": {"test"}}}
	result := outbound.FinalizeTransportRequest(request)
	require.Equal(t, "gpt-6-sol", gjson.GetBytes(result.Body, "model").String())
	require.Equal(t, "priority", gjson.GetBytes(result.Body, "service_tier").String())
	require.Empty(t, result.Headers.Get(RoutingHintHeader))
	require.Equal(t, "test", result.Headers.Get("X-Request-Id"))
	for _, model := range []string{"gpt-6-sol", "custom-fast", "codex-auto-review-fast"} {
		request := &httpclient.Request{Body: []byte(fmt.Sprintf(`{"model":%q}`, model))}
		require.Same(t, request, outbound.FinalizeTransportRequest(request))
	}
}
