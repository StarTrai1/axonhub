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
func normalizeGPT6ConfigurationUpdates(model string, body []byte) ([]byte, error) {
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
	return body, nil
}
