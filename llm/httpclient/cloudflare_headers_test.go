package httpclient

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCloudflareCredentialsMaskedWithoutChangingTransport(t *testing.T) {
	for _, name := range []string{"X-Auth-Key", "x-auth-user-service-key"} {
		headers := http.Header{name: {"fixture-cloudflare-secret"}, "X-Request-Id": {"public-id"}}
		masked := MaskSensitiveHeaders(headers)
		require.Equal(t, []string{"******"}, masked[name])
		require.Equal(t, []string{"fixture-cloudflare-secret"}, headers[name])
		require.Equal(t, []string{"public-id"}, masked["X-Request-Id"])
	}
}
