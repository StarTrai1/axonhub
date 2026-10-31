package openai

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
)

func TestChatFinishErrorClassification(t *testing.T) {
	adapter, err := NewOutboundTransformer("https://example.com/v1", "fixture")
	require.NoError(t, err)
	for _, reason := range []string{"error", "stop", "length", "tool_calls", "content_filter", "", "vendor_specific"} {
		t.Run(reason, func(t *testing.T) {
			body := fmt.Appendf(nil, `{"choices":[{"index":0,"finish_reason":"stop"},{"index":1,"finish_reason":%q}]}`, reason)
			response, err := adapter.TransformResponse(t.Context(), &httpclient.Response{StatusCode: http.StatusOK, Body: body})
			if reason == "error" {
				var upstreamError *llm.ResponseError
				require.ErrorAs(t, err, &upstreamError)
				require.Equal(t, http.StatusBadGateway, upstreamError.StatusCode)
				require.Equal(t, "upstream_generation_failed", upstreamError.Detail.Code)
				require.Nil(t, response)
			} else {
				require.NoError(t, err)
				require.Equal(t, reason, *response.Choices[1].FinishReason)
			}
		})
	}
}
