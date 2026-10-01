package openai

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer"
)

func TestGPT6ChatSamplingAndPassThrough(t *testing.T) {
	outbound, err := NewOutboundTransformer("https://example.test/v1", "fake-key")
	require.NoError(t, err)
	for _, model := range []string{"gpt-6-sol", "gpt-6-luna", "gpt-6-astra", "gpt-6.1-sol"} {
		for _, effort := range []string{"", "none", "minimal", "high"} {
			t.Run(model+"/"+effort, func(t *testing.T) {
				body := []byte(fmt.Sprintf(`{"model":%q,"reasoning_effort":%q,"temperature":0.7,"top_p":0.8,"top_logprobs":2,"logprobs":true,"messages":[{"role":"user","content":"hello"}]}`, model, effort))
				request, err := NewInboundTransformer().TransformRequest(t.Context(), &httpclient.Request{Body: body, Headers: http.Header{"Content-Type": {"application/json"}}})
				require.NoError(t, err)
				wire, err := outbound.TransformRequest(t.Context(), request)
				require.NoError(t, err)
				wantSample := effort == "none" && model != "gpt-6-astra" && model != "gpt-6.1-sol"
				for _, field := range []string{"temperature", "top_p", "top_logprobs", "logprobs"} {
					require.Equal(t, wantSample, gjson.GetBytes(wire.Body, field).Exists(), field)
				}
				require.Equal(t, wantSample, outbound.(*OutboundTransformer).AllowPassThroughBody(t.Context(), request, wire))
			})
		}
	}
}

func TestSol61ChatToolCallsRequireResponses(t *testing.T) {
	outbound, err := NewOutboundTransformer("https://example.test/v1", "fake-key")
	require.NoError(t, err)
	for _, body := range []string{
		`{"model":"gpt-6.1-sol","messages":[{"role":"user","content":"hello"}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}]}`,
		`{"model":"gpt-6.1-sol","reasoning_effort":"none","messages":[{"role":"user","content":"hello"}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}]}`,
		`{"model":"gpt-6.1-sol","messages":[{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_1","content":"found"}]}`,
	} {
		request, err := NewInboundTransformer().TransformRequest(t.Context(), &httpclient.Request{Body: []byte(body), Headers: http.Header{"Content-Type": {"application/json"}}})
		require.NoError(t, err)
		wire, err := outbound.TransformRequest(t.Context(), request)
		require.ErrorIs(t, err, transformer.ErrInvalidRequest)
		require.ErrorContains(t, err, "tool calls require a Responses endpoint")
		require.Nil(t, wire)
	}
}

func TestGPT6ChatCompletionTokenLimit(t *testing.T) {
	outbound, err := NewOutboundTransformer("https://example.test/v1", "fake-key")
	require.NoError(t, err)
	for _, model := range []string{"gpt-6-astra", "gpt-6-sol", "gpt-6-luna", "gpt-6.1-sol"} {
		for _, limits := range []struct {
			fields string
			want   int64
		}{
			{`"max_tokens":123`, 123},
			{`"max_tokens":123,"max_completion_tokens":456`, 456},
			{`"max_completion_tokens":456`, 456},
		} {
			t.Run(model+"/"+limits.fields, func(t *testing.T) {
				body := []byte(fmt.Sprintf(`{"model":%q,%s,"messages":[{"role":"user","content":"hello"}]}`, model, limits.fields))
				request, err := NewInboundTransformer().TransformRequest(t.Context(), &httpclient.Request{Body: body, Headers: http.Header{"Content-Type": {"application/json"}}})
				require.NoError(t, err)
				wire, err := outbound.TransformRequest(t.Context(), request)
				require.NoError(t, err)
				require.False(t, gjson.GetBytes(wire.Body, "max_tokens").Exists())
				require.Equal(t, limits.want, gjson.GetBytes(wire.Body, "max_completion_tokens").Int())
				require.Equal(t, !gjson.GetBytes(body, "max_tokens").Exists(), outbound.(*OutboundTransformer).AllowPassThroughBody(t.Context(), request, wire))
				require.Equal(t, body, request.RawRequest.Body)
			})
		}
	}
}
