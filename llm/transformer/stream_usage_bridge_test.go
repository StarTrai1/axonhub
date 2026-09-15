package transformer_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
	"github.com/looplj/axonhub/llm/transformer"
	"github.com/looplj/axonhub/llm/transformer/anthropic"
	"github.com/looplj/axonhub/llm/transformer/deepseek"
	"github.com/looplj/axonhub/llm/transformer/gemini"
	"github.com/looplj/axonhub/llm/transformer/ollama"
	"github.com/looplj/axonhub/llm/transformer/openai"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

func TestCrossProtocolChatStreamUsage(t *testing.T) {
	for _, tc := range []struct {
		name      string
		inbound   transformer.Inbound
		path      string
		body      string
		usagePath string
	}{
		{name: "anthropic", inbound: anthropic.NewInboundTransformer(), path: "/v1/messages",
			body: `{"model":"test","max_tokens":64,"stream":true,"messages":[{"role":"user","content":"hello"}]}`, usagePath: "usage.output_tokens"},
		{name: "gemini", inbound: gemini.NewInboundTransformer(), path: "/v1beta/models/test:streamGenerateContent",
			body: `{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`, usagePath: "usageMetadata.candidatesTokenCount"},
		{name: "responses", inbound: responses.NewInboundTransformer(), path: "/v1/responses",
			body: `{"model":"test","input":"hello","stream":true}`, usagePath: "response.usage.output_tokens"},
	} {
		for _, provider := range []struct {
			name    string
			factory func(string, string) (transformer.Outbound, error)
		}{
			{name: "openai", factory: openai.NewOutboundTransformer},
			{name: "deepseek", factory: deepseek.NewOutboundTransformer},
		} {
			t.Run(tc.name+"/"+provider.name, func(t *testing.T) {
				request, err := tc.inbound.TransformRequest(t.Context(), &httpclient.Request{Path: tc.path, Body: []byte(tc.body)})
				require.NoError(t, err)
				require.Nil(t, request.StreamOptions)
				before, err := json.Marshal(request)
				require.NoError(t, err)
				out, err := provider.factory("https://example.com", "test-only")
				require.NoError(t, err)
				wire, err := out.TransformRequest(t.Context(), request)
				require.NoError(t, err)
				require.True(t, gjson.GetBytes(wire.Body, "stream").Bool())
				require.True(t, gjson.GetBytes(wire.Body, "stream_options.include_usage").Bool())
				after, err := json.Marshal(request)
				require.NoError(t, err)
				require.JSONEq(t, string(before), string(after), "a Chat attempt must not modify a later provider attempt")

				source := streams.NoNil(streams.SliceStream([]*httpclient.StreamEvent{
					{Data: []byte(`{"id":"chat_usage","model":"test","choices":[{"index":0,"delta":{"role":"assistant","content":"Hello"}}]}`)},
					{Data: []byte(`{"id":"chat_usage","model":"test","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`)},
					{Data: []byte(`{"id":"chat_usage","model":"test","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":3,"total_tokens":13}}`)},
					{Data: []byte(`[DONE]`)},
				}))
				decoded, err := out.TransformStream(t.Context(), wire, source)
				require.NoError(t, err)
				client, err := tc.inbound.TransformStream(t.Context(), streamWithTestMetadata(request, decoded))
				require.NoError(t, err)
				defer client.Close()
				usageSeen := false
				for client.Next() {
					if gjson.GetBytes(client.Current().Data, tc.usagePath).Int() == 3 {
						usageSeen = true
					}
				}
				require.NoError(t, client.Err())
				require.True(t, usageSeen, "final upstream usage must reach the original protocol")

				for _, nativeFactory := range []func() (transformer.Outbound, error){
					func() (transformer.Outbound, error) { return responses.NewOutboundTransformer("https://example.com", "test-only") },
					func() (transformer.Outbound, error) {
						return ollama.NewOutboundTransformerWithConfig(&ollama.Config{BaseURL: "https://example.com"})
					},
				} {
					native, err := nativeFactory()
					require.NoError(t, err)
					nativeWire, err := native.TransformRequest(t.Context(), request)
					require.NoError(t, err)
					require.False(t, gjson.GetBytes(nativeWire.Body, "stream_options").Exists())
				}
			})
		}
	}
}

func TestChatStreamUsagePreservesExplicitOptions(t *testing.T) {
	for _, tc := range []struct {
		name    string
		format  llm.APIFormat
		stream  bool
		options *llm.StreamOptions
	}{
		{name: "native default", format: llm.APIFormatOpenAIChatCompletion, stream: true},
		{name: "native disabled", format: llm.APIFormatOpenAIChatCompletion, stream: true, options: &llm.StreamOptions{}},
		{name: "native enabled", format: llm.APIFormatOpenAIChatCompletion, stream: true, options: &llm.StreamOptions{IncludeUsage: true}},
		{name: "explicit converted option", format: llm.APIFormatOpenAIResponse, stream: true, options: &llm.StreamOptions{}},
		{name: "nonstream", format: llm.APIFormatOpenAIResponse},
		{name: "unspecified format", stream: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := &llm.Request{APIFormat: tc.format, Stream: &tc.stream, StreamOptions: tc.options}
			converted := openai.RequestFromLLM(t.Context(), request, openai.ReasoningFieldNone)
			if tc.options == nil {
				require.Nil(t, converted.StreamOptions)
			} else {
				require.NotNil(t, converted.StreamOptions)
				require.Equal(t, tc.options.IncludeUsage, converted.StreamOptions.IncludeUsage)
				converted.StreamOptions.IncludeUsage = !converted.StreamOptions.IncludeUsage
				require.NotEqual(t, request.StreamOptions.IncludeUsage, converted.StreamOptions.IncludeUsage)
			}
		})
	}
}
