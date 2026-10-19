package orchestrator

import (
	"encoding/json"
	"strings"

	"github.com/tidwall/gjson"
)

// Count text JSON bytes conservatively; image transport bytes are not text
// tokens. Keep the original image unchanged and reserve a large fixed visual
// allowance. Unknown file/audio/encrypted inputs retain the compatibility path.
const unifiedImageAllowance = 16 << 10

func unifiedItemSize(item json.RawMessage) int {
	size := len(item)
	for _, field := range []string{"content", "output"} {
		for _, part := range gjson.GetBytes(item, field).Array() {
			if part.Get("type").String() == "input_image" {
				url := part.Get("image_url")
				if url.Type == gjson.String {
					size += unifiedImageAllowance - len(url.Raw)
				}
			}
		}
	}
	return size + 1
}

func unifiedItemsSize(items []json.RawMessage) int {
	size := 2
	for _, item := range items {
		size += unifiedItemSize(item)
	}
	return size
}

func unifiedContextSize(body []byte) int {
	size := len(body)
	for _, item := range gjson.GetBytes(body, "input").Array() {
		size += unifiedItemSize(json.RawMessage(item.Raw)) - len(item.Raw) - 1
	}
	return size
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

// Large coding turns split only between completed tool interactions. A call
// and its output, including an intervening user correction, are indivisible.
func unifiedHistoryUnits(items []json.RawMessage) [][]json.RawMessage {
	var units [][]json.RawMessage
	pending := make(map[string]bool)
	start := 0
	for i, item := range items {
		value := gjson.ParseBytes(item)
		switch value.Get("type").String() {
		case "function_call", "custom_tool_call":
			if id := value.Get("call_id").String(); id != "" {
				pending[id] = true
			}
		case "function_call_output", "custom_tool_call_output":
			delete(pending, value.Get("call_id").String())
		}
		if len(pending) == 0 {
			units = append(units, items[start:i+1])
			start = i + 1
		}
	}
	if start < len(items) {
		units = append(units, items[start:])
	}
	return units
}
