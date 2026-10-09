package api

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
)

// Hosted CLI checks consume the gateway's actual encoder output, rather than a
// hand-written error fixture that could drift from the wire contract.
func TestCodex162RetryFixture(t *testing.T) {
	_, encode := newResponsesStreamAdapter(context.Background(), &errorAfterStream{}, nil)
	event, err := encode(t.Context(), &llm.ResponseError{
		StatusCode: 503,
		Detail: llm.ErrorDetail{Code: "server_is_overloaded", Type: "server_error", Message: "synthetic overload"},
		Cause: &httpclient.Error{StatusCode: 503, Headers: http.Header{"Retry-After": {"3"}, "Authorization": {"must-not-escape"}}},
	})
	require.NoError(t, err)
	require.Equal(t, "response.failed", event.Type)
	require.Equal(t, "3", gjson.GetBytes(event.Data, "response.error.headers.retry-after").String())
	require.NotContains(t, string(event.Data), "must-not-escape")
	if path := os.Getenv("AXONHUB_CODEX_RETRY_FIXTURE"); path != "" {
		require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf("event: %s\ndata: %s\n\n", event.Type, event.Data)), 0o600))
	}
}
