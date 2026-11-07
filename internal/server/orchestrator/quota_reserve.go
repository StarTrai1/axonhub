package orchestrator

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/internal/server/biz/provider_quota"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

func quotaReserveBlocks(ctx context.Context, ch *biz.Channel, req *llm.Request, memory bool, provider ProviderQuotaStatusProvider, now time.Time) bool {
	r := applicableQuotaReserve(ch, req, memory)
	if r == nil || provider == nil {
		return false
	}
	blocked, _ := evaluateQuotaReserve(r, provider.GetQuotaStatus(ctx, ch.ID), now)
	return blocked
}

func applicableQuotaReserve(ch *biz.Channel, req *llm.Request, memory bool) *objects.ChannelQuotaReserve {
	if ch == nil || ch.Channel == nil || ch.Settings == nil || ch.Settings.QuotaReserve == nil ||
		(ch.Type != channel.TypeCodex && ch.Type != channel.TypeClaudecode) {
		return nil
	}
	r := ch.Settings.QuotaReserve
	if r.FiveHourPercent == nil && r.SevenDayPercent == nil ||
		memory && r.AllowMemory || isStandaloneSearch(req) && r.AllowSearch || isStandaloneImage(req) && r.AllowImage {
		return nil
	}
	return r
}

// evaluateQuotaReserve separates a measured threshold from missing evidence.
// Cached selection can defer unknown usage to dispatch; dispatch must not.
func evaluateQuotaReserve(r *objects.ChannelQuotaReserve, status *biz.QuotaChannelStatus, now time.Time) (blocked, known bool) {
	if status == nil {
		return false, false
	}
	fiveHourKnown, sevenDayKnown := r.FiveHourPercent == nil, r.SevenDayPercent == nil
	for _, limit := range status.Limits {
		// Named model-specific reserves and image buckets do not represent the
		// account's ordinary 5h/7d allowance. Never infer them from display names.
		if limit.Type != provider_quota.QuotaLimitTypeToken || limit.AvailabilityGroup != "" ||
			(limit.Status != "available" && limit.Status != "warning" && limit.Status != "exhausted") ||
			math.IsNaN(limit.UsageRatio) || math.IsInf(limit.UsageRatio, 0) || limit.UsageRatio < 0 {
			continue
		}
		if limit.NextResetAt != nil && !limit.NextResetAt.After(now) {
			continue
		}
		// PeriodStart is derived from reset time minus duration for display.
		// A fresh idle Codex window can start just after our local clock because
		// the provider clock is ahead. It must not invalidate measured usage.
		var percent *int
		switch limit.Window {
		case provider_quota.QuotaWindow5h:
			percent = r.FiveHourPercent
			fiveHourKnown = true
		case provider_quota.QuotaWindow7d:
			percent = r.SevenDayPercent
			sevenDayKnown = true
		}
		if percent != nil && *percent >= 1 && *percent <= 100 && limit.UsageRatio >= float64(*percent)/100 {
			return true, true
		}
	}
	return false, fiveHourKnown && sevenDayKnown
}

func checkQuotaReserveForDispatch(ctx context.Context, ch *biz.Channel, req *llm.Request, memory bool, provider ProviderQuotaStatusProvider) error {
	r := applicableQuotaReserve(ch, req, memory)
	if r == nil {
		return nil
	}
	checker, ok := provider.(ProviderQuotaReserveChecker)
	if !ok {
		return newQuotaReserveUnavailableError(req.Model)
	}
	status, err := checker.CheckQuotaReserve(ctx, ch.ID)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		if safe, ok := errors.AsType[*biz.QuotaReserveCheckError](err); ok {
			log.Warn(ctx, "Quota reserve usage could not be verified", log.Int("channel_id", ch.ID), log.String("reason", safe.Error()))
			return newQuotaReserveUnavailableError(req.Model, safe.Error())
		}
		log.Warn(ctx, "Quota reserve usage could not be verified", log.Int("channel_id", ch.ID))
		return newQuotaReserveUnavailableError(req.Model)
	}
	blocked, known := evaluateQuotaReserve(r, status, time.Now())
	if blocked {
		log.Info(ctx, "Quota reserve threshold reached", log.Int("channel_id", ch.ID), log.Any("limits", status.Limits))
		return NewQuotaReserveError(req.Model)
	}
	if !known {
		return newQuotaReserveUnavailableError(req.Model, "usage response did not contain current measurements for every configured window")
	}
	return nil
}

func (g *QuotaRoutingGate) reserveBlocks(ctx context.Context, candidate *ChannelModelsCandidate, req *llm.Request) bool {
	return quotaReserveBlocks(ctx, candidate.Channel, req, shared.GetCodexMemoryTask(ctx) != shared.CodexMemoryNone, g.provider, time.Now())
}
