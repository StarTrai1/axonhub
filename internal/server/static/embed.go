package static

import (
	"bytes"
	"compress/gzip"
	"embed"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-contrib/static"
	"github.com/gin-gonic/gin"

	"github.com/looplj/axonhub/internal/objects"
)

//go:embed all:dist/*
var dist embed.FS

var staticFS static.ServeFileSystem

// Vite content hashes change whenever asset contents change. Only these URLs
// are immutable; entry HTML, unhashed files and missing assets must revalidate.
var immutableAssetPath = regexp.MustCompile(`^/assets/[^/]+-[A-Za-z0-9_-]{8,}\.[A-Za-z0-9]+$`)

var apiPrefixes = []string{
	"/admin",
	"/anthropic",
	"/codex",
	"/doubao",
	"/gemini",
	"/jina",
	"/openapi",
	"/v1",
	"/v1beta",
}

func init() {
	var err error

	staticFS, err = static.EmbedFolder(dist, "dist")
	if err != nil {
		panic(err)
	}
}

func Handler(basePaths ...string) gin.HandlerFunc {
	basePath := ""
	if len(basePaths) > 0 { basePath = basePaths[0] }
	serveAsset := static.Serve("/", staticFS)
	index := loadSPAIndex(basePath)
	var compressed bytes.Buffer
	if index != nil {
		w := gzip.NewWriter(&compressed)
		_, _ = w.Write(index)
		_ = w.Close()
	}
	return func(c *gin.Context) {
		path := c.Request.URL.Path

		if isAPIPath(path) {
			serveAPINotFound(c, path)
			return
		}

		if isStaticAssetPath(path) {
			if servePrecompressed(c, path, immutableAssetPath.MatchString(path)) {
				return
			}
			if immutableAssetPath.MatchString(path) && staticFS.Exists("/", path) {
				c.Header("Cache-Control", "public, max-age=31536000, immutable")
			}
			serveAsset(c)
			return
		}

		serveSPAIndex(c, index, compressed.Bytes(), basePath)
	}
}

func serveAPINotFound(c *gin.Context, path string) {
	err := fmt.Errorf("path not found: %s", path)
	_ = c.Error(err)
	c.AbortWithStatusJSON(http.StatusNotFound, objects.ErrorResponse{
		Error: objects.Error{
			Type:    http.StatusText(http.StatusNotFound),
			Message: err.Error(),
		},
	})
}

func loadSPAIndex(basePath string) []byte {
	file, err := staticFS.Open("/index.html")
	if err != nil {
		return nil
	}
	defer file.Close()

	index, err := io.ReadAll(file)
	if err != nil {
		return nil
	}

	// Vite emits relative asset URLs so lazy chunks resolve from their modules.
	// The entry document must use absolute URLs because it also serves deep links.
	assetPath := basePath + "/assets/"
	replacer := strings.NewReplacer(
		`src="./assets/`, `src="`+assetPath,
		`href="./assets/`, `href="`+assetPath,
		`src="/assets/`, `src="`+assetPath,
		`href="/assets/`, `href="`+assetPath,
		`href="/favicon"`, `href="`+basePath+`/favicon"`,
	)
	html := replacer.Replace(string(index))
	if basePath != "" {
		meta := `<meta name="axonhub-base-path" content="` + basePath + `">`
		html = strings.Replace(html, "<head>", "<head>\n    "+meta, 1)
	}
	return []byte(html)
}

func serveSPAIndex(c *gin.Context, index, compressed []byte, basePath string) {
	// SPA routes should always reload the latest index.html.
	c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
	c.Header("Pragma", "no-cache")
	c.Header("Expires", "0")
	if index == nil {
		if basePath == "" && servePrecompressed(c, "/index.html", false) { return }
		c.FileFromFS("/", staticFS)
		return
	}
	c.Writer.Header().Add("Vary", "Accept-Encoding")
	if acceptsGzip(c.Request.Header.Values("Accept-Encoding")) {
		c.Header("Content-Encoding", "gzip")
		index = compressed
	}
	c.Header("Content-Type", "text/html; charset=utf-8")
	http.ServeContent(c.Writer, c.Request, "index.html", time.Time{}, bytes.NewReader(index))
}

func isAPIPath(path string) bool {
	for _, prefix := range apiPrefixes {
		if hasPathPrefix(path, prefix) {
			return true
		}
	}

	return false
}

// isStaticAssetPath determines if a path should be served as a static asset.
func isStaticAssetPath(path string) bool {
	if strings.HasPrefix(path, "/assets/") ||
		strings.HasPrefix(path, "/images/") ||
		path == "/favicon.ico" ||
		strings.HasSuffix(path, ".js") ||
		strings.HasSuffix(path, ".css") ||
		strings.HasSuffix(path, ".png") ||
		strings.HasSuffix(path, ".jpg") ||
		strings.HasSuffix(path, ".jpeg") ||
		strings.HasSuffix(path, ".gif") ||
		strings.HasSuffix(path, ".svg") ||
		strings.HasSuffix(path, ".webp") {
		return true
	}

	return false
}

func hasPathPrefix(path, prefix string) bool {
	return path == prefix || strings.HasPrefix(path, prefix+"/")
}
