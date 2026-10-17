package orchestrator

import (
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/server/biz"
)

func TestRequestLatencyIncludesPreparationAndRetries(t *testing.T) {
	start := time.Now().Add(-6 * time.Minute)
	perf := &biz.PerformanceRecord{
		StartTime: start.Add(320 * time.Second), EndTime: start.Add(342 * time.Second),
		FirstTokenTime: lo.ToPtr(start.Add(334 * time.Second)), Stream: true,
	}
	state := &PersistenceState{RequestStartedAt: start, Perf: perf}
	metrics := requestLatencyMetrics(state, perf)
	require.EqualValues(t, 342000, *metrics.LatencyMs)
	require.EqualValues(t, 334000, *metrics.FirstTokenLatencyMs)
	first, latency, _ := perf.Calculate()
	require.EqualValues(t, 14000, first, "channel metrics must keep their independent clock")
	require.EqualValues(t, 22000, latency)
	perf.Stream = false
	require.Nil(t, requestLatencyMetrics(state, perf).FirstTokenLatencyMs)
	require.Nil(t, requestLatencyMetrics(nil, nil))
	require.EqualValues(t, 22000, *requestLatencyMetrics(nil, perf).LatencyMs)
}
