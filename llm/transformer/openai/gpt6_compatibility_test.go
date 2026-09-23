package openai

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm/httpclient"
)

func TestGPT6ChatSamplingAndPassThrough(t *testing.T) {
	outbound, err := NewOutboundTransformer("https://example.test/v1", "fake-key")
	require.NoError(t, err)
	for _, model := range []string{"gpt-6-sol", "gpt-6-luna", "gpt-6-astra"} {
		for _, effort := range []string{"", "none", "minimal", "high"} {
			t.Run(model+"/"+effort, func(t *testing.T) {
				body := []byte(fmt.Sprintf(`{"model":%q,"reasoning_effort":%q,"temperature":0.7,"top_p":0.8,"top_logprobs":2,"logprobs":true,"messages":[{"role":"user","content":"hello"}]}`, model, effort))
				request, err := NewInboundTransformer().TransformRequest(t.Context(), &httpclient.Request{Body: body})
				require.NoError(t, err)
				wire, err := outbound.TransformRequest(t.Context(), request)
				require.NoError(t, err)
				wantSample := effort == "none" && model != "gpt-6-astra"
				for _, field := range []string{"temperature", "top_p", "top_logprobs", "logprobs"} {
					require.Equal(t, wantSample, gjson.GetBytes(wire.Body, field).Exists(), field)
				}
				require.Equal(t, wantSample, outbound.(*OutboundTransformer).AllowPassThroughBody(t.Context(), request, wire))
			})
		}
	}
}
