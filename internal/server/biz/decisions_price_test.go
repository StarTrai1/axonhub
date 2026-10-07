package biz

import (
	"context"
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/llm"
)

func TestDecisionsPricingIsolation(t *testing.T) {
	usage := &llm.Usage{PromptTokens: 1_000_000, CompletionTokens: 50, PromptTokensDetails: &llm.PromptTokensDetails{CachedTokens: 300_000, WriteCachedTokens: 100_000}}
	price := objects.ModelPrice{Items: []objects.ModelPriceItem{
		{ItemCode: objects.PriceItemCodeUsage, Pricing: objects.Pricing{Mode: objects.PricingModeUsagePerUnit, UsagePerUnit: lo.ToPtr(decimal.NewFromInt(2))}},
		{ItemCode: objects.PriceItemCodeCompletion, Pricing: objects.Pricing{Mode: objects.PricingModeFlatFee, FlatFee: lo.ToPtr(decimal.NewFromInt(3))}},
		{ItemCode: objects.PriceItemCodePromptCachedToken, Pricing: objects.Pricing{Mode: objects.PricingModeFlatFee, FlatFee: lo.ToPtr(decimal.NewFromInt(4))}},
		{ItemCode: objects.PriceItemCodeWriteCachedTokens, Pricing: objects.Pricing{Mode: objects.PricingModeFlatFee, FlatFee: lo.ToPtr(decimal.NewFromInt(5))}},
		{ItemCode: objects.PriceItemCodeDecisionsInputTokens, Pricing: objects.Pricing{Mode: objects.PricingModeUsagePerUnit, UsagePerUnit: lo.ToPtr(decimal.RequireFromString("0.10"))}},
	}}
	items, total := computeUsageCostForFormat(usage, price, time.Now(), llm.APIFormatOpenAIDecisions)
	require.Len(t, items, 1)
	require.Equal(t, int64(1_000_000), items[0].Quantity)
	require.True(t, total.Equal(decimal.RequireFromString("0.10")))
	items, total = ComputeUsageCost(usage, price, time.Now())
	require.Len(t, items, 4)
	require.True(t, total.Equal(decimal.RequireFromString("13.2")))
	// Flat Decisions fees must not leak into ordinary requests either.
	price.Items[4].Pricing = objects.Pricing{Mode: objects.PricingModeFlatFee, FlatFee: lo.ToPtr(decimal.NewFromInt(99))}
	_, total = ComputeUsageCost(usage, price, time.Now())
	require.True(t, total.Equal(decimal.RequireFromString("13.2")))
}

func TestDecisionsPricingVolumeAndSchedule(t *testing.T) {
	price := objects.ModelPrice{Items: []objects.ModelPriceItem{{
		ItemCode: objects.PriceItemCodeDecisionsInputTokens,
		Pricing: objects.Pricing{Mode: objects.PricingModeVolume, UsageTiered: &objects.TieredPricing{Tiers: []objects.PriceTier{
			{UpTo: lo.ToPtr(int64(272_000)), PricePerUnit: decimal.RequireFromString("0.10")},
			{PricePerUnit: decimal.RequireFromString("0.20")},
		}}},
	}}}
	for _, count := range []int64{272_000, 272_001} {
		_, total := computeUsageCostForFormat(&llm.Usage{PromptTokens: count}, price, time.Now(), llm.APIFormatOpenAIDecisions)
		rate := decimal.RequireFromString("0.10")
		if count > 272_000 {
			rate = decimal.RequireFromString("0.20")
		}
		require.True(t, total.Equal(rate.Mul(decimal.NewFromInt(count)).Div(decimal.NewFromInt(1_000_000))))
	}
	price.Schedule = &objects.PriceSchedule{Timezone: "UTC", Overrides: []objects.PriceOverride{{
		When: objects.OverrideWhen{}, Items: []objects.ModelPriceItem{{ItemCode: objects.PriceItemCodeDecisionsInputTokens, Pricing: objects.Pricing{Mode: objects.PricingModeUsagePerUnit, UsagePerUnit: lo.ToPtr(decimal.RequireFromString("0.11"))}}},
	}}}
	_, total := computeUsageCostForFormat(&llm.Usage{PromptTokens: 1_000_000}, price, time.Now(), llm.APIFormatOpenAIDecisions)
	require.True(t, total.Equal(decimal.RequireFromString("0.11")))
}

func TestDecisionsMissingPriceIsUnknown(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()
	ctx := authz.WithTestBypass(context.Background())
	ch := &Channel{Channel: &ent.Channel{ID: 42}, cachedModelPrices: map[string]*ent.ChannelModelPrice{
		"gpt-6-luna": {ReferenceID: "chat-only", Price: objects.ModelPrice{Items: []objects.ModelPriceItem{{ItemCode: objects.PriceItemCodeUsage, Pricing: objects.Pricing{Mode: objects.PricingModeFlatFee, FlatFee: lo.ToPtr(decimal.NewFromInt(1))}}}}},
	}}
	channels := NewChannelServiceForTest(client)
	channels.SetEnabledChannelsForTest([]*Channel{ch})
	svc := NewUsageLogService(client, nil, channels)
	usage := &llm.Usage{PromptTokens: 42, Cost: lo.ToPtr(99.0)}
	items, total, reference := svc.computeUsageCost(ctx, 42, "gpt-6-luna", usage, llm.APIFormatOpenAIDecisions)
	require.Empty(t, items)
	require.Nil(t, total)
	require.Empty(t, reference)
	svc.InjectUsageCost(ctx, 42, "gpt-6-luna", usage, llm.APIFormatOpenAIDecisions)
	require.Nil(t, usage.Cost)
	svc.InjectUsageCost(ctx, 42, "gpt-6-luna", usage, llm.APIFormatOpenAIChatCompletion)
	require.NotNil(t, usage.Cost)
	require.Equal(t, 1.0, *usage.Cost)
}
