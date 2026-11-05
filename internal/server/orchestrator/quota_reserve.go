package orchestrator

import (
	"context"
	"math"
	"time"

	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/internal/server/biz/provider_quota"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

func quotaReserveBlocks(ctx context.Context, ch *biz.Channel, req *llm.Request, memory bool, provider ProviderQuotaStatusProvider, now time.Time) bool {
	if ch == nil || ch.Channel == nil || ch.Settings == nil || ch.Settings.QuotaReserve == nil || provider == nil {
		return false
	}
	if ch.Type != channel.TypeCodex && ch.Type != channel.TypeClaudecode {
		return false
	}
	r := ch.Settings.QuotaReserve
	if r.FiveHourPercent == nil && r.SevenDayPercent == nil {
		return false
	}
	if memory && r.AllowMemory || isStandaloneSearch(req) && r.AllowSearch || isStandaloneImage(req) && r.AllowImage {
		return false
	}
	status := provider.GetQuotaStatus(ctx, ch.ID)
	if status == nil {
		return false
	}
	for _, limit := range status.Limits {
		// Named model-specific reserves and image buckets do not represent the
		// account's ordinary 5h/7d allowance. Never infer them from display names.
		if limit.Type != provider_quota.QuotaLimitTypeToken || limit.AvailabilityGroup != "" || limit.Status == "unknown" ||
			math.IsNaN(limit.UsageRatio) || math.IsInf(limit.UsageRatio, 0) || limit.UsageRatio < 0 {
			continue
		}
		if limit.NextResetAt != nil && !limit.NextResetAt.After(now) {
			continue
		}
		if limit.PeriodStart != nil && limit.PeriodStart.After(now) {
			continue
		}
		var percent *int
		switch limit.Window {
		case provider_quota.QuotaWindow5h:
			percent = r.FiveHourPercent
		case provider_quota.QuotaWindow7d:
			percent = r.SevenDayPercent
		}
		if percent != nil && *percent >= 1 && *percent <= 100 && limit.UsageRatio >= float64(*percent)/100 {
			return true
		}
	}
	return false
}

func (g *QuotaRoutingGate) reserveBlocks(ctx context.Context, candidate *ChannelModelsCandidate, req *llm.Request) bool {
	return quotaReserveBlocks(ctx, candidate.Channel, req, shared.GetCodexMemoryTask(ctx) != shared.CodexMemoryNone, g.provider, time.Now())
}
