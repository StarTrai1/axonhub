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
	require.Equal(t, []string{"future-model", "gpt-6.1-sol", "gpt-6-astra", "gpt-6-astra-fast", "gpt-6.1-sol-fast", "gpt-image-2"}, models)
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

func TestCodexModelsIncludeImageModel(t *testing.T) {
	for _, tt := range []struct {
		name string
		body string
		want []string
	}{
		{"missing", `{"models":[{"slug":"second","visibility":"list","priority":2},{"slug":"first","visibility":"list","priority":1}]}`, []string{"first", "second", "gpt-image-2"}},
		{"existing", `{"models":[{"slug":"first","visibility":"list","priority":1},{"slug":"gpt-image-2","visibility":"list","priority":2},{"slug":"last","visibility":"list","priority":3}]}`, []string{"first", "gpt-image-2", "last"}},
		{"duplicate image", `{"models":[{"slug":"gpt-image-2","visibility":"list","priority":1},{"slug":"first","visibility":"list","priority":2},{"slug":"gpt-image-2","visibility":"list","priority":3}]}`, []string{"gpt-image-2", "first"}},
		{"stable order", `{"models":[{"slug":"second","visibility":"list","priority":1},{"slug":"first","visibility":"list","priority":1},{"slug":"second","visibility":"list","priority":2}]}`, []string{"second", "first", "second", "gpt-image-2"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			// Given a nonempty visible upstream catalog.
			body := []byte(tt.body)
			// When the catalog is parsed.
			models, err := ParseModelCatalog(body)
			// Then one image model is included without reordering other models.
			require.NoError(t, err)
			require.Equal(t, tt.want, models)
		})
	}

	t.Run("fallback", func(t *testing.T) {
		// Given the static fallback catalog, when it is requested.
		models := DefaultModels()
		// Then its original order is retained with the image model appended once.
		require.Contains(t, models, "gpt-reserve")
		require.Contains(t, models, "gpt-6.1-sol")
		require.Contains(t, models, "gpt-6.1-sol-fast")
		require.Equal(t, "gpt-image-2", models[len(models)-1])
		count := 0
		for _, model := range models {
			if model == "gpt-image-2" { count++ }
		}
		require.Equal(t, 1, count)
	})

	for _, body := range []string{
		`{"models":[]}`,
		`{"models":[{"slug":"gpt-image-2","visibility":"hide"}]}`,
		`{"models":[{"slug":" ","visibility":"list"}]}`,
	} {
		t.Run("empty visible catalog "+body, func(t *testing.T) {
			// Given a catalog with no valid visible models, when it is parsed.
			models, err := ParseModelCatalog([]byte(body))
			// Then the original validation still rejects it before supplementation.
			require.EqualError(t, err, "Codex catalog has no visible models")
			require.Nil(t, models)
		})
	}
}
