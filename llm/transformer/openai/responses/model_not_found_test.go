package responses

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
)

func TestCodex165ModelNotFoundRetainsTerminalErrorDetails(t *testing.T) {
	const message = "The selected fixture model is unavailable."
	for _, body := range []string{
		`{"type":"error","error":{"code":"model_not_found","message":"The selected fixture model is unavailable."}}`,
		`{"type":"response.failed","response":{"id":"resp_missing","status":"failed","error":{"code":"model_not_found","message":"The selected fixture model is unavailable."}}}`,
	} {
		err := StreamEventError(&httpclient.StreamEvent{Data: []byte(body)})
		var failure *llm.ResponseError
		require.ErrorAs(t, err, &failure)
		require.Equal(t, 404, failure.StatusCode)
		require.Equal(t, "model_not_found", failure.Detail.Code)
		require.Equal(t, message, failure.Detail.Message)
	}
}
