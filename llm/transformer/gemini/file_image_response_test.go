package gemini

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
)

func TestGeminiFileImageSurvivesChatAndStreamConversion(t *testing.T) {
	const body = `{"responseId":"image-result","candidates":[{"index":0,"content":{"parts":[{"fileData":{"mimeType":"image/png","fileUri":"https://images.example/result.png"}}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":20,"totalTokenCount":120}}`
	adapter := &OutboundTransformer{}
	response, err := adapter.TransformResponse(t.Context(), &httpclient.Response{StatusCode: http.StatusOK, Body: []byte(body)})
	require.NoError(t, err)
	require.Len(t, response.Choices, 1)
	require.Len(t, response.Choices[0].Message.Content.MultipleContent, 1)
	require.Equal(t, "https://images.example/result.png", response.Choices[0].Message.Content.MultipleContent[0].ImageURL.URL)
	require.EqualValues(t, 120, response.Usage.TotalTokens)
	stream, err := adapter.TransformStream(t.Context(), &httpclient.Request{}, streams.SliceStream([]*httpclient.StreamEvent{{Data: []byte(body)}}))
	require.NoError(t, err)
	defer stream.Close()
	require.True(t, stream.Next())
	require.Equal(t, response.Choices[0].Message.Content, stream.Current().Choices[0].Delta.Content)
}

func TestGeminiImageEndpointIncludesOnlyNonemptyImageParts(t *testing.T) {
	const body = `{"candidates":[null,{"content":{"parts":[null,{"inlineData":{"mimeType":"image/png","data":"inline"},"fileData":{"mimeType":"image/png","fileUri":"https://images.example/duplicate.png"}},{"fileData":{"mimeType":" Image/PNG ","fileUri":"https://images.example/result.png"}},{"fileData":{"mimeType":"image/png","fileUri":" "}},{"inlineData":{"mimeType":"image/png","data":""}},{"fileData":{"mimeType":"application/pdf","fileUri":"https://files.example/test.pdf"}}]}}]}`
	response, err := transformImageGenerationResponse(&httpclient.Response{StatusCode: http.StatusOK, Body: []byte(body)})
	require.NoError(t, err)
	require.Equal(t, []llm.ImageData{{B64JSON: "inline"}, {URL: "https://images.example/result.png"}}, response.Image.Data)
}
