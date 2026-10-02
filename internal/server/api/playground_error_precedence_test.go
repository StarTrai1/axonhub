package api

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/transformer/antigravity"
)

func TestPlaygroundError_PrefersSanitizedProviderDetail(t *testing.T) {
	adapter, err := antigravity.NewTransformer(antigravity.Config{APIKey: "synthetic", Project: "fixture"})
	require.NoError(t, err)
	raw := &httpclient.Error{
		StatusCode: http.StatusForbidden,
		Body:       []byte(`{"error":{"code":403,"status":"PERMISSION_DENIED","message":"Access denied for projects/private-project"}}`),
	}
	converted := adapter.TransformError(t.Context(), raw)
	result := (&PlaygroundHandlers{}).HandleError(pipeline.WrapUpstreamError(converted))
	require.Equal(t, http.StatusForbidden, result.Status)
	require.Equal(t, http.StatusForbidden, result.Error.Code)
	require.Equal(t, "Access denied for projects/***", result.Error.Message)
	var original *httpclient.Error
	require.ErrorAs(t, converted, &original)
	require.Same(t, raw, original)
	require.Contains(t, string(original.Body), "private-project")
}
