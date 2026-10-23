package gemini

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
)

func TestPromptBlockIsNotEmptyGeneration(t *testing.T) {
	outbound, err := NewOutboundTransformer("https://synthetic.invalid", "synthetic")
	require.NoError(t, err)
	body := []byte(`{"promptFeedback":{"blockReason":"SAFETY","blockReasonMessage":"provider detail"},"usageMetadata":{"promptTokenCount":20}}`)
	_, err = outbound.TransformResponse(t.Context(), &httpclient.Response{StatusCode: 200, Body: body})
	var providerError *llm.ResponseError
	require.ErrorAs(t, err, &providerError)
	require.Equal(t, 400, providerError.StatusCode)
	require.Equal(t, "SAFETY", providerError.Detail.Code)
	require.Contains(t, providerError.Detail.Message, "provider detail")
	stream, err := outbound.TransformStream(t.Context(), &httpclient.Request{}, streams.SliceStream([]*httpclient.StreamEvent{{Data: body}}))
	require.NoError(t, err)
	defer stream.Close()
	require.False(t, stream.Next())
	require.ErrorAs(t, stream.Err(), &providerError)
	require.Equal(t, "SAFETY", providerError.Detail.Code)
	aggregated, metadata, err := AggregateStreamChunks(t.Context(), []*httpclient.StreamEvent{{Data: body}})
	require.ErrorAs(t, err, &providerError)
	require.Equal(t, 400, providerError.StatusCode)
	require.Equal(t, "SAFETY", providerError.Detail.Code)
	require.Nil(t, aggregated)
	require.False(t, metadata.Completed)
	for _, unblocked := range []string{`{}`, `{"promptFeedback":{}}`, `{"promptFeedback":{"blockReason":"BLOCK_REASON_UNSPECIFIED"}}`} {
		response, err := outbound.TransformResponse(t.Context(), &httpclient.Response{StatusCode: 200, Body: []byte(unblocked)})
		require.NoError(t, err)
		require.NotNil(t, response)
	}
}
