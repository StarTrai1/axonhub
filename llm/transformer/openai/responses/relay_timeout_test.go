package responses

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
)

func TestResponsesRelayTimeoutKeepsBoundedRetryStatus(t *testing.T) {
	const message = "OpenAI Responses HTTP error [408]: The operation was timeout. [trace_id=synthetic]"
	for _, test := range []struct {
		name, code, message string
		status, want int
	}{
		{"wrapped timeout", "", message, 400, 408},
		{"bad request wrapper", "bad_request", message, 400, 408},
		{"real invalid input", "invalid_encrypted_content", message, 400, 400},
		{"auth unchanged", "", message, 401, 401},
		{"context error unchanged", "context_length_exceeded", message, 400, 400},
		{"unrelated timeout text", "", "input mentions timeout", 400, 400},
		{"untrusted suffix", "", message + " and other text", 400, 400},
	} {
		t.Run(test.name, func(t *testing.T) {
			body, err := json.Marshal(map[string]any{"error": llm.ErrorDetail{Type: "invalid_request_error", Code: test.code, Message: test.message}})
			require.NoError(t, err)
			raw := &httpclient.Error{StatusCode: test.status, Body: body, Headers: http.Header{"Retry-After": {"2"}}}
			out := &OutboundTransformer{}
			result := out.TransformError(t.Context(), raw)
			require.Equal(t, test.want, result.StatusCode)
			require.Equal(t, test.message, result.Detail.Message)
			require.Equal(t, test.status, raw.StatusCode, "preserve source evidence")
			if test.want == 408 {
				var cause *httpclient.Error
				require.True(t, errors.As(result, &cause))
				require.Equal(t, test.want, cause.StatusCode)
				require.Equal(t, "2", cause.Headers.Get("Retry-After"))
				require.Equal(t, body, cause.Body)
			}
		})
	}
	streamError := responseErrorFromStreamEvent(&StreamEvent{Error: &Error{Type: "invalid_request_error", Message: message}})
	require.Equal(t, http.StatusRequestTimeout, streamError.StatusCode)
	streamError = responseErrorFromStreamEvent(&StreamEvent{Status: 403, Error: &Error{Type: "permission_error", Message: message}})
	require.Equal(t, http.StatusForbidden, streamError.StatusCode)
}
