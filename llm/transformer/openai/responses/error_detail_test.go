package responses

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
)

func TestFailedResponsePreservesParameterAndRequestID(t *testing.T) {
	var response Response
	require.NoError(t, json.Unmarshal([]byte(`{"id":"resp_failed","status":"failed","error":{"code":"invalid_encrypted_content","type":"invalid_request_error","message":"bad response status code 400","param":"input[3].encrypted_content","request_id":"req_upstream"}}`), &response))
	failure := responseErrorFromResponse(&response)
	require.Equal(t, 400, failure.StatusCode)
	require.Equal(t, "input[3].encrypted_content", failure.Detail.Param)
	require.Equal(t, "req_upstream", failure.Detail.RequestID)
}

func TestResponsesStreamPreservesNestedErrorStatus(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		fields string
		want   int
	}{
		{"unauthorized", `"status":401`, 401},
		{"forbidden", `"status":403`, 403},
		{"rate limit", `"status":429`, 429},
		{"overload", `"status":529`, 529},
		{"string status", `"status":"503"`, 503},
		{"status code alias", `"status_code":500`, 500},
		{"status code precedence", `"status_code":429,"status":401`, 429},
		{"invalid code falls back to status", `"status_code":200,"status":403`, 403},
		{"symbolic status", `"status":"RESOURCE_EXHAUSTED"`, 502},
		{"success is not an error status", `"status":200`, 502},
		{"invalid status", `"status":{"code":429}`, 502},
	} {
		for _, eventType := range []string{"error", "response.failed"} {
			t.Run(testCase.name+"/"+eventType, func(t *testing.T) {
				detail := fmt.Sprintf(`{%s,"code":"opaque_provider_error","message":"upstream rejected the request","param":"input","request_id":"req_status"}`, testCase.fields)
				body := fmt.Sprintf(`{"type":"error","status":502,"error":%s}`, detail)
				if eventType == "response.failed" {
					body = fmt.Sprintf(`{"type":"response.failed","response":{"id":"resp_status","status":"failed","error":%s}}`, detail)
				}
				transformer, err := NewOutboundTransformer("https://example.com", "test-key")
				require.NoError(t, err)
				stream, err := transformer.TransformStream(t.Context(), &httpclient.Request{}, streams.SliceStream([]*httpclient.StreamEvent{
					{Type: eventType, Data: []byte(body)},
				}))
				require.NoError(t, err)
				_, err = streams.All(stream)
				var failure *llm.ResponseError
				require.ErrorAs(t, err, &failure)
				require.Equal(t, testCase.want, failure.StatusCode)
				require.Equal(t, "opaque_provider_error", failure.Detail.Code)
				require.Equal(t, "input", failure.Detail.Param)
				require.Equal(t, "req_status", failure.Detail.RequestID)
				require.NoError(t, stream.Close())
			})
		}
	}
}

func TestResponsesWebSocketErrorPreservesRetryHeadersAndStatusAlias(t *testing.T) {
	data := []byte(`{"type":"error","status_code":503,"error":{"type":"service_unavailable_error","code":"server_is_overloaded","message":"temporarily overloaded"},"headers":{"retry-after":12,"X-Request-Id":"req_upstream","X-Retryable":true,"X-Null":null,"invalid":"bad\r\nheader","invalid\tname":"ignored"}}`)
	var event StreamEvent
	require.NoError(t, json.Unmarshal(data, &event))
	failure := responseErrorFromStreamEvent(&event)
	require.Equal(t, 503, failure.StatusCode)
	var raw *httpclient.Error
	require.True(t, errors.As(failure, &raw))
	require.Equal(t, "12", raw.Headers.Get("Retry-After"))
	require.Equal(t, "req_upstream", raw.Headers.Get("X-Request-Id"))
	require.Equal(t, "true", raw.Headers.Get("X-Retryable"))
	require.NotContains(t, raw.Headers, "X-Null")
	require.Empty(t, raw.Headers.Get("invalid"))
	require.Empty(t, raw.Headers.Get("invalid\tname"))
	failureRaw := TopLevelWebSocketError([]*httpclient.StreamEvent{{Type: "error", Data: data}})
	require.ErrorAs(t, failureRaw, &raw)
	require.Equal(t, 503, raw.StatusCode)
	require.Equal(t, "12", raw.Headers.Get("Retry-After"))
}
