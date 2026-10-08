package claudecode

import (
	"fmt"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Only the system anchor inserted by this transformer may be changed here.
// A later 1h anchor already covers this prefix; inserting a 5m anchor before it
// would make an otherwise valid mixed-TTL request fail upstream validation.
func alignInjectedSystemCacheTTL(body []byte) ([]byte, error) {
	root := gjson.ParseBytes(body)
	system := root.Get("system").Array()
	if len(system) == 0 || system[0].Get("text").String() != claudeCodeSystemMessage || system[0].Get("cache_control.type").String() != "ephemeral" {
		return body, nil
	}
	hasLaterHour := oneHourControl(root.Get("cache_control"))
	for _, part := range system[1:] {
		hasLaterHour = hasLaterHour || oneHourControl(part.Get("cache_control"))
	}
	root.Get("messages").ForEach(func(_, message gjson.Result) bool {
		hasLaterHour = hasLaterHour || contentHasOneHourControl(message.Get("content"))
		return !hasLaterHour
	})
	if !hasLaterHour {
		return body, nil
	}
	updated, err := sjson.SetBytes(body, "system.0.cache_control.ttl", "1h")
	if err != nil {
		return nil, fmt.Errorf("align injected Claude system cache TTL: %w", err)
	}
	return updated, nil
}

func oneHourControl(control gjson.Result) bool {
	return control.Get("type").String() == "ephemeral" && control.Get("ttl").String() == "1h"
}

func contentHasOneHourControl(content gjson.Result) bool {
	if !content.IsArray() {
		return false
	}
	found := false
	content.ForEach(func(_, block gjson.Result) bool {
		found = oneHourControl(block.Get("cache_control")) || contentHasOneHourControl(block.Get("content"))
		return !found
	})
	return found
}
