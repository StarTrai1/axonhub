package responses

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm/httpclient"
)

func TestGPT6SolLunaSamplingFollowsEffectiveReasoning(t *testing.T) {
	outbound, err := NewOutboundTransformer("https://example.test/v1", "fake-key")
	require.NoError(t, err)
	for _, model := range []string{"gpt-6-sol", "gpt-6-luna", "gpt-6-astra"} {
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
				wantSample := tc.wantSample && model != "gpt-6-astra"
				for _, field := range []string{"temperature", "top_p", "top_logprobs"} {
					require.Equal(t, wantSample, gjson.GetBytes(wire.Body, field).Exists(), field)
				}
				include := gjson.GetBytes(wire.Body, "include").String()
				require.Contains(t, include, "reasoning.encrypted_content")
				require.Equal(t, wantSample, gjson.GetBytes(wire.Body, `include.#(=="message.output_text.logprobs")`).Exists())
				require.Equal(t, wantSample, outbound.AllowPassThroughBody(t.Context(), request, wire))
				if tc.effort == "none" && model != "gpt-6-astra" {
					require.Equal(t, "none", gjson.GetBytes(wire.Body, "reasoning.effort").String())
				}
				if tc.update != "" {
					require.Equal(t, tc.update, gjson.GetBytes(wire.Body, "input.1.reasoning.effort").String())
				}
				require.Equal(t, string(body), string(request.RawRequest.Body))
			})
		}
	}
}
