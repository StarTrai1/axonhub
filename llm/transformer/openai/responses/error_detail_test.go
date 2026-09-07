package responses

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm/httpclient"
)

func TestFailedResponsePreservesParameterAndRequestID(t *testing.T) {
	var response Response
	require.NoError(t, json.Unmarshal([]byte(`{"id":"resp_failed","status":"failed","error":{"code":"invalid_encrypted_content","type":"invalid_request_error","message":"bad response status code 400","param":"input[3].encrypted_content","request_id":"req_upstream"}}`), &response))
	failure := responseErrorFromResponse(&response)
	require.Equal(t, 400, failure.StatusCode)
	require.Equal(t, "input[3].encrypted_content", failure.Detail.Param)
	require.Equal(t, "req_upstream", failure.Detail.RequestID)
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
