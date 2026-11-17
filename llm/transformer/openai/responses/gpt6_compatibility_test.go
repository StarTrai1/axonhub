package responses

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
)

func TestGPT6SolLunaSamplingFollowsEffectiveReasoning(t *testing.T) {
	outbound, err := NewOutboundTransformer("https://example.test/v1", "fake-key")
	require.NoError(t, err)
	for _, model := range []string{"gpt-6-sol", "gpt-6-luna", "gpt-6-astra", "gpt-6.1-sol", "gpt-6.1-terra", "gpt-6.2-sol", "gpt-6.2-astra"} {
		for _, tc := range []struct {
			name       string
			effort     string
			update     string
			wantSample bool
		}{
			{name: "default"},
			{name: "reasoning", effort: "high"},
			{name: "minimal", effort: "minimal"},
			{name: "none", effort: "none", wantSample: true},
			{name: "update to high", effort: "none", update: "high"},
			{name: "update to none", effort: "high", update: "none", wantSample: true},
		} {
			t.Run(model+"/"+tc.name, func(t *testing.T) {
				update := ""
				if tc.update != "" {
					update = fmt.Sprintf(`,{"type":"configuration_update","reasoning":{"effort":%q}}`, tc.update)
				}
				body := []byte(fmt.Sprintf(`{"model":%q,"reasoning":{"effort":%q},"temperature":0.7,"top_p":0.8,"top_logprobs":2,"include":["message.output_text.logprobs","reasoning.encrypted_content"],"input":[{"role":"user","content":"hello"}%s]}`, model, tc.effort, update))
				request, err := NewInboundTransformer().TransformRequest(t.Context(), &httpclient.Request{Body: body})
				require.NoError(t, err)
				request.RawRequest = &httpclient.Request{Body: body}
				wire, err := outbound.TransformRequest(t.Context(), request)
				require.NoError(t, err)
				supportsNone := model == "gpt-6-sol" || model == "gpt-6-luna" || model == "gpt-6.1-terra"
				wantSample := tc.wantSample && supportsNone
				for _, field := range []string{"temperature", "top_p", "top_logprobs"} {
					require.Equal(t, wantSample, gjson.GetBytes(wire.Body, field).Exists(), field)
				}
				include := gjson.GetBytes(wire.Body, "include").String()
				require.Contains(t, include, "reasoning.encrypted_content")
				require.Equal(t, wantSample, gjson.GetBytes(wire.Body, `include.#(=="message.output_text.logprobs")`).Exists())
				require.Equal(t, wantSample, outbound.AllowPassThroughBody(t.Context(), request, wire))
				wantEffort := tc.effort
				if tc.effort == "minimal" || (tc.effort == "none" && !supportsNone) {
					wantEffort = "low"
				}
				require.Equal(t, wantEffort, gjson.GetBytes(wire.Body, "reasoning.effort").String())
				if tc.update != "" {
					expectedUpdate := tc.update
					if tc.update == "none" && !supportsNone {
						expectedUpdate = "low"
					}
					require.Equal(t, expectedUpdate, gjson.GetBytes(wire.Body, "input.1.reasoning.effort").String())
				}
				require.Equal(t, string(body), string(request.RawRequest.Body))
			})
		}
	}
}

func TestGPT62ResponsesCacheMigrationFromChat(t *testing.T) {
	outbound, err := NewOutboundTransformer("https://example.test/v1", "fake-key")
	require.NoError(t, err)
	request := &llm.Request{
		Model:      "gpt-6.2-sol",
		APIFormat:  llm.APIFormatOpenAIChatCompletion,
		Messages:   []llm.Message{{Role: "user"}},
		RawRequest: &httpclient.Request{Body: []byte(`{"prompt_cache_retention":"24h"}`)},
	}
	wire, err := outbound.TransformRequest(t.Context(), request)
	require.NoError(t, err)
	require.Equal(t, "30m", gjson.GetBytes(wire.Body, "prompt_cache_options.ttl").String())
}

func TestGPT62ResponsesDoesNotReplayForeignOpaqueReasoning(t *testing.T) {
	outbound, err := NewOutboundTransformer("https://example.test/v1", "fake-key")
	require.NoError(t, err)
	request := &llm.Request{
		Model:     "gpt-6.2-sol",
		APIFormat: llm.APIFormatAnthropicMessage,
		Messages: []llm.Message{
			{Role: "assistant", ReasoningItems: []llm.ReasoningItem{{ID: "foreign", Signature: "opaque"}}},
			{Role: "user"},
		},
	}
	wire, err := outbound.TransformRequest(t.Context(), request)
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(wire.Body, `input.#(type=="reasoning")`).Exists())
}

func TestGPT62ResponsesCacheMigrationAndHistory(t *testing.T) {
	outbound, err := NewOutboundTransformer("https://example.test/v1", "fake-key")
	require.NoError(t, err)
	for _, model := range []string{"gpt-6.2-astra", "gpt-6.2-sol"} {
		for _, scenario := range []struct {
			name        string
			cache       string
			wantTTL     string
			passThrough bool
		}{
			{name: "legacy", cache: `"prompt_cache_retention":"24h"`, wantTTL: "30m"},
			{name: "explicit ttl wins", cache: `"prompt_cache_retention":"24h","prompt_cache_options":{"mode":"explicit","ttl":"12h"}`, wantTTL: "12h"},
			{name: "native", cache: `"prompt_cache_options":{"mode":"explicit","ttl":"12h"}`, wantTTL: "12h", passThrough: true},
		} {
			t.Run(model+"/"+scenario.name, func(t *testing.T) {
				body := []byte(fmt.Sprintf(`{"model":%q,%s,"store":false,"reasoning":{"effort":"low"},"include":["reasoning.encrypted_content"],"service_tier":"ultrafast","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"stable prefix","prompt_cache_breakpoint":{"mode":"explicit"}}]},{"type":"reasoning","id":"rs_native","summary":[],"encrypted_content":"opaque"},{"type":"function_call","call_id":"call_lookup","name":"lookup","arguments":"{}"},{"type":"function_call_output","call_id":"call_lookup","output":"result"},{"type":"configuration_update","reasoning":{"effort":"high"}},{"type":"message","role":"user","content":"continue"}]}`, model, scenario.cache))
				rawRequest := &httpclient.Request{Body: body}
				request, err := NewInboundTransformer().TransformRequest(t.Context(), rawRequest)
				require.NoError(t, err)
				request.RawRequest = rawRequest
				wire, err := outbound.TransformRequest(t.Context(), request)
				require.NoError(t, err)
				require.Equal(t, scenario.passThrough, outbound.AllowPassThroughBody(t.Context(), request, wire))
				parsed := gjson.ParseBytes(wire.Body)
				require.False(t, parsed.Get("prompt_cache_retention").Exists())
				require.Equal(t, scenario.wantTTL, parsed.Get("prompt_cache_options.ttl").String())
				require.Equal(t, "ultrafast", parsed.Get("service_tier").String())
				require.Equal(t, "low", parsed.Get("reasoning.effort").String())
				require.Equal(t, "explicit", parsed.Get("input.0.content.0.prompt_cache_breakpoint.mode").String())
				require.Equal(t, "rs_native", parsed.Get("input.1.id").String())
				require.Equal(t, "opaque", parsed.Get("input.1.encrypted_content").String())
				require.Equal(t, "call_lookup", parsed.Get("input.2.call_id").String())
				require.Equal(t, "call_lookup", parsed.Get("input.3.call_id").String())
				require.Equal(t, "high", parsed.Get("input.4.reasoning.effort").String())
				require.Equal(t, "user", parsed.Get("input.5.role").String())
				require.Equal(t, body, request.RawRequest.Body)
			})
		}
	}
}
