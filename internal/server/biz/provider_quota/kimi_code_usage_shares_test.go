package provider_quota

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestKimiCodeMonthlySharesDriveReadinessWithoutInventingCounts(t *testing.T) {
	quota, err := parseKimiCodeUsageResponse([]byte(`{"usage":{"limit":100,"used":0},"usages":{"limit5h":{"usedRatio":0.25},"limit7d":{"usedRatio":"0.5"},"limitMonthTotal":{"usedRatio":1,"resetTime":1793491200000},"limitMonthCode":{"usedRatio":0.1}}}`))
	require.NoError(t, err)
	require.False(t, quota.Ready)
	require.Len(t, quota.Limits, 3)
	rows := quota.RawData["rows"].([]kimiCodeUsageRow)
	require.Len(t, rows, 3)
	for _, row := range rows {
		require.NotNil(t, row.UsageRatio)
		require.Zero(t, row.Limit)
		require.Zero(t, row.Used)
	}
	require.Equal(t, 1.0, *rows[2].UsageRatio)
	require.Equal(t, time.UnixMilli(1793491200000).UTC().Format(time.RFC3339), *rows[2].ResetAt)
}

func TestKimiCodeMonthlyShareFallbackAndMalformedValues(t *testing.T) {
	for _, body := range []string{
		`{"usages":{"limit_month_code":{"used_ratio":0.75,"reset_time":"2026-11-01T00:00:00Z"}}}`,
		`{"usages":{"limit_month_total":{"used_ratio":"NaN"},"limit_month_code":{"used_ratio":"0.75","reset_time":"1793491200"}}}`,
	} {
		quota, err := parseKimiCodeUsageResponse([]byte(body))
		require.NoError(t, err)
		require.Len(t, quota.Limits, 1)
		require.InDelta(t, 0.75, quota.Limits[0].UsageRatio, 0.000001)
		require.NotNil(t, quota.Limits[0].NextResetAt)
	}
	_, err := parseKimiCodeUsageResponse([]byte(`{"usages":{"limit_month_total":{"used_ratio":"Inf"}}}`))
	require.Error(t, err)
}
