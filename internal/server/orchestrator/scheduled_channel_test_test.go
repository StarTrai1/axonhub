package orchestrator

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeScheduledHealthCheckTimes(t *testing.T) {
	times, err := normalizeScheduledHealthCheckTimes([]string{"23:59:58", " 08:00:01 ", "08:00:01"})
	require.NoError(t, err)
	require.Equal(t, []string{"08:00:01", "23:59:58"}, times)

	_, err = normalizeScheduledHealthCheckTimes([]string{"24:00:00"})
	require.Error(t, err)
}

func TestScheduledChannelTestCron(t *testing.T) {
	require.Equal(t, "7 6 5 * * * *", scheduledChannelTestCron("05:06:07"))
}
