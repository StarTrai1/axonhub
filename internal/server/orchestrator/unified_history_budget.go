package orchestrator

import (
	"encoding/json"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func unifiedPortableIdentity(raw json.RawMessage) (json.RawMessage, error) {
	if rawInputItemType(raw) == "agent_message" {
		return raw, nil // Agent delivery identity is not an upstream hydration ID.
	}
	var err error
	for _, field := range []string{"id", "status", "internal_chat_message_metadata_passthrough"} {
		if gjson.GetBytes(raw, field).Exists() {
			raw, err = sjson.DeleteBytes(raw, field)
			if err != nil {
				return nil, err
			}
		}
	}
	return raw, nil
}

func unifiedPortableItem(item json.RawMessage) bool {
	if exactReplayTextItem(item) {
		return true
	}
	value := gjson.ParseBytes(item)
	if value.Get("encrypted_content").String() != "" || value.Get("encrypted_function_args").Exists() {
		return false
	}
	if typ := value.Get("type").String(); typ != "message" && typ != "" && typ != "function_call_output" && typ != "custom_tool_call_output" {
		return false
	}
	for _, field := range []string{"content", "output"} {
		parts := value.Get(field)
		if !parts.Exists() || parts.Type == gjson.String || parts.Type == gjson.Null {
			continue
		}
		if !parts.IsArray() {
			return false
		}
		for _, part := range parts.Array() {
			if part.Get("file_id").Exists() || part.Get("encrypted_content").Exists() || part.Get("encrypted_function_args").Exists() {
				return false
			}
			switch part.Get("type").String() {
			case "input_text", "output_text", "text", "reasoning_text":
			case "input_image":
				url := part.Get("image_url").String()
				if !strings.HasPrefix(url, "data:image/") && !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://") {
					return false
				}
			default:
				return false
			}
		}
	}
	return true
}
