package static

import (
	"bytes"
	"compress/gzip"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// servePrecompressed sends build-time gzip bytes without runtime compression.
// Identity clients retain ordinary MIME, HEAD and Range behavior via ServeContent.
func servePrecompressed(c *gin.Context, name string, immutable bool) bool {
	file, err := staticFS.Open(name + ".gz")
	if err != nil {
		return false
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.IsDir() {
		return false
	}
	c.Writer.Header().Add("Vary", "Accept-Encoding")
	contentType := mime.TypeByExtension(filepath.Ext(name))
	if contentType != "" {
		c.Header("Content-Type", contentType)
	}
	var content io.ReadSeeker = file
	if acceptsGzip(c.Request.Header.Values("Accept-Encoding")) {
		c.Header("Content-Encoding", "gzip")
	} else {
		reader, err := gzip.NewReader(file)
		if err != nil {
			c.AbortWithStatus(http.StatusInternalServerError)
			return true
		}
		decoded, err := io.ReadAll(reader)
		_ = reader.Close()
		if err != nil {
			c.AbortWithStatus(http.StatusInternalServerError)
			return true
		}
		content = bytes.NewReader(decoded)
	}
	if immutable {
		c.Header("Cache-Control", "public, max-age=31536000, immutable")
	}
	http.ServeContent(c.Writer, c.Request, name, info.ModTime(), content)
	c.Abort()
	return true
}

func acceptsGzip(headers []string) bool {
	var gzipQuality, wildcardQuality float64
	explicitGzip := false
	for _, header := range headers {
		for value := range strings.SplitSeq(header, ",") {
			parts := strings.Split(value, ";")
			encoding := strings.ToLower(strings.TrimSpace(parts[0]))
			quality := 1.0
			for _, parameter := range parts[1:] {
				key, raw, found := strings.Cut(strings.TrimSpace(parameter), "=")
				if found && strings.EqualFold(key, "q") {
					parsed, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
					if err != nil || !(parsed >= 0 && parsed <= 1) {
						quality = 0
					} else {
						quality = parsed
					}
				}
			}
			switch encoding {
			case "gzip":
				explicitGzip = true
				gzipQuality = max(gzipQuality, quality)
			case "*":
				wildcardQuality = max(wildcardQuality, quality)
			}
		}
	}
	if explicitGzip {
		return gzipQuality > 0
	}
	return wildcardQuality > 0
}
