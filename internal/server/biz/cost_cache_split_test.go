package biz

import (
	"testing"

	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/llm"
)

func TestCacheWriteCostPreservesUnlabelledTokens(t *testing.T) {
	price := []objects.ModelPriceItem{
		{ItemCode: objects.PriceItemCodeUsage, Pricing: objects.Pricing{Mode: objects.PricingModeUsagePerUnit, UsagePerUnit: lo.ToPtr(decimal.NewFromInt(1))}},
		{ItemCode: objects.PriceItemCodeWriteCachedTokens,
			Pricing: objects.Pricing{Mode: objects.PricingModeUsagePerUnit, UsagePerUnit: lo.ToPtr(decimal.NewFromInt(7))},
			PromptWriteCacheVariants: []objects.PromptWriteCacheVariant{
				{VariantCode: objects.PromptWriteCacheVariantCode5Min, Pricing: objects.Pricing{Mode: objects.PricingModeUsagePerUnit, UsagePerUnit: lo.ToPtr(decimal.NewFromInt(2))}},
				{VariantCode: objects.PromptWriteCacheVariantCode1Hour, Pricing: objects.Pricing{Mode: objects.PricingModeUsagePerUnit, UsagePerUnit: lo.ToPtr(decimal.NewFromInt(4))}},
			}},
	}
	for _, tt := range []struct {
		name                                  string
		total, five, hour, wantFive, wantHour int64
		wantCost                              string
	}{
		{"total only", 100, 0, 0, 100, 0, "0.0003"},
		{"partial split", 100, 20, 30, 70, 30, "0.00036"},
		{"complete split", 100, 70, 30, 70, 30, "0.00036"},
		{"split without total", 0, 70, 30, 70, 30, "0.00036"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			details := &llm.PromptTokensDetails{WriteCachedTokens: tt.total, WriteCached5MinTokens: tt.five, WriteCached1HourTokens: tt.hour}
			before := *details
			items, total := computeUsageCostWithItems(&llm.Usage{PromptTokens: 200, PromptTokensDetails: details}, price)
			var five, hour int64
			for _, item := range items {
				switch item.PromptWriteCacheVariantCode {
				case objects.PromptWriteCacheVariantCode5Min:
					five += item.Quantity
				case objects.PromptWriteCacheVariantCode1Hour:
					hour += item.Quantity
				}
			}
			require.Equal(t, tt.wantFive, five)
			require.Equal(t, tt.wantHour, hour)
			require.True(t, decimal.RequireFromString(tt.wantCost).Equal(total), total.String())
			require.Equal(t, before, *details, "billing must not rewrite upstream usage")
		})
	}
}
