package gemini

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
)

func TestGeminiSuccessfulHTTPWithErrorEnvelopeFails(t *testing.T) {
	outbound := &OutboundTransformer{}
	for _, status := range []int{http.StatusBadRequest, http.StatusForbidden, http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			body := []byte(fmt.Sprintf(`{"error":{"code":%d,"message":"upstream rejected this request","status":"TEST_STATUS"}}`, status))
			response, err := outbound.TransformResponse(t.Context(), &httpclient.Response{StatusCode: http.StatusOK, Body: body})
			require.Nil(t, response)
			var responseErr *llm.ResponseError
			require.ErrorAs(t, err, &responseErr)
			require.Equal(t, status, responseErr.StatusCode)
			require.Equal(t, fmt.Sprint(status), responseErr.Detail.Code)
			require.Equal(t, "TEST_STATUS", responseErr.Detail.Type)
			require.Equal(t, "upstream rejected this request", responseErr.Detail.Message)
		})
	}
}

func TestGeminiStreamAndAggregationDoNotCompleteAfterErrorEnvelope(t *testing.T) {
	events := []*httpclient.StreamEvent{
		{Data: []byte(`{"candidates":[{"content":{"parts":[{"text":"partial answer"}]}}]}`)},
		{Data: []byte(`{"error":{"code":429,"message":"rate limit","status":"RESOURCE_EXHAUSTED"}}`)},
	}
	outbound := &OutboundTransformer{}
	stream, err := outbound.TransformStream(t.Context(), &httpclient.Request{}, streams.SliceStream(events))
	require.NoError(t, err)
	defer stream.Close()
	require.True(t, stream.Next())
	require.Equal(t, "partial answer", *stream.Current().Choices[0].Delta.Content.Content)
	require.False(t, stream.Next())
	var responseErr *llm.ResponseError
	require.ErrorAs(t, stream.Err(), &responseErr)
	require.Equal(t, http.StatusTooManyRequests, responseErr.StatusCode)
	body, metadata, err := AggregateStreamChunks(t.Context(), events)
	require.ErrorAs(t, err, &responseErr)
	require.Equal(t, http.StatusTooManyRequests, responseErr.StatusCode)
	require.Nil(t, body)
	require.False(t, metadata.Completed)
}

func TestGeminiModelFinishReasonsRemainProtocolResults(t *testing.T) {
	outbound := &OutboundTransformer{}
	for _, reason := range []string{"STOP", "MAX_TOKENS", "SAFETY", "MALFORMED_FUNCTION_CALL", "UNEXPECTED_TOOL_CALL"} {
		t.Run(reason, func(t *testing.T) {
			body := []byte(fmt.Sprintf(`{"candidates":[{"content":{"parts":[{"text":"model result"}]},"finishReason":%q}]}`, reason))
			response, err := outbound.TransformResponse(t.Context(), &httpclient.Response{StatusCode: http.StatusOK, Body: body})
			require.NoError(t, err)
			require.NotNil(t, response)
		})
	}
	response, err := outbound.TransformResponse(t.Context(), &httpclient.Response{StatusCode: http.StatusOK, Body: []byte(`{"promptFeedback":{"blockReason":"SAFETY"}}`)})
	require.NoError(t, err)
	require.NotNil(t, response)
}
