package gemini

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
)

func TestGeminiMalformedToolFailsWithoutLosingRawUsage(t *testing.T) {
	const raw = `{"responseId":"malformed","candidates":[{"index":0,"content":{"parts":[{"text":"partial"}]},"finishReason":"MALFORMED_FUNCTION_CALL"}],"usageMetadata":{"promptTokenCount":100,"cachedContentTokenCount":80,"candidatesTokenCount":3,"totalTokenCount":103}}`
	adapter := &OutboundTransformer{}
	response, err := adapter.TransformResponse(t.Context(), &httpclient.Response{StatusCode: http.StatusOK, Body: []byte(raw)})
	require.Nil(t, response)
	var apiError *llm.ResponseError
	require.ErrorAs(t, err, &apiError)
	require.Equal(t, http.StatusBadGateway, apiError.StatusCode)
	require.Equal(t, "MALFORMED_FUNCTION_CALL", apiError.Detail.Code)
	events := []*httpclient.StreamEvent{{Data: []byte(raw)}}
	stream, err := adapter.TransformStream(t.Context(), &httpclient.Request{}, streams.SliceStream(events))
	require.NoError(t, err)
	defer stream.Close()
	require.False(t, stream.Next())
	require.ErrorAs(t, stream.Err(), &apiError)
	body, meta, err := AggregateStreamChunks(t.Context(), events)
	require.NoError(t, err)
	require.Contains(t, string(body), "MALFORMED_FUNCTION_CALL")
	require.False(t, meta.Completed)
	require.EqualValues(t, 100, meta.Usage.PromptTokens)
	require.EqualValues(t, 80, meta.Usage.PromptTokensDetails.CachedTokens)
}

func TestGeminiMalformedToolAfterTextDoesNotComplete(t *testing.T) {
	events := []*httpclient.StreamEvent{
		{Data: []byte(`{"candidates":[{"content":{"parts":[{"text":"partial answer"}]}}]}`)},
		{Data: []byte(`{"candidates":[{"finishReason":"MALFORMED_FUNCTION_CALL"}]}`)},
	}
	stream, err := (&OutboundTransformer{}).TransformStream(t.Context(), &httpclient.Request{}, streams.SliceStream(events))
	require.NoError(t, err)
	defer stream.Close()
	require.True(t, stream.Next())
	require.Equal(t, "partial answer", *stream.Current().Choices[0].Delta.Content.Content)
	require.False(t, stream.Next())
	require.ErrorContains(t, stream.Err(), "MALFORMED_FUNCTION_CALL")
}

func TestGeminiMalformedToolKeepsDeliveredCallsPerCandidate(t *testing.T) {
	for _, index := range []string{"0", "1"} {
		t.Run(index, func(t *testing.T) {
			events := []*httpclient.StreamEvent{
				{Data: []byte(`{"candidates":[{"index":0,"content":{"parts":[{"functionCall":{"name":"inspect","args":{"value":7}}}]}}]}`)},
				{Data: []byte(`{"candidates":[{"index":` + index + `,"finishReason":"MALFORMED_FUNCTION_CALL"}]}`)},
			}
			stream, err := (&OutboundTransformer{}).TransformStream(t.Context(), &httpclient.Request{}, streams.SliceStream(events))
			require.NoError(t, err)
			defer stream.Close()
			require.True(t, stream.Next())
			require.Len(t, stream.Current().Choices[0].Delta.ToolCalls, 1)
			for stream.Next() {
				if response := stream.Current(); response != nil && len(response.Choices) > 0 && response.Choices[0].FinishReason != nil {
					require.Equal(t, "tool_calls", *response.Choices[0].FinishReason)
				}
			}
			if index == "0" {
				require.NoError(t, stream.Err())
			} else {
				require.ErrorContains(t, stream.Err(), "MALFORMED_FUNCTION_CALL")
			}
		})
	}
}
