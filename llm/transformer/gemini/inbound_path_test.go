package gemini

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm/httpclient"
)

func TestExtractRequestParamsUsesTerminalAction(t *testing.T) {
	for _, test := range []struct {
		path string
		model string
		stream bool
	}{
		{"/v1beta/models/gemini-4-neon:generateContent", "gemini-4-neon", false},
		{"/v1beta/models/gemini-4-neon:custom:generateContent", "gemini-4-neon:custom", false},
		{"/v1beta/models/relay:gemini-4-neon:streamGenerateContent", "relay:gemini-4-neon", true},
	} {
		t.Run(test.path, func(t *testing.T) {
			model, stream, err := extractRequestParams(&httpclient.Request{Path: test.path})
			require.NoError(t, err)
			require.Equal(t, test.model, model)
			require.Equal(t, test.stream, stream)
		})
	}
	for _, path := range []string{"", "/v1beta/models/:generateContent", "/v1beta/models/gemini:", "/v1beta/models/gemini", "/v1beta/models/gemini:generateContent:unknown"} {
		_, _, err := extractRequestParams(&httpclient.Request{Path: path})
		require.ErrorIs(t, err, ErrInvalidRequestURL, path)
	}
}
