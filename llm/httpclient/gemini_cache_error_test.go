package httpclient

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMissingGeminiCacheIsRequestScoped(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
		want   bool
	}{
		{403, `{"error":{"code":403,"message":"CachedContent not found (or permission denied)","status":"PERMISSION_DENIED"}}`, true},
		{403, `{"error":{"message":"Permission denied for API key"}}`, false},
		{401, `{"error":{"message":"CachedContent not found"}}`, false},
		{500, `{"error":{"message":"CachedContent not found"}}`, false},
		{403, `CachedContent not found`, false},
	} {
		err := fmt.Errorf("wrapped: %w", &Error{StatusCode: tc.status, Body: []byte(tc.body)})
		require.Equal(t, tc.want, IsMissingGeminiCachedContent(err))
	}
}
