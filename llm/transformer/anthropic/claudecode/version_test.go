package claudecode

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFetchLatestClaudeCodeVersion(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "application/vnd.github+json", r.Header.Get("Accept"))
		_, _ = w.Write([]byte(`{"tag_name":"v2.1.232","draft":false,"prerelease":false}`))
	}))
	t.Cleanup(server.Close)

	version, err := fetchLatestClaudeCodeVersion(t.Context(), server.Client(), server.URL)
	require.NoError(t, err)
	require.Equal(t, "2.1.232", version)
}

func TestCompareClaudeCodeVersions(t *testing.T) {
	t.Parallel()

	require.Equal(t, 1, compareClaudeCodeVersions("2.1.232", "2.1.231"))
	require.Equal(t, -1, compareClaudeCodeVersions("2.1.230", "2.1.231"))
	require.Equal(t, 0, compareClaudeCodeVersions("2.1.231", "2.1.231"))
	require.Equal(t, 0, compareClaudeCodeVersions("invalid", "2.1.231"))
}

func TestClaudeCodeVersionFromUserAgent(t *testing.T) {
	t.Parallel()

	require.Equal(t, "2.1.232", claudeCodeVersionFromUserAgent("claude-cli/2.1.232 (external, cli)"))
	require.Equal(t, claudeCodeDefaultVersion, claudeCodeVersionFromUserAgent("axonhub/1.0"))
}
