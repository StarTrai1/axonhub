package antigravity

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/oauth"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

func TestResponsesSummaryVisibilityThroughAntigravity(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		fields string
		want   bool
	}{
		{"unspecified keeps default", "", true},
		{"explicit auto", `,"summary":"auto"`, true},
		{"explicit concise", `,"summary":"concise"`, true},
		{"explicit none", `,"summary":"none"`, false},
		{"explicit null", `,"summary":null`, false},
		{"legacy detailed", `,"generate_summary":"detailed"`, true},
		{"legacy none", `,"generate_summary":"none"`, false},
		{"canonical null wins", `,"summary":null,"generate_summary":"detailed"`, false},
		{"canonical auto wins", `,"summary":"auto","generate_summary":"none"`, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			body := []byte(fmt.Sprintf(`{"model":"gemini-3-flash","reasoning":{"effort":"high"%s},"input":"hello"}`, scenario.fields))
			raw := &httpclient.Request{Body: body, Headers: make(http.Header)}
			request, err := responses.NewInboundTransformer().TransformRequest(t.Context(), raw)
			require.NoError(t, err)
			request.RawRequest = raw
			outbound, err := NewTransformer(Config{BaseURL: "https://example.invalid", Project: "test-project"})
			require.NoError(t, err)
			outbound.tokenProvider = NewTokenProvider(oauth.TokenProviderParams{Credentials: &oauth.OAuthCredentials{
				AccessToken: "synthetic-token", ExpiresAt: time.Now().Add(time.Hour),
			}})
			result, err := outbound.TransformRequest(t.Context(), request)
			require.NoError(t, err)
			visibility := gjson.GetBytes(result.Body, "request.generationConfig.thinkingConfig.includeThoughts")
			require.True(t, visibility.Exists(), "explicit false must survive wire encoding")
			require.Equal(t, scenario.want, visibility.Bool())
			require.Equal(t, "high", gjson.GetBytes(result.Body, "request.generationConfig.thinkingConfig.thinkingLevel").String())
			require.Equal(t, body, raw.Body)
		})
	}
}
