package orchestrator

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/internal/server/biz"
)

// Preserve the existing assembly target. A matching catalog contract can prove
// that a larger input still leaves the full model output allowance available.
// Token admission is independent of archive and segment byte limits.
func unifiedHistoryInputBudgets(body []byte, candidate *ChannelModelsCandidate, modelIndex, contextTokens int) (int, int) {
	target := min(unifiedContextMaxBytes, contextTokens-contextTokens/4)
	ceiling := target
	if modelIndex >= 0 && modelIndex < len(candidate.Models) {
		model := candidate.Models[modelIndex].ActualModel
		known := 0
		if gjson.GetBytes(body, "model").String() == model {
			known = biz.BundledModelHistoryInputLimit(model, contextTokens)
		}
		if known > 0 {
			ceiling = min(unifiedContextMaxBytes, max(0, known-4096))
			target = min(target, ceiling)
		}
	}
	output := max(candidate.DefaultMaxTokens, gjson.GetBytes(body, "max_output_tokens").Int())
	if output > 0 {
		remaining := max(int64(0), int64(contextTokens)-output-4096)
		ceiling = int(min(int64(ceiling), remaining))
		target = min(target, ceiling)
	}
	return target, ceiling
}

func unifiedSegmentSummaryMessage(segment unifiedHistorySegment) json.RawMessage {
	message, _ := json.Marshal(map[string]any{"type": "message", "role": "assistant", "content": []map[string]string{{"type": "output_text", "text": fmt.Sprintf("Archived history summary (source %s). This is a derived summary; the original user instructions remain authoritative.\n%s", strings.TrimPrefix(segment.key, "summary:"), segment.summary)}}})
	return message
}

// Validate cached notes before paying to prepare any missing siblings. The
// uncached allowance is a planning reserve; final generated text is checked
// again without truncation before a provider request can start.
func validateUnifiedHistoryMinimum(plan *unifiedHistoryPlan, budget int) error {
	size := plan.mandatory
	for _, segment := range plan.segments {
		if segment.key == "" {
			continue
		}
		if segment.summary == "" {
			size += 1024
		} else {
			size += plan.measure.item(unifiedSegmentSummaryMessage(segment))
		}
	}
	if size > budget {
		return unifiedHistoryPreparationLimit("the retained instructions and cached segment summaries already exceed the input budget; no additional summaries were generated")
	}
	return nil
}

// Count text JSON bytes conservatively; image transport bytes are not text
// tokens. Keep the original image unchanged and reserve a large fixed visual
// allowance. Unknown file/audio/encrypted inputs retain the compatibility path.
const unifiedImageAllowance = 16 << 10

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
	measure := unifiedModelMeasure(gjson.GetBytes(body, "model").String())
	envelope, err := sjson.DeleteBytes(body, "input")
	if err != nil {
		return len(body)
	}
	size := measure(envelope) + 32
	for _, item := range gjson.GetBytes(body, "input").Array() {
		size += measure.item(json.RawMessage(item.Raw))
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
