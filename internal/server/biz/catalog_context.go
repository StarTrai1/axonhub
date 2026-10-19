package biz

import (
	"encoding/json"
	"sync"
)

// The bundled catalog is also available to installations using direct channel
// routing, without importing admin model rows or fetching a catalog mid-request.
var bundledContextLimits = sync.OnceValue(func() map[string]int {
	var file catalogFile
	limits := make(map[string]int)
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
			if prior := limits[id]; prior == 0 || int(context) < prior {
				limits[id] = int(context)
			}
		}
	}
	return limits
})

func BundledModelContextLimit(model string) int {
	return bundledContextLimits()[model]
}
