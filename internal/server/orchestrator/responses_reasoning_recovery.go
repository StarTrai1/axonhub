package orchestrator

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
)

var responsesRejectedReasoningParamPattern = regexp.MustCompile(`^input\[(\d+)\]\.encrypted_content$`)

func responsesRejectedReasoningRule(body []byte, param string) (responsesRejectedStatusRule, bool) {
	if !gjson.ValidBytes(body) || gjson.GetBytes(body, "previous_response_id").String() != "" {
		return responsesRejectedStatusRule{}, false
	}
	input := gjson.GetBytes(body, "input")
	if !input.IsArray() {
		return responsesRejectedStatusRule{}, false
	}
	if param != "" {
		match := responsesRejectedReasoningParamPattern.FindStringSubmatch(param)
		if len(match) != 2 {
			return responsesRejectedStatusRule{}, false
		}
		index, err := strconv.Atoi(match[1])
		if err != nil {
			return responsesRejectedStatusRule{}, false
		}
		item := gjson.GetBytes(body, fmt.Sprintf("input.%d", index))
		if item.Get("type").String() != "reasoning" || item.Get("encrypted_content").Type != gjson.String ||
			item.Get("encrypted_content").String() == "" {
			return responsesRejectedStatusRule{}, false
		}
	}

	hasUserHistory := false
	hasEncryptedReasoning := false
	toolCalls := make(map[string]string)
	for _, item := range input.Array() {
		if !item.IsObject() {
			return responsesRejectedStatusRule{}, false
		}
		itemType := item.Get("type").String()
		if itemType != "reasoning" && item.Get("encrypted_content").String() != "" {
			return responsesRejectedStatusRule{}, false
		}
		if item.Get("encrypted_function_args").Exists() {
			return responsesRejectedStatusRule{}, false
		}
		switch itemType {
		case "additional_tools":
			if !item.Get("tools").IsArray() {
				return responsesRejectedStatusRule{}, false
			}
		case "configuration_update":
		case "message", "":
			content := item.Get("content")
			if item.Get("role").String() == "user" &&
				((content.Type == gjson.String && content.String() != "") || (content.IsArray() && len(content.Array()) > 0)) {
				hasUserHistory = true
			}
		case "reasoning":
			encrypted := item.Get("encrypted_content")
			if encrypted.Exists() && encrypted.Type != gjson.String && encrypted.Type != gjson.Null {
				return responsesRejectedStatusRule{}, false
			}
			hasEncryptedReasoning = hasEncryptedReasoning || encrypted.String() != ""
		case "function_call", "custom_tool_call":
			if callID := item.Get("call_id").String(); callID != "" {
				toolCalls[callID] = itemType
			}
		case "function_call_output", "custom_tool_call_output":
			if itemType == "function_call_output" && item.Get("call_id").String() == "" &&
				strings.TrimSpace(item.Get("name").String()) != "" && item.Get("output").Exists() {
				continue
			}
			if toolCalls[item.Get("call_id").String()] != strings.TrimSuffix(itemType, "_output") {
				return responsesRejectedStatusRule{}, false
			}
		case "agent_message":
			for _, content := range item.Get("content").Array() {
				if content.Get("type").String() == "encrypted_content" {
					return responsesRejectedStatusRule{}, false
				}
			}
		case "compaction", "compaction_summary", "context_compaction", "item_reference":
			return responsesRejectedStatusRule{}, false
		default:
			return responsesRejectedStatusRule{}, false
		}
	}
	if !hasUserHistory || !hasEncryptedReasoning {
		return responsesRejectedStatusRule{}, false
	}
	return responsesRejectedStatusRule{itemType: "reasoning", index: -1, field: "encrypted_content", dropItem: true}, true
}

func recoverResponsesReasoningSummary(item []byte) ([]byte, error) {
	type textPart struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	var reasoning struct {
		Summary []textPart `json:"summary"`
		Content []textPart `json:"content"`
	}
	if err := json.Unmarshal(item, &reasoning); err != nil {
		return nil, fmt.Errorf("decode rejected reasoning summary: %w", err)
	}
	var paragraphs []string
	for _, summary := range reasoning.Summary {
		if summary.Type != "summary_text" && summary.Type != "" {
			return nil, fmt.Errorf("cannot recover reasoning summary type %q", summary.Type)
		}
		if summary.Text != "" {
			paragraphs = append(paragraphs, summary.Text)
		}
	}
	for _, content := range reasoning.Content {
		if content.Type != "reasoning_text" {
			return nil, fmt.Errorf("cannot recover reasoning content type %q", content.Type)
		}
		if content.Text != "" {
			paragraphs = append(paragraphs, content.Text)
		}
	}
	if len(paragraphs) == 0 {
		return nil, nil
	}
	return json.Marshal(map[string]any{
		"type": "message",
		"role": "assistant",
		"content": []map[string]string{{
			"type": "output_text",
			"text": strings.Join(paragraphs, "\n\n"),
		}},
	})
}
