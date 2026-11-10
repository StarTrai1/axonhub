package orchestrator

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/ent/providerquotastatus"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/internal/server/biz/provider_quota"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

func TestQuotaReserveWindowEvidence(t *testing.T) {
	now := time.Now()
	candidate := quotaRoutingCandidate(1, objects.QuotaRoutingModeIgnoreQuota)
	candidate.Channel.Type = channel.TypeCodex
	candidate.Channel.Settings.QuotaReserve = &objects.ChannelQuotaReserve{FiveHourPercent: lo.ToPtr(90), SevenDayPercent: lo.ToPtr(80)}
	for _, tc := range []struct {
		name          string
		window        string
		ratio         float64
		reset         time.Time
		status, group string
		blocked       bool
	}{
		{"below", provider_quota.QuotaWindow5h, .8999, now.Add(time.Hour), "available", "", false},
		{"at 5h", provider_quota.QuotaWindow5h, .90, now.Add(time.Hour), "warning", "", true},
		{"at 7d", provider_quota.QuotaWindow7d, .80, now.Add(time.Hour), "warning", "", true},
		{"reset", provider_quota.QuotaWindow5h, .99, now, "warning", "", false},
		{"unknown", provider_quota.QuotaWindow5h, .99, now.Add(time.Hour), "unknown", "", false},
		{"invalid", provider_quota.QuotaWindow5h, math.NaN(), now.Add(time.Hour), "warning", "", false},
		{"image model bucket", provider_quota.QuotaWindow5h, .99, now.Add(time.Hour), "warning", "images", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status := &biz.QuotaChannelStatus{Limits: []provider_quota.QuotaLimitStatus{{Type: provider_quota.QuotaLimitTypeToken, Window: tc.window, UsageRatio: tc.ratio, NextResetAt: &tc.reset, Status: tc.status, AvailabilityGroup: tc.group}}}
			provider := &mockQuotaStatusProvider{statuses: map[int]*biz.QuotaChannelStatus{1: status}}
			require.Equal(t, tc.blocked, quotaReserveBlocks(t.Context(), candidate.Channel, &llm.Request{}, false, provider, now))
		})
	}
	require.False(t, quotaReserveBlocks(t.Context(), candidate.Channel, &llm.Request{}, false, &mockQuotaStatusProvider{}, now))
}

func TestQuotaReserveMeasuredUsageDoesNotDependOnInferredWindowStart(t *testing.T) {
	// Read-only production observation: HTTP 200, allowed=true, 0% / 38%.
	// The provider's idle 5h reset implies a start 0.505s after local receipt.
	now := time.Unix(1794078724, 495434500)
	reset := time.Unix(1794096725, 0)
	weeklyReset := time.Unix(1794499138, 0)
	reserve := &objects.ChannelQuotaReserve{FiveHourPercent: lo.ToPtr(95), SevenDayPercent: lo.ToPtr(98)}
	for _, tc := range []struct {
		name       string
		five, week float64
		blocked    bool
	}{
		{name: "observed idle window", five: 0, week: .38},
		{name: "five-hour threshold still enforced", five: .95, week: .38, blocked: true},
		{name: "weekly threshold still enforced", five: 0, week: .98, blocked: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			limits := []provider_quota.QuotaLimitStatus{
				provider_quota.NewTokenLimitStatus("available", tc.five, &reset).WithWindow(provider_quota.QuotaWindow5h, 5*time.Hour),
				provider_quota.NewTokenLimitStatus("available", tc.week, &weeklyReset).WithWindow(provider_quota.QuotaWindow7d, 7*24*time.Hour),
			}
			require.True(t, limits[0].PeriodStart.After(now), "retain provider timing for display")
			blocked, known := evaluateQuotaReserve(reserve, &biz.QuotaChannelStatus{Limits: limits}, now)
			require.True(t, known, "measured current usage must not be discarded because of inferred window start")
			require.Equal(t, tc.blocked, blocked)
		})
	}
}

func TestQuotaReserveToolsDoNotGrantCapabilitiesOrBypassHardQuota(t *testing.T) {
	candidate := quotaRoutingCandidate(1, objects.QuotaRoutingModeRemoveOnExhausted)
	candidate.Channel.Type = channel.TypeCodex
	reserve := &objects.ChannelQuotaReserve{FiveHourPercent: lo.ToPtr(90), AllowSearch: true, AllowImage: true, AllowMemory: true}
	candidate.Channel.Settings.QuotaReserve = reserve
	status := quotaStickyOnlyStatus()
	provider := &mockQuotaStatusProvider{statuses: map[int]*biz.QuotaChannelStatus{1: status}}
	gate := NewQuotaRoutingGate(provider, biz.QuotaRoutingSettings{DefaultMode: objects.QuotaRoutingModeRemoveOnExhausted})
	filter := func(ctx context.Context, candidates []*ChannelModelsCandidate, req *llm.Request, stickyID int) []*ChannelModelsCandidate {
		preferred, fallback := gate.Filter(ctx, candidates, req, stickyID)
		return append(preferred, fallback...)
	}
	chat := &llm.Request{RequestType: llm.RequestTypeChat}
	search := &llm.Request{RequestType: llm.RequestTypeSearch, APIFormat: llm.APIFormatOpenAISearch}
	image := &llm.Request{RequestType: llm.RequestTypeImage, APIFormat: llm.APIFormatOpenAIImageGeneration, Image: &llm.ImageRequest{}}
	for _, mode := range []objects.QuotaRoutingMode{objects.QuotaRoutingModeRemoveOnExhausted, objects.QuotaRoutingModeBackpressure, objects.QuotaRoutingModeIgnoreQuota} {
		candidate.Channel.Settings.QuotaRoutingMode = mode
		// Explicit limits also apply to pinned/phase-two and ignore-quota candidates.
		require.Empty(t, filter(t.Context(), []*ChannelModelsCandidate{candidate}, chat, 1))
	}
	candidate.Channel.Settings.QuotaRoutingMode = objects.QuotaRoutingModeRemoveOnExhausted
	for _, req := range []*llm.Request{search, image} {
		require.Len(t, filter(t.Context(), []*ChannelModelsCandidate{candidate}, req, 0), 1)
	}
	memoryCtx := shared.WithCodexMemoryTask(t.Context(), shared.CodexMemoryExtract)
	require.Len(t, filter(memoryCtx, []*ChannelModelsCandidate{candidate}, chat, 0), 1)
	candidate.Channel.Policies.CodexMemory = objects.CodexMemoryExclude
	require.False(t, candidate.Channel.AllowsWorkload(chat, true), "quota exception never grants excluded memory")
	candidate.Channel.Policies.ImageGeneration = objects.ImageGenerationOtherChannels
	require.False(t, candidate.Channel.AllowsWorkload(image, false), "quota exception never grants excluded images")
	reserve.AllowSearch, reserve.AllowImage, reserve.AllowMemory = false, false, false
	for _, req := range []*llm.Request{chat, search, image} {
		require.Empty(t, filter(t.Context(), []*ChannelModelsCandidate{candidate}, req, 0))
	}
	reserve.AllowMemory = true
	status.Status = providerquotastatus.StatusExhausted
	status.Limits[0].Status, status.Limits[0].UsageRatio = "exhausted", 1
	require.Empty(t, filter(memoryCtx, []*ChannelModelsCandidate{candidate}, chat, 0))
	status.Limits[0].NextResetAt = lo.ToPtr(time.Now().Add(-time.Second))
	status.Status = providerquotastatus.StatusAvailable
	// A reserve's reset must not override an independently exhausted quota.
	require.Empty(t, filter(t.Context(), []*ChannelModelsCandidate{candidate}, chat, 0))
	status.Limits[0].Status, status.Limits[0].UsageRatio = "warning", .9
	require.Len(t, filter(t.Context(), []*ChannelModelsCandidate{candidate}, chat, 0), 1, "reset automatically reopens ordinary routing")
}

func TestQuotaReserveFinalDispatchAndRetryRefresh(t *testing.T) {
	ctx, client := setupTest(t)
	entity := createRetryGuardChannel(t, ctx, client, "quota-reserve-refresh", objects.ChannelCredentials{APIKey: "synthetic"})
	_, err := client.Channel.UpdateOneID(entity.ID).SetType(channel.TypeCodex).SetSettings(&objects.ChannelSettings{QuotaReserve: &objects.ChannelQuotaReserve{FiveHourPercent: lo.ToPtr(90)}}).Save(ctx)
	require.NoError(t, err)
	svc := newTestChannelServiceForChannels(client)
	snapshot := retryGuardSnapshot(t, ctx, svc, entity.ID)
	p := newRetryGuardTransformer(svc, snapshot, "synthetic")
	p.state.quotaReserveProvider = &mockQuotaStatusProvider{statuses: map[int]*biz.QuotaChannelStatus{entity.ID: quotaStickyOnlyStatus()}}
	p.state.LlmRequest = &llm.Request{Model: "synthetic-model"}
	require.ErrorIs(t, p.refreshChannelBeforeRetry(ctx), errChannelUnavailableForRetry)
	p.state.ChannelModelsCandidates = []*ChannelModelsCandidate{p.state.CurrentCandidate}
	_, err = p.TransformRequest(ctx, p.state.LlmRequest)
	var exhausted *llm.ResponseError
	require.ErrorAs(t, err, &exhausted, "final dispatch cannot bypass a threshold reached after selection")
	require.Equal(t, "quota_reserve_reached", exhausted.Detail.Code)
}
