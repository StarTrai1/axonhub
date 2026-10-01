package codex

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm/oauth"
)

func TestModelCatalogPreservesAvailableFastAliases(t *testing.T) {
	models, err := ParseModelCatalog([]byte(`{"models":[
		{"slug":"gpt-6.1-sol","visibility":"list","priority":2},
		{"slug":"gpt-6-luna","visibility":"hide","priority":0},
		{"slug":"future-model","visibility":"list","priority":1},
		{"slug":"gpt-6-astra-fast","visibility":"list","priority":4},
		{"slug":"gpt-6-astra","visibility":"list","priority":3}
	]}`))
	require.NoError(t, err)
	require.Equal(t, []string{"future-model", "gpt-6.1-sol", "gpt-6-astra", "gpt-6-astra-fast", "gpt-6.1-sol-fast"}, models)
	require.NotContains(t, models, "gpt-6-luna-fast")
}

func TestModelsRequestUsesCurrentClientIdentity(t *testing.T) {
	request, err := ModelsRequest(t.Context(), staticTokenGetter{creds: &oauth.OAuthCredentials{AccessToken: "synthetic-token"}}, codexBaseURL)
	require.NoError(t, err)
	parsed, err := url.Parse(request.URL)
	require.NoError(t, err)
	require.Equal(t, "https://chatgpt.com/backend-api/codex/models", parsed.Scheme+"://"+parsed.Host+parsed.Path)
	version := parsed.Query().Get("client_version")
	require.GreaterOrEqual(t, compareCodexVersions(version, codexDefaultVersion), 0)
	require.Equal(t, version, request.Headers.Get("Version"))
	require.Equal(t, "codex_cli_rs/"+version, request.Headers.Get("User-Agent"))
}
