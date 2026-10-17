package orchestrator

import (
	"time"

	"github.com/looplj/axonhub/internal/server/biz"
)

// Request logs measure the wait from admission through the final attempt.
// Execution logs and load balancing keep the original per-attempt metrics.
func requestLatencyMetrics(state *PersistenceState, perf *biz.PerformanceRecord) *biz.LatencyMetrics {
	var start time.Time
	if state != nil {
		start = state.RequestStartedAt
	}
	if start.IsZero() && perf != nil {
		start = perf.StartTime
	}
	if start.IsZero() {
		return nil
	}
	end := time.Now()
	if perf != nil && !perf.EndTime.IsZero() {
		end = perf.EndTime
	}
	latency := biz.ClampLatency(end.Sub(start).Milliseconds())
	metrics := &biz.LatencyMetrics{LatencyMs: &latency}
	if perf != nil && perf.Stream && perf.FirstTokenTime != nil {
		first := biz.ClampLatency(perf.FirstTokenTime.Sub(start).Milliseconds())
		metrics.FirstTokenLatencyMs = &first
	}
	return metrics
}
