package gemini

import (
	"net/url"
	"strings"
)

// vertexModelURL accepts a host, a version/project/location prefix, or a full
// publisher models prefix. All Vertex request types share these path rules.
func vertexModelURL(base, model, action string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if strings.Contains(base, "/publishers/") {
		return base + "/" + model + ":" + action
	}
	parsed, err := url.Parse(base)
	hasVersion := false
	if err == nil {
		for _, segment := range strings.Split(parsed.Path, "/") {
			if len(segment) >= 2 && segment[0] == 'v' && segment[1] >= '0' && segment[1] <= '9' {
				hasVersion = true
				break
			}
		}
	}
	if !hasVersion {
		base += "/v1"
	}
	return base + "/publishers/google/models/" + model + ":" + action
}
