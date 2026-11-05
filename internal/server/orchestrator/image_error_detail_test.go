package orchestrator

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm/httpclient"
)

func TestImageErrorDetailIsRecorded(t *testing.T) {
	raw := &httpclient.Error{StatusCode: http.StatusBadRequest, Body: []byte(`{"detail":"GIF output is not enabled for this request."}`)}
	require.Equal(t, "GIF output is not enabled for this request.", ExtractErrorMessage(raw))
	raw.Body = []byte(`{"error":{"message":"original error"},"detail":"other"}`)
	require.Equal(t, "original error", ExtractErrorMessage(raw))
	raw.Body = []byte(`{"detail":[{"input":"do not expose the request"}]}`)
	require.NotContains(t, ExtractErrorMessage(raw), "do not expose")
}
