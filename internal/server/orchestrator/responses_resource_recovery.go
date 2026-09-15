package orchestrator

import (
	"fmt"
	"regexp"
	"strconv"

	"github.com/tidwall/gjson"
)

var (
	responsesResourceMismatchMessagePattern = regexp.MustCompile(
		`^(?:OpenAI Responses bad request: )?The requested item was created under a different (?:Azure|\*\*\*) OpenAI resource\. Use the same resource that created the item to access it\.(?: \[trace_id=[A-Za-z0-9_-]+\])?$`,
	)
	responsesResourceItemIDParamPattern = regexp.MustCompile(`^input\[(\d+)\]\.id$`)
)

// A resource mismatch is distinct from a reasoning decryption rejection: a
// fully materialized message or tool call can still carry a stored item ID.
// Detach only those optional IDs first, preserving call_id and opaque reasoning.
func responsesRejectedResourceRule(body []byte, code, message, param string) (responsesRejectedStatusRule, bool) {
	if code != "" && code != "bad_request" && code != "invalid_request_error" {
		return responsesRejectedStatusRule{}, false
	}
	if !responsesResourceMismatchMessagePattern.MatchString(message) {
		return responsesRejectedStatusRule{}, false
	}
	hasIDs, safe := responsesResourceHistorySupportsRecovery(body)
	if !safe {
		return responsesRejectedStatusRule{}, false
	}
	if param != "" {
		match := responsesResourceItemIDParamPattern.FindStringSubmatch(param)
		if len(match) != 2 {
			return responsesRejectedStatusRule{}, false
		}
		index, err := strconv.Atoi(match[1])
		if err != nil {
			return responsesRejectedStatusRule{}, false
		}
		item := gjson.GetBytes(body, fmt.Sprintf("input.%d", index))
		if !responsesInputSupportsPortableID(item.Get("type").String()) || item.Get("id").String() == "" {
			return responsesRejectedStatusRule{}, false
		}
	}
	if hasIDs {
		return responsesRejectedStatusRule{index: -1, field: "id"}, true
	}
	// When no portable IDs remain, this explicit rejection permits the existing
	// complete-history reasoning recovery. Never infer this
	// from a 500, or discard native compaction to make a request pass validation.
	return responsesRejectedReasoningRule(body, "")
}

func responsesInputSupportsPortableID(itemType string) bool {
	switch itemType {
	case "", "message", "additional_tools", "function_call", "function_call_output", "custom_tool_call", "custom_tool_call_output":
		return true
	default:
		return false
	}
}

func responsesResourceHistorySupportsRecovery(body []byte) (hasIDs, safe bool) {
	if _, complete := responsesExplicitHistorySupportsRecovery(body); !complete {
		return false, false
	}
	for _, item := range gjson.GetBytes(body, "input").Array() {
		itemType := item.Get("type").String()
		switch itemType {
		case "", "message":
			content := item.Get("content")
			if item.Get("role").String() == "" || (!content.IsArray() && content.Type != gjson.String) {
				return false, false
			}
		case "function_call", "custom_tool_call":
			field := "arguments"
			if itemType == "custom_tool_call" {
				field = "input"
			}
			if item.Get("call_id").String() == "" || item.Get("name").String() == "" || item.Get(field).Type != gjson.String {
				return false, false
			}
		case "function_call_output", "custom_tool_call_output":
			if !item.Get("output").Exists() || item.Get("output").Type == gjson.Null {
				return false, false
			}
		case "additional_tools", "reasoning", "configuration_update", "compaction_trigger":
		default:
			return false, false
		}
		// File IDs and encrypted tool results have their own resource ownership;
		// this recovery cannot materialize them by removing an outer item ID.
		for _, field := range []string{"content", "output"} {
			for _, part := range item.Get(field).Array() {
				if part.Get("file_id").String() != "" || part.Get("encrypted_content").String() != "" ||
					part.Get("type").String() == "encrypted_content" || part.Get("type").String() == "item_reference" {
					return false, false
				}
			}
		}
		if responsesInputSupportsPortableID(itemType) {
			id := item.Get("id")
			if id.Exists() && id.Type != gjson.String && id.Type != gjson.Null {
				return false, false
			}
			hasIDs = hasIDs || id.String() != ""
		}
	}
	return hasIDs, true
}
