package biz

import (
	"encoding/json"
	"sync"
)

// The bundled catalog is also available to installations using direct channel
// routing, without importing admin model rows or fetching a catalog mid-request.
type bundledHistoryLimits struct {
	context int
	input   int
}

var bundledContextLimits = sync.OnceValue(func() map[string]bundledHistoryLimits {
	var file catalogFile
	limits := make(map[string]bundledHistoryLimits)
	if json.Unmarshal(catalogFallbackJSON, &file) != nil {
		return limits
	}
	for _, provider := range file.Providers {
		for _, model := range provider.Models {
			id, _ := model["id"].(string)
			limit, _ := model["limit"].(map[string]any)
			context, _ := limit["context"].(float64)
			if id == "" || context <= 0 || context > 100_000_000 {
				continue
			}
			input, _ := limit["input"].(float64)
			output, _ := limit["output"].(float64)
			entry := bundledHistoryLimits{context: int(context)}
			if input > 0 && input <= context && output > 0 && output < context {
				entry.input = min(int(input), int(context)-int(output))
			}
			if prior, found := limits[id]; found {
				entry.context = min(entry.context, prior.context)
				// An incomplete catalog entry cannot authorize a larger budget.
				entry.input = min(entry.input, prior.input)
			}
			limits[id] = entry
		}
	}
	return limits
})

func BundledModelContextLimit(model string) int {
	return bundledContextLimits()[model].context
}

// BundledModelHistoryInputLimit reserves the model's full output allowance and
// respects its separate input limit. A different configured window is not
// evidence that this catalog's input/output contract applies to that route.
func BundledModelHistoryInputLimit(model string, contextTokens int) int {
	limits := bundledContextLimits()[model]
	if limits.context != contextTokens {
		return 0
	}
	return limits.input
}
