package orchestrator

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNormalizeScheduledHealthCheckTimes(t *testing.T) {
	times, err := normalizeScheduledHealthCheckTimes([]string{"23:59:58", " 08:00:01 ", "08:00:01"})
	require.NoError(t, err)
	require.Equal(t, []string{"08:00:01", "23:59:58"}, times)

	_, err = normalizeScheduledHealthCheckTimes([]string{"24:00:00"})
	require.Error(t, err)
}

func TestScheduledChannelChecksDueBetween(t *testing.T) {
	location := time.FixedZone("test", 8*60*60)
	registered := map[int][]string{
		2: {"10:00:00", "12:00:00"},
		1: {"10:00:00"},
	}

	due := scheduledChannelChecksDueBetween(
		registered,
		time.Date(2026, time.September, 2, 9, 59, 59, 0, location),
		time.Date(2026, time.September, 2, 10, 0, 1, 0, location),
	)
	require.Equal(t, []scheduledChannelCheck{
		{channelID: 1, scheduledAt: "10:00:00", occursAt: time.Date(2026, time.September, 2, 10, 0, 0, 0, location)},
		{channelID: 2, scheduledAt: "10:00:00", occursAt: time.Date(2026, time.September, 2, 10, 0, 0, 0, location)},
	}, due)
}

func TestScheduledChannelChecksDueBetweenCatchesLatestRunAfterWake(t *testing.T) {
	location := time.FixedZone("test", 8*60*60)
	due := scheduledChannelChecksDueBetween(
		map[int][]string{7: {"06:30:00"}},
		time.Date(2026, time.August, 31, 23, 0, 0, 0, location),
		time.Date(2026, time.September, 2, 8, 0, 0, 0, location),
	)

	require.Equal(t, []scheduledChannelCheck{{
		channelID:   7,
		scheduledAt: "06:30:00",
		occursAt:    time.Date(2026, time.September, 2, 6, 30, 0, 0, location),
	}}, due)
}

func TestScheduledChannelChecksDueBetweenDoesNotReplayOnStartupOrClockRollback(t *testing.T) {
	now := time.Date(2026, time.September, 2, 8, 0, 0, 0, time.Local)
	registered := map[int][]string{1: {"07:00:00"}}

	require.Empty(t, scheduledChannelChecksDueBetween(registered, now, now))
	require.Empty(t, scheduledChannelChecksDueBetween(registered, now, now.Add(-time.Minute)))
}
