package provider_quota

import (
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestClaudeCodeOverageDoesNotExhaustSharedQuota(t *testing.T) {
	for _, tc := range []struct{ name, fiveHour, sevenDay, want string }{
		{"absent shared evidence", "", "", "unknown"},
		{"healthy shared windows", "allowed", "allowed", "available"},
		{"shared warning", "allowed_warning", "allowed", "warning"},
		{"five hour exhausted", "rejected", "allowed", "exhausted"},
		{"partial shared evidence", "allowed", "", "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now().Truncate(time.Second)
			billing := strconv.FormatInt(now.Add(20*24*time.Hour).Unix(), 10)
			headers := make(http.Header)
			headers.Set("Anthropic-Ratelimit-Unified-Status", "rejected")
			headers.Set("Anthropic-Ratelimit-Unified-Representative-Claim", "overage")
			headers.Set("Anthropic-Ratelimit-Unified-Reset", billing)
			headers.Set("Anthropic-Ratelimit-Unified-Overage-Reset", billing)
			if tc.fiveHour != "" {
				headers.Set("Anthropic-Ratelimit-Unified-5h-Status", tc.fiveHour)
				headers.Set("Anthropic-Ratelimit-Unified-5h-Reset", strconv.FormatInt(now.Add(time.Hour).Unix(), 10))
			}
			if tc.sevenDay != "" {
				headers.Set("Anthropic-Ratelimit-Unified-7d-Status", tc.sevenDay)
			}
			quota, err := NewClaudeCodeQuotaChecker(nil).parseResponse(headers)
			require.NoError(t, err)
			require.Equal(t, tc.want, quota.Status)
			state, _ := EvaluateQuotaRouting(quota.Limits, quota.Status, QuotaLimitTypeToken, now)
			if tc.want == "exhausted" {
				require.Equal(t, RoutingExhausted, state)
				require.NotNil(t, quota.NextResetAt)
				require.Equal(t, now.Add(time.Hour), *quota.NextResetAt)
			} else {
				require.NotEqual(t, RoutingExhausted, state)
				require.Nil(t, quota.NextResetAt)
			}
			require.Equal(t, now.Add(20*24*time.Hour).Unix(), quota.RawData["reset"])
		})
	}
}
