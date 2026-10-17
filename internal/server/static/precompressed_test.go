package static

import (
	"bytes"
	"compress/gzip"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	ginstatic "github.com/gin-contrib/static"
)

func TestPrecompressedAssetsPreserveHTTPBehavior(t *testing.T) {
	gin.SetMode(gin.TestMode)
	root := t.TempDir()
	require.NoError(t, os.Mkdir(root+"/assets", 0o755))
	raw := []byte("console.log('content-canary');")
	var packed bytes.Buffer
	writer := gzip.NewWriter(&packed)
	_, err := writer.Write(raw)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	require.NoError(t, os.WriteFile(root+"/assets/app-Ab12cd_9.js.gz", packed.Bytes(), 0o644))
	require.NoError(t, os.WriteFile(root+"/index.html.gz", packed.Bytes(), 0o644))
	original := staticFS
	staticFS = ginstatic.LocalFile(root, false)
	t.Cleanup(func() { staticFS = original })
	router := gin.New()
	router.NoRoute(Handler())
	for _, tc := range []struct {
		name, method, encoding, byteRange, path string
		status                                  int
		gzip                                    bool
	}{
		{name: "gzip", encoding: "gzip, deflate, br", status: http.StatusOK, gzip: true},
		{name: "identity", status: http.StatusOK},
		{name: "gzip forbidden overrides wildcard", encoding: "gzip;q=0, *;q=1", status: http.StatusOK},
		{name: "wildcard", encoding: "br, *;q=0.5", status: http.StatusOK, gzip: true},
		{name: "invalid quality", encoding: "gzip;q=oops", status: http.StatusOK},
		{name: "invalid numeric quality", encoding: "gzip;q=NaN", status: http.StatusOK},
		{name: "head gzip", method: http.MethodHead, encoding: "gzip", status: http.StatusOK, gzip: true},
		{name: "head identity", method: http.MethodHead, status: http.StatusOK},
		{name: "identity range", byteRange: "bytes=0-6", status: http.StatusPartialContent},
		{name: "SPA gzip", path: "/channels", encoding: "gzip", status: http.StatusOK, gzip: true},
		{name: "SPA identity", path: "/channels", status: http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			method := tc.method
			if method == "" {
				method = http.MethodGet
			}
			path := tc.path
			if path == "" {
				path = "/assets/app-Ab12cd_9.js"
			}
			req := httptest.NewRequest(method, path, nil)
			req.Header.Set("Accept-Encoding", tc.encoding)
			req.Header.Set("Range", tc.byteRange)
			result := httptest.NewRecorder()
			router.ServeHTTP(result, req)
			require.Equal(t, tc.status, result.Code)
			require.Contains(t, result.Header().Values("Vary"), "Accept-Encoding")
			if tc.gzip {
				require.Equal(t, "gzip", result.Header().Get("Content-Encoding"))
			} else {
				require.Empty(t, result.Header().Get("Content-Encoding"))
			}
			if tc.path == "/channels" {
				require.Contains(t, result.Header().Get("Content-Type"), "text/html")
				require.Equal(t, "no-cache, no-store, must-revalidate", result.Header().Get("Cache-Control"))
			} else {
				require.Contains(t, result.Header().Get("Content-Type"), "javascript")
				require.Equal(t, "public, max-age=31536000, immutable", result.Header().Get("Cache-Control"))
			}
			if method == http.MethodHead {
				require.Empty(t, result.Body.Bytes())
				return
			}
			got := result.Body.Bytes()
			if tc.gzip {
				reader, err := gzip.NewReader(bytes.NewReader(got))
				require.NoError(t, err)
				got, err = io.ReadAll(reader)
				require.NoError(t, err)
				require.NoError(t, reader.Close())
			}
			if tc.byteRange != "" {
				require.Equal(t, raw[:7], got)
			} else {
				require.Equal(t, raw, got)
			}
		})
	}
}

func TestPackedEmbeddedAssetsMatchViteOutput(t *testing.T) {
	if _, err := dist.ReadFile("dist/index.html.gz"); err != nil {
		t.Skip("packed assets are validated after make build-frontend in hosted CI")
	}
	count := 0
	require.NoError(t, fs.WalkDir(dist, "dist", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(name, ".gz") {
			return nil
		}
		packed, err := dist.ReadFile(name)
		require.NoError(t, err)
		reader, err := gzip.NewReader(bytes.NewReader(packed))
		require.NoError(t, err)
		decoded, err := io.ReadAll(reader)
		require.NoError(t, err)
		require.NoError(t, reader.Close())
		original, err := os.ReadFile("../../../../frontend/" + strings.TrimSuffix(name, ".gz"))
		require.NoError(t, err)
		require.Equal(t, original, decoded, name)
		count++
		return nil
	}))
	require.Positive(t, count)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.NoRoute(Handler())
	result := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/channels", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	router.ServeHTTP(result, req)
	require.Equal(t, http.StatusOK, result.Code)
	require.Equal(t, "gzip", result.Header().Get("Content-Encoding"))
	require.Contains(t, result.Header().Get("Content-Type"), "text/html")
	require.Equal(t, "no-cache, no-store, must-revalidate", result.Header().Get("Cache-Control"))
}
