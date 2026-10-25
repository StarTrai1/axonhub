package biz

import (
	"encoding/json"
	"testing"

	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/llm/transformer/openai"
)

func TestCacheCreationInputAliasBilledOnce(t *testing.T) {
	var wire openai.Usage
	require.NoError(t, json.Unmarshal([]byte(`{"prompt_tokens":1000,"completion_tokens":0,"total_tokens":1000,"prompt_tokens_details":{"cached_tokens":300,"cache_creation_input_tokens":200}}`), &wire))
	price := []objects.ModelPriceItem{
		{ItemCode: objects.PriceItemCodeUsage, Pricing: objects.Pricing{Mode: objects.PricingModeUsagePerUnit, UsagePerUnit: lo.ToPtr(decimal.NewFromInt(1))}},
		{ItemCode: objects.PriceItemCodePromptCachedToken, Pricing: objects.Pricing{Mode: objects.PricingModeUsagePerUnit, UsagePerUnit: lo.ToPtr(decimal.NewFromInt(2))}},
		{ItemCode: objects.PriceItemCodeWriteCachedTokens, Pricing: objects.Pricing{Mode: objects.PricingModeUsagePerUnit, UsagePerUnit: lo.ToPtr(decimal.NewFromInt(3))}},
	}
	items, total := computeUsageCostWithItems(wire.ToLLMUsage(), price)
	quantities := make(map[objects.PriceItemCode]int64)
	for _, item := range items { quantities[item.ItemCode] += item.Quantity }
	require.Equal(t, int64(500), quantities[objects.PriceItemCodeUsage])
	require.Equal(t, int64(300), quantities[objects.PriceItemCodePromptCachedToken])
	require.Equal(t, int64(200), quantities[objects.PriceItemCodeWriteCachedTokens])
	require.True(t, decimal.RequireFromString("0.0017").Equal(total), total.String())
}
