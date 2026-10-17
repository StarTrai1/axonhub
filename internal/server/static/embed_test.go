package static

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	ginstatic "github.com/gin-contrib/static"

	"github.com/looplj/axonhub/internal/objects"
)

func useTestStaticFS(t *testing.T) {
	t.Helper()

	tempDir := t.TempDir()
	require.NoError(t, os.WriteFile(tempDir+"/index.html", []byte("<html><body>test</body></html>"), 0o644))

	originalStaticFS := staticFS
	staticFS = ginstatic.LocalFile(tempDir, false)

	t.Cleanup(func() {
		staticFS = originalStaticFS
	})
}

func TestHandler_ReturnsJSON404ForUnknownAPIPaths(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	useTestStaticFS(t)

	router := gin.New()
	router.NoRoute(Handler())

	for _, path := range []string{"/v1/not-found", "/anthropic/not-found", "/admin/not-found"} {
		t.Run(path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, path, nil)

			router.ServeHTTP(recorder, req)

			require.Equal(t, http.StatusNotFound, recorder.Code)
			require.Contains(t, recorder.Header().Get("Content-Type"), "application/json")

			var resp objects.ErrorResponse
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
			require.Equal(t, http.StatusText(http.StatusNotFound), resp.Error.Type)
			require.Equal(t, "path not found: "+path, resp.Error.Message)
		})
	}
}

func TestHandler_ServesSPAIndexForFrontendRoutes(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	useTestStaticFS(t)

	router := gin.New()
	router.NoRoute(Handler())

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/settings/profile", nil)

	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Header().Get("Content-Type"), "text/html")
	require.Equal(t, "no-cache, no-store, must-revalidate", recorder.Header().Get("Cache-Control"))
}

func TestCodexPrefixAPINotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	useTestStaticFS(t)
	router := gin.New()
	router.NoRoute(Handler())
	result := httptest.NewRecorder()
	router.ServeHTTP(result, httptest.NewRequest(http.MethodGet, "/codex/alpha/notes/v2/unknown", nil))
	require.Equal(t, http.StatusNotFound, result.Code)
	require.Contains(t, result.Header().Get("Content-Type"), "application/json")
}

func TestHandler_DoesNotFallbackMissingStaticAssetToSPAIndex(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	useTestStaticFS(t)

	router := gin.New()
	router.NoRoute(Handler())

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/assets/definitely-missing.js", nil)

	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusNotFound, recorder.Code)
	require.NotContains(t, recorder.Header().Get("Content-Type"), "text/html")
}

func TestHandlerCachesOnlyExistingContentHashedAssets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	root := t.TempDir()
	require.NoError(t, os.Mkdir(root+"/assets", 0o755))
	for _, name := range []string{"app-Ab12cd_9.js", "app.js"} {
		require.NoError(t, os.WriteFile(root+"/assets/"+name, []byte("console.log('fixture')"), 0o644))
	}
	original := staticFS
	staticFS = ginstatic.LocalFile(root, false)
	t.Cleanup(func() { staticFS = original })
	router := gin.New()
	router.NoRoute(Handler())
	for _, tc := range []struct {
		path   string
		status int
		cache  string
	}{
		{"/assets/app-Ab12cd_9.js", http.StatusOK, "public, max-age=31536000, immutable"},
		{"/assets/app.js", http.StatusOK, ""},
		{"/assets/missing-Ab12cd_9.js", http.StatusNotFound, ""},
	} {
		t.Run(tc.path, func(t *testing.T) {
			result := httptest.NewRecorder()
			router.ServeHTTP(result, httptest.NewRequest(http.MethodGet, tc.path, nil))
			require.Equal(t, tc.status, result.Code)
			require.Equal(t, tc.cache, result.Header().Get("Cache-Control"))
		})
	}
}
