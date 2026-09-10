package provider_quota

import (
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCodexQuotaChecker_ExhaustionPreservesIndependentWindowUsage(t *testing.T) {
	t.Parallel()

	for _, secondaryExhausted := range []bool{false, true} {
		name := "five_hour_exhausted"
		primaryUsage, secondaryUsage := 100.0, 80.0
		primaryStatus, secondaryStatus := "exhausted", "warning"
		if secondaryExhausted {
			name = "weekly_exhausted"
			primaryUsage, secondaryUsage = 20, 100
			primaryStatus, secondaryStatus = "available", "exhausted"
		}
		t.Run(name, func(t *testing.T) {
			body, err := json.Marshal(map[string]any{
				"rate_limit": map[string]any{
					"allowed":       false,
					"limit_reached": true,
					"primary_window": map[string]any{
						"used_percent":         primaryUsage,
						"limit_window_seconds": 18000,
					},
					"secondary_window": map[string]any{
						"used_percent":         secondaryUsage,
						"limit_window_seconds": 604800,
					},
				},
			})
			require.NoError(t, err)
			quota, err := (&CodexQuotaChecker{}).parseResponse(body)
			require.NoError(t, err)
			require.Equal(t, "exhausted", quota.Status)
			require.False(t, quota.Ready)
			require.Len(t, quota.Limits, 2)
			require.Equal(t, QuotaWindow5h, quota.Limits[0].Window)
			require.Equal(t, QuotaWindow7d, quota.Limits[1].Window)
			require.InDelta(t, primaryUsage/100, quota.Limits[0].UsageRatio, 0.0001)
			require.InDelta(t, secondaryUsage/100, quota.Limits[1].UsageRatio, 0.0001)
			require.Equal(t, primaryStatus, quota.Limits[0].Status)
			require.Equal(t, secondaryStatus, quota.Limits[1].Status)
			routing, _ := EvaluateQuotaRouting(quota.Limits, quota.Status, QuotaLimitTypeToken, time.Now())
			require.Equal(t, RoutingExhausted, routing)
		})
	}
}

func TestCodexQuotaChecker_ExhaustedWindowWithoutUsageReportsZeroRemaining(t *testing.T) {
	t.Parallel()

	resetAt := time.Date(2099, 9, 13, 12, 0, 0, 0, time.UTC)
	body := []byte(`{
		"rate_limit": {
			"allowed": false,
			"primary_window": {
				"reset_at": ` + strconv.FormatInt(resetAt.Unix(), 10) + `,
				"limit_window_seconds": 604800
			}
		}
	}`)

	checker := &CodexQuotaChecker{}

	quota, err := checker.parseResponse(body)

	require.NoError(t, err)
	require.Len(t, quota.Limits, 1)
	require.Equal(t, QuotaWindow7d, quota.Limits[0].Window)
	require.Equal(t, "exhausted", quota.Limits[0].Status)
	require.False(t, quota.Limits[0].Ready)
	require.Equal(t, float64(1), quota.Limits[0].UsageRatio)
	require.Equal(t, "exhausted", quota.Status)
	require.False(t, quota.Ready)
	require.True(t, quota.NextResetAt.Equal(resetAt))
	require.True(t, quota.Limits[0].PeriodStart.Equal(resetAt.Add(-7*24*time.Hour)))
}
