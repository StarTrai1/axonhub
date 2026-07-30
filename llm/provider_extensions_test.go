package llm

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCloneProviderExtensions_ClonesClientMetadata(t *testing.T) {
	source := &ProviderExtensions{
		OpenAIResponses: &OpenAIResponsesProviderExtensions{
			Request: &OpenAIResponsesRequestExtensions{
				ClientMetadata: json.RawMessage(`{"thread_id":"thread-1"}`),
			},
		},
	}

	cloned := CloneProviderExtensions(source)
	require.NotNil(t, cloned)
	require.NotNil(t, cloned.OpenAIResponses)
	require.NotNil(t, cloned.OpenAIResponses.Request)
	require.JSONEq(t, `{"thread_id":"thread-1"}`, string(cloned.OpenAIResponses.Request.ClientMetadata))

	source.OpenAIResponses.Request.ClientMetadata[0] = '['
	require.JSONEq(t, `{"thread_id":"thread-1"}`, string(cloned.OpenAIResponses.Request.ClientMetadata))
}
