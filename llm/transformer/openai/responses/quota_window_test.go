package responses

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
)

func TestQuotaWindowSurvivesHTTPAndStreamErrors(t *testing.T) {
	for _, window := range []string{"10080", "300", "null", `"unknown"`} {
		t.Run(window, func(t *testing.T) {
			body := []byte(`{"error":{"type":"usage_limit_reached","code":"usage_limit_reached","message":"quota reached","limit_window_minutes":` + window + `}}`)
			outbound := &OutboundTransformer{}
			httpError := outbound.TransformError(t.Context(), &httpclient.Error{StatusCode: http.StatusTooManyRequests, Body: body})
			var event StreamEvent
			require.NoError(t, json.Unmarshal(body, &event))
			event.Type = StreamEventTypeError
			event.Status = http.StatusTooManyRequests
			streamError := responseErrorFromStreamEvent(&event)
			for _, upstreamError := range []*llm.ResponseError{httpError, streamError} {
				got := NewInboundTransformer().TransformError(t.Context(), upstreamError)
				require.Equal(t, http.StatusTooManyRequests, got.StatusCode)
				require.Equal(t, window, gjson.GetBytes(got.Body, "error.limit_window_minutes").Raw)
				for _, created := range []bool{false, true} {
					stream := &responsesInboundStream{hasResponseCreated: created}
					require.NoError(t, stream.emitStreamErrorEvent(upstreamError))
					require.Len(t, stream.eventQueue, 1)
					path := "error.limit_window_minutes"
					if created {
						path = "response." + path
					}
					require.Equal(t, window, gjson.GetBytes(stream.eventQueue[0].Data, path).Raw)
				}
			}
		})
	}
}
