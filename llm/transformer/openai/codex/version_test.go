package codex

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFetchLatestCodexVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		require.Equal(t, "application/vnd.github+json", request.Header.Get("Accept"))
		require.NotEmpty(t, request.Header.Get("User-Agent"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"rust-v0.148.1","draft":false,"prerelease":false}`))
	}))
	defer server.Close()

	version, err := fetchLatestCodexVersion(t.Context(), server.Client(), server.URL)
	require.NoError(t, err)
	require.Equal(t, "0.148.1", version)
}

func TestFetchLatestCodexVersionRejectsNonStableRelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"rust-v0.149.0-alpha.1","draft":false,"prerelease":true}`))
	}))
	defer server.Close()

	_, err := fetchLatestCodexVersion(t.Context(), server.Client(), server.URL)
	require.Error(t, err)
}

func TestCompareCodexVersions(t *testing.T) {
	require.Equal(t, 1, compareCodexVersions("0.148.0", "0.147.9"))
	require.Equal(t, -1, compareCodexVersions("0.147.9", "0.148.0"))
	require.Equal(t, 0, compareCodexVersions("0.148.0", "0.148.0"))
	require.Equal(t, 0, compareCodexVersions("invalid", "0.148.0"))
}

func TestCodexVersionFromUserAgent(t *testing.T) {
	version, ok := codexVersionFromUserAgent("codex_cli_rs/0.146.0 (Linux 6.8; x86_64) Terminal")
	require.True(t, ok)
	require.Equal(t, "0.146.0", version)

	_, ok = codexVersionFromUserAgent("axonhub/1.0")
	require.False(t, ok)
}
