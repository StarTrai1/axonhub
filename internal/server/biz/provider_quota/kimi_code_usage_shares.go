package provider_quota

import (
	"math"
	"strconv"
	"time"
)

// Newer Kimi memberships report shares instead of absolute counts. Prefer an
// explicit share over the old count for the same window. The monthly coding
// share is the fallback only when no usable monthly total is supplied.
func mergeKimiCodeUsageShares(rows []kimiCodeUsageRow, windows []time.Duration, usages map[string]map[string]any) ([]kimiCodeUsageRow, []time.Duration) {
	seen := make(map[time.Duration]bool)
	for _, spec := range []struct {
		key, camel, label string
		window            time.Duration
	}{
		{"limit_5h", "limit5h", "5h limit", 5 * time.Hour},
		{"limit_7d", "limit7d", "Weekly limit", 7 * 24 * time.Hour},
		{"limit_month_total", "limitMonthTotal", "Monthly limit", 30 * 24 * time.Hour},
		{"limit_month_code", "limitMonthCode", "Monthly limit", 30 * 24 * time.Hour},
	} {
		if seen[spec.window] {
			continue
		}
		raw := usages[spec.key]
		if raw == nil {
			raw = usages[spec.camel]
		}
		ratio, ok := kimiCodeFiniteNumber(raw["used_ratio"])
		if !ok {
			ratio, ok = kimiCodeFiniteNumber(raw["usedRatio"])
		}
		if !ok {
			continue
		}
		seen[spec.window] = true
		ratio = max(0, min(1, ratio))
		row := kimiCodeUsageRow{Label: spec.label, UsageRatio: &ratio, ResetAt: kimiCodeShareResetAt(raw)}
		keptRows, keptWindows := rows[:0], windows[:0]
		for i, previous := range rows {
			if windows[i] != spec.window {
				keptRows = append(keptRows, previous)
				keptWindows = append(keptWindows, windows[i])
			}
		}
		rows, windows = append(keptRows, row), append(keptWindows, spec.window)
	}
	return rows, windows
}

func kimiCodeFiniteNumber(value any) (float64, bool) {
	var n float64
	switch v := value.(type) {
	case float64:
		n = v
	case string:
		var err error
		n, err = strconv.ParseFloat(v, 64)
		if err != nil {
			return 0, false
		}
	default:
		return 0, false
	}
	return n, !math.IsNaN(n) && !math.IsInf(n, 0)
}

func kimiCodeShareResetAt(raw map[string]any) *string {
	for _, key := range []string{"resetTime", "resetAt", "reset_at", "reset_time"} {
		if value, ok := raw[key].(string); ok {
			if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
				formatted := parsed.UTC().Format(time.RFC3339Nano)
				return &formatted
			}
		}
		if n, ok := kimiCodeFiniteNumber(raw[key]); ok && n > 0 && n < 1e15 {
			if n > 1e11 {
				n /= 1000
			}
			formatted := time.Unix(int64(n), 0).UTC().Format(time.RFC3339)
			return &formatted
		}
	}
	return nil
}
