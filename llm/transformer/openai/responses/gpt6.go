package responses

import (
	"strconv"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/llm/transformer/shared"
)

// Old conversations can contain effort updates that their new model no longer
// accepts. Keep item order and unknown fields while applying the same effort
// migration as the request-level setting after raw control items are merged.
func normalizeGPT6ResponsesBody(model string, body, sourceBody []byte) ([]byte, error) {
	if !shared.IsGPT6Model(model) {
		return body, nil
	}
	for index, item := range gjson.GetBytes(body, "input").Array() {
		if item.Get("type").String() != "configuration_update" {
			continue
		}
		effort := item.Get("reasoning.effort")
		if effort.Type != gjson.String {
			continue
		}
		normalized := shared.NormalizeGPT6Effort(model, effort.String())
		if normalized == effort.String() {
			continue
		}
		updated, err := sjson.SetBytes(body, "input."+strconv.Itoa(index)+".reasoning.effort", normalized)
		if err != nil {
			return nil, err
		}
		body = updated
	}
	if shared.IsGPT62Model(model) {
		return migrateGPT62CacheRetention(body, sourceBody)
	}
	return body, nil
}

func migrateGPT62CacheRetention(body, sourceBody []byte) ([]byte, error) {
	const field = "prompt_cache_retention"

	retention := gjson.GetBytes(body, field)
	if retention.Exists() {
		updated, err := sjson.DeleteBytes(body, field)
		if err != nil {
			return nil, err
		}
		body = updated
	} else {
		retention = gjson.GetBytes(sourceBody, field)
	}
	if retention.String() != "" && gjson.GetBytes(body, "prompt_cache_options.ttl").String() == "" {
		return sjson.SetBytes(body, "prompt_cache_options.ttl", "30m")
	}
	return body, nil
}
