package llm

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCloneProviderExtensions_ClonesRawFields(t *testing.T) {
	source := &ProviderExtensions{
		OpenAIResponses: &OpenAIResponsesProviderExtensions{
			Request: &OpenAIResponsesRequestExtensions{
				RawFields: map[string]json.RawMessage{
					"client_metadata": json.RawMessage(`{"thread_id":"thread-1"}`),
				},
			},
		},
	}

	cloned := CloneProviderExtensions(source)
	require.NotNil(t, cloned)
	require.NotNil(t, cloned.OpenAIResponses)
	require.NotNil(t, cloned.OpenAIResponses.Request)
	require.JSONEq(t, `{"thread_id":"thread-1"}`, string(cloned.OpenAIResponses.Request.RawFields["client_metadata"]))

	source.OpenAIResponses.Request.RawFields["client_metadata"][0] = '['
	require.JSONEq(t, `{"thread_id":"thread-1"}`, string(cloned.OpenAIResponses.Request.RawFields["client_metadata"]))
}
