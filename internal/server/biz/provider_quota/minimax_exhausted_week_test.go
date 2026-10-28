package provider_quota

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMinimaxExhaustedWeeklyWindowRemainsVisibleAndBlocksRouting(t *testing.T) {
	quota, err := parseMinimaxResponse([]byte(`{"base_resp":{"status_code":0},"model_remains":[{"model_name":"general","current_interval_status":1,"current_interval_remaining_percent":80,"current_weekly_status":2,"current_weekly_remaining_percent":0,"weekly_boost_permille":1500,"weekly_start_time":4070908800000,"weekly_end_time":4071513600000}]}`))
	require.NoError(t, err)
	require.Len(t, quota.Limits, 2)
	weekly := quota.Limits[1]
	require.Equal(t, QuotaWindowWeekly, weekly.Window)
	require.Equal(t, "exhausted", weekly.Status)
	require.False(t, weekly.Ready)
	require.Equal(t, time.UnixMilli(4071513600000), *weekly.NextResetAt)
	require.Equal(t, time.UnixMilli(4070908800000), *weekly.PeriodStart)
	require.Equal(t, "exhausted", quota.Status)
	require.False(t, quota.Ready)
	rows := quota.RawData["rows"].([]minimaxModelRow)
	require.Equal(t, "exhausted", rows[0].WeeklyStatus)
	require.Equal(t, 150.0, rows[0].WeeklyTotalPercent)
	require.NotNil(t, rows[0].WeeklyResetAt)
}
