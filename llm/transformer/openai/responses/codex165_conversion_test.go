package responses_test

import (
	"testing"
	"net/http"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer"
	"github.com/looplj/axonhub/llm/transformer/anthropic"
	"github.com/looplj/axonhub/llm/transformer/gemini"
	"github.com/looplj/axonhub/llm/transformer/openai"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

func TestCodex165EffortUpdatesAcrossProtocols(t *testing.T) {
	chat, err := openai.NewOutboundTransformer("https://example.com", "synthetic")
	require.NoError(t, err)
	claude, err := anthropic.NewOutboundTransformer("https://example.com", "synthetic")
	require.NoError(t, err)
	google, err := gemini.NewOutboundTransformer("https://example.com", "synthetic")
	require.NoError(t, err)
	native, err := responses.NewOutboundTransformer("https://example.com", "synthetic")
	require.NoError(t, err)
	for _, tc := range []struct { name, model, field string; outbound transformer.Outbound }{
		{"chat", "gpt-6.1-terra", "reasoning_effort", chat},
		{"claude", "claude-sonnet-4-6", "output_config.effort", claude},
		{"gemini", "gemini-3-pro-preview", "generationConfig.thinkingConfig.thinkingLevel", google},
		{"responses", "gpt-6.1-terra", "reasoning.effort", native},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := &httpclient.Request{Body: []byte(`{"model":"gpt-6.1-terra","reasoning":{"effort":"low"},"input":[{"role":"user","content":"hello"},{"type":"configuration_update","reasoning":{"effort":"medium"}},{"type":"configuration_update","reasoning":{"effort":"high"}}]}`)}
			req, err := responses.NewInboundTransformer().TransformRequest(t.Context(), raw)
			require.NoError(t, err)
			req.RawRequest, req.Model = raw, tc.model
			wire, err := tc.outbound.TransformRequest(t.Context(), req)
			require.NoError(t, err)
			want := "high"
			if tc.name == "responses" {
				want = "low"
				require.Equal(t, "high", gjson.GetBytes(wire.Body, "input.2.reasoning.effort").String())
			}
			require.Equal(t, want, gjson.GetBytes(wire.Body, tc.field).String(), string(wire.Body))
			require.Equal(t, "low", req.ReasoningEffort)
			require.Equal(t, "low", gjson.GetBytes(raw.Body, "reasoning.effort").String())
		})
	}
}

func TestWebSearchBlockedDomainsSurviveProtocolConversion(t *testing.T) {
	inbound := anthropic.NewInboundTransformer()
	raw := &httpclient.Request{Headers: http.Header{"Content-Type": []string{"application/json"}}, Body: []byte(`{"model":"claude-sonnet-4-6","max_tokens":100,"messages":[{"role":"user","content":"Search"}],"tools":[{"type":"web_search_20250305","name":"web_search","blocked_domains":["example.com"]}]}`)}
	req, err := inbound.TransformRequest(t.Context(), raw)
	require.NoError(t, err)
	req.Model = "gpt-6.1-terra"
	outbound, err := responses.NewOutboundTransformer("https://api.openai.com", "synthetic")
	require.NoError(t, err)
	wire, err := outbound.TransformRequest(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, "example.com", gjson.GetBytes(wire.Body, "tools.0.filters.blocked_domains.0").String())
	back, err := responses.NewInboundTransformer().TransformRequest(t.Context(), wire)
	require.NoError(t, err)
	require.Equal(t, llm.ToolTypeWebSearch, back.Tools[0].Type)
	require.Equal(t, []string{"example.com"}, back.Tools[0].WebSearch.BlockedDomains)
}
