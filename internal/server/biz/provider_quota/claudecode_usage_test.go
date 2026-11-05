package provider_quota

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/objects"
)

func TestClaudeCodeQuotaUsageReadOnly(t *testing.T) {
	client := newCodexQuotaTestHTTPClient(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		require.Equal(t, http.MethodGet, req.Method)
		require.Equal(t, "https://relay.example/api/oauth/usage", req.URL.String())
		require.Equal(t, "Bearer synthetic", req.Header.Get("Authorization"))
		require.Nil(t, req.Body)
		return codexQuotaTestResponse(`{"five_hour":{"utilization":95,"resets_at":"2030-01-01T00:00:00Z"},"seven_day":{"utilization":10},"seven_day_opus":{"utilization":100},"extra_usage":{"is_enabled":true},"metadata":"ignored"}`), nil
	}))
	data, err := NewClaudeCodeQuotaChecker(client).CheckUsage(context.Background(), &ent.Channel{
		BaseURL:     "https://relay.example/v1",
		Credentials: objects.ChannelCredentials{OAuth: &objects.OAuthCredentials{AccessToken: "synthetic"}},
	})
	require.NoError(t, err)
	require.Len(t, data.Limits, 2)
	require.Equal(t, .95, data.Limits[0].UsageRatio)
	require.Equal(t, QuotaWindow5h, data.Limits[0].Window)
	require.Equal(t, .1, data.Limits[1].UsageRatio)
	require.Equal(t, QuotaWindow7d, data.Limits[1].Window)
}

func TestClaudeCodeQuotaUsageMissingIsNotZero(t *testing.T) {
	data, err := parseClaudeCodeUsage([]byte(`{"five_hour":{"resets_at":null},"seven_day":null}`))
	require.NoError(t, err)
	require.Empty(t, data.Limits)
	data, err = parseClaudeCodeUsage([]byte(`{"five_hour":{"utilization":0,"resets_at":null}}`))
	require.NoError(t, err)
	require.Len(t, data.Limits, 1, "an unused window may not have a reset time yet")
	require.Zero(t, data.Limits[0].UsageRatio)
}
