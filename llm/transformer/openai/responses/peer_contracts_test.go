package responses

import (
	"encoding/json"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
)

func TestResponsesChatToolsPreserveNonStrictDefault(t *testing.T) {
	outbound, err := NewOutboundTransformer("https://api.openai.com", "test-key")
	require.NoError(t, err)
	for _, testCase := range []struct {
		name     string
		format   llm.APIFormat
		strict   *bool
		expected *bool
	}{
		{name: "chat default", format: llm.APIFormatOpenAIChatCompletion, expected: lo.ToPtr(false)},
		{name: "chat explicit strict", format: llm.APIFormatOpenAIChatCompletion, strict: lo.ToPtr(true), expected: lo.ToPtr(true)},
		{name: "chat explicit nonstrict", format: llm.APIFormatOpenAIChatCompletion, strict: lo.ToPtr(false), expected: lo.ToPtr(false)},
		{name: "responses default", format: llm.APIFormatOpenAIResponse},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			request := &llm.Request{
				Model:     "gpt-6-astra",
				APIFormat: testCase.format,
				Messages:  []llm.Message{{Role: "user", Content: llm.MessageContent{Content: lo.ToPtr("hello")}}},
				Tools: []llm.Tool{{Type: "function", Function: llm.Function{
					Name: "lookup", Strict: testCase.strict,
					Parameters: json.RawMessage(`{"type":"object","properties":{"optional":{"type":"string"}}}`),
				}}},
			}
			result, err := outbound.TransformRequest(t.Context(), request)
			require.NoError(t, err)
			strict := gjson.GetBytes(result.Body, "tools.0.strict")
			if testCase.expected == nil {
				require.False(t, strict.Exists())
			} else {
				require.True(t, strict.Exists())
				require.Equal(t, *testCase.expected, strict.Bool())
			}
			require.Equal(t, testCase.strict, request.Tools[0].Function.Strict)
		})
	}
}

func TestResponsesServiceTierAndCacheWriteRoundTrip(t *testing.T) {
	outbound, err := NewOutboundTransformer("https://api.openai.com", "test-key")
	require.NoError(t, err)
	response, err := outbound.TransformResponse(t.Context(), &httpclient.Response{Body: []byte(`{"id":"resp_tier","model":"gpt-6-astra","status":"completed","service_tier":"ultrafast","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":4000,"output_tokens":2,"total_tokens":4002,"input_tokens_details":{"cached_tokens":2000,"cache_write_tokens":1000}}}`)})
	require.NoError(t, err)
	require.Equal(t, "ultrafast", response.ServiceTier)
	require.Equal(t, int64(1000), response.Usage.PromptTokensDetails.WriteCachedTokens)
	converted := convertToResponsesAPIResponse(response)
	require.Equal(t, "ultrafast", lo.FromPtr(converted.ServiceTier))
	require.Equal(t, int64(1000), converted.Usage.InputTokenDetails.CacheWriteTokens)
}

func TestResponsesStreamServiceTierUsesActualTerminalValue(t *testing.T) {
	events := []*httpclient.StreamEvent{
		{Data: []byte(`{"type":"response.created","response":{"id":"resp_tier","model":"gpt-6-astra","service_tier":"default","output":[]}}`)},
		{Data: []byte(`{"type":"response.output_text.delta","item_id":"msg_tier","output_index":0,"content_index":0,"delta":"ok"}`)},
		{Data: []byte(`{"type":"response.completed","response":{"id":"resp_tier","model":"gpt-6-astra","status":"completed","service_tier":"priority","output":[],"usage":{"input_tokens":100,"output_tokens":1,"total_tokens":101,"input_tokens_details":{"cache_write_tokens":25}}}}`)},
	}
	chunks, err := streams.All(newResponsesOutboundStream(streams.SliceStream(events)))
	require.NoError(t, err)
	require.Equal(t, "default", chunks[0].ServiceTier)
	var terminal *llm.Response
	for _, chunk := range chunks {
		if chunk.Usage != nil {
			terminal = chunk
		}
	}
	require.NotNil(t, terminal)
	require.Equal(t, "priority", terminal.ServiceTier)
	require.Equal(t, int64(25), terminal.Usage.PromptTokensDetails.WriteCachedTokens)
	inbound, err := NewInboundTransformer().TransformStream(t.Context(), streams.SliceStream(chunks))
	require.NoError(t, err)
	converted, err := streams.All(inbound)
	require.NoError(t, err)
	var completed Response
	for _, event := range converted {
		var parsed StreamEvent
		require.NoError(t, json.Unmarshal(event.Data, &parsed))
		if parsed.Type == StreamEventTypeResponseCompleted {
			completed = *parsed.Response
		}
	}
	require.Equal(t, "priority", lo.FromPtr(completed.ServiceTier))
	require.Equal(t, int64(25), completed.Usage.InputTokenDetails.CacheWriteTokens)

	aggregator := newStreamAggregator()
	for _, event := range events {
		var parsed StreamEvent
		require.NoError(t, json.Unmarshal(event.Data, &parsed))
		aggregator.processEvent(&parsed)
	}
	require.Equal(t, "priority", lo.FromPtr(aggregator.buildResponse().ServiceTier))
}
