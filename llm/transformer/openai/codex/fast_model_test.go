package codex

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

func TestCodexFastAliasKeepsGPT6PassThroughConstraints(t *testing.T) {
	outbound := &OutboundTransformer{}
	for _, model := range []string{"gpt-6-sol-fast", "gpt-6-luna-fast", "gpt-6.1-sol-fast"} {
		for _, effort := range []string{"none", "high", "minimal"} {
			request := &llm.Request{
				Model:     model,
				APIFormat: llm.APIFormatOpenAIResponse,
				RawRequest: &httpclient.Request{
					Body: []byte(fmt.Sprintf(`{"model":%q,"reasoning":{"effort":%q},"temperature":0.5}`, model, effort)),
				},
			}
			wantPassThrough := effort == "none" && model != "gpt-6.1-sol-fast"
			require.Equal(t, wantPassThrough, outbound.AllowPassThroughBody(t.Context(), request, nil), model+"/"+effort)
		}
	}
}

func TestCodexFastAliasAfterPassThrough(t *testing.T) {
	for _, model := range []string{"gpt-6-sol", "gpt-6-luna", "gpt-6.1-sol"} {
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
		require.Same(t, request, outbound.normalizeFastModelRequest(request))
		wire := outbound.FinalizeTransportRequest(request)
		require.Equal(t, model, gjson.GetBytes(wire.Body, "model").String())
		require.False(t, gjson.GetBytes(wire.Body, "service_tier").Exists())
	}
}

func TestCodexUltrafastAndRelayModelNames(t *testing.T) {
	for _, model := range []string{"gpt-6-astra", "gpt-6.1-sol"} {
		for _, speed := range []string{"fast", "ultrafast"} {
			alias := model + "-" + speed
			for _, preserve := range []bool{false, true} {
				outbound := newTestCodexOutbound(t)
				outbound.official = false
				outbound.preserveSpeedModel = preserve
				request := &llm.Request{Model: alias, Messages: []llm.Message{{Role: "user", Content: llm.MessageContent{Content: lo.ToPtr("synthetic")}}}}
				wire, err := outbound.TransformRequest(t.Context(), request)
				require.NoError(t, err)
				wire = outbound.FinalizeTransportRequest(wire)
				wantModel, wantTier := model, "priority"
				if speed == "ultrafast" { wantTier = "ultrafast" }
				if preserve { wantModel, wantTier = alias, "" }
				require.Equal(t, wantModel, gjson.GetBytes(wire.Body, "model").String())
				require.Equal(t, wantTier, gjson.GetBytes(wire.Body, "service_tier").String())
				// Finalization also handles native body pass-through without changing history.
				raw := &httpclient.Request{Body: []byte(fmt.Sprintf(`{"model":%q,"service_tier":"default","previous_response_id":"resp_keep","input":[{"type":"compaction","encrypted_content":"ciphertext"}]}`, alias))}
				got := outbound.FinalizeTransportRequest(raw)
				require.Equal(t, wantModel, gjson.GetBytes(got.Body, "model").String())
				require.Equal(t, "default", gjson.GetBytes(got.Body, "service_tier").String())
				require.Equal(t, "resp_keep", gjson.GetBytes(got.Body, "previous_response_id").String())
				require.Equal(t, "ciphertext", gjson.GetBytes(got.Body, "input.0.encrypted_content").String())
			}
		}
	}
}
