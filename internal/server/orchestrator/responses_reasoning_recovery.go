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

// Some compatible relays wrap the upstream error without preserving its code.
// Require the complete rejection and an exact item in this request, rather than
// treating arbitrary mentions of encryption as permission to rebuild history.
var responsesRejectedReasoningMessagePattern = regexp.MustCompile(
	`^(?:OpenAI Responses bad request: )?The encrypted content for item (rs_[A-Za-z0-9_-]+) could not be verified\. Reason: Encrypted content could not be decrypted or parsed\.(?: \[trace_id=[A-Za-z0-9_-]+\])?$`,
)

// Relays may rename the code for both reasoning and compaction rejections.
// Callers must still match the complete message and the exact encrypted item;
// a generic thinking signature error alone does not authorize history changes.
func responsesEncryptedContentRejectionCode(code string) bool {
	switch code {
	case "", "bad_request", "invalid_request_error", "invalid_encrypted_content", "thinking_signature_invalid":
		return true
	default:
		return false
	}
}

func responsesRejectedReasoningMessageRule(body []byte, code, message, param string) (responsesRejectedStatusRule, bool) {
	if !responsesEncryptedContentRejectionCode(code) {
		return responsesRejectedStatusRule{}, false
	}
	match := responsesRejectedReasoningMessagePattern.FindStringSubmatch(message)
	if len(match) != 2 {
		return responsesRejectedStatusRule{}, false
	}
	index := -1
	for i, item := range gjson.GetBytes(body, "input").Array() {
		if item.Get("id").String() != match[1] {
			continue
		}
		if index >= 0 || item.Get("type").String() != "reasoning" {
			return responsesRejectedStatusRule{}, false
		}
		index = i
	}
	if index < 0 {
		return responsesRejectedStatusRule{}, false
	}
	itemParam := fmt.Sprintf("input[%d].encrypted_content", index)
	// Codex WebSocket errors can identify the whole input parameter while the
	// complete message still names one unique reasoning item. An indexed param
	// must agree with that item; never fall back to another indexed item.
	if param != "" && param != "input" && param != itemParam {
		return responsesRejectedStatusRule{}, false
	}
	return responsesRejectedReasoningRule(body, itemParam)
}

func responsesRejectedReasoningRule(body []byte, param string) (responsesRejectedStatusRule, bool) {
	// An indexed rejection identifies reasoning, not the checkpoint. Unindexed
	// generic encryption errors must keep the stricter complete-history guard.
	hasEncryptedReasoning, complete := responsesReasoningHistorySupportsRecoveryWithAgents(body, param != "", param != "")
	if !complete || !hasEncryptedReasoning {
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
	return responsesRejectedStatusRule{itemType: "reasoning", index: -1, field: "encrypted_content", dropItem: true, preserveCompaction: param != "", preserveAgentMessages: param != ""}, true
}

// A native checkpoint can remain verbatim while repairing rejected reasoning
// outside it. This does not claim the checkpoint is decryptable on the target;
// a subsequent checkpoint rejection uses the separate retained-source recovery.
func responsesReasoningHistorySupportsRecovery(body []byte, preserveCompaction bool) (bool, bool) {
	return responsesReasoningHistorySupportsRecoveryWithAgents(body, preserveCompaction, false)
}

func responsesReasoningHistorySupportsRecoveryWithAgents(body []byte, preserveCompaction, preserveAgentMessages bool) (bool, bool) {
	if hasReasoning, complete := responsesHistorySupportsRecoveryWithAgents(body, false, preserveAgentMessages); complete || !preserveCompaction {
		return hasReasoning, complete
	}
	if _, complete := responsesResourceHistorySupportsRecoveryWithAgents(body, preserveAgentMessages); !complete {
		return false, false
	}
	checkpoints := 0
	for _, item := range gjson.GetBytes(body, "input").Array() {
		if item.Get("type").String() != remoteCompactionItemType && item.Get("type").String() != legacyRemoteCompactionSummaryType {
			continue
		}
		checkpoints++
		ref := &remoteCompactionReference{ID: item.Get("id").String(), EncryptedContent: item.Get("encrypted_content").String()}
		if checkpoints > 1 || !strings.HasPrefix(ref.ID, "cmp_") || isLocalCompactionReference(ref) {
			return false, false
		}
	}
	return responsesHistorySupportsRecoveryWithAgents(body, true, preserveAgentMessages)
}

func responsesExplicitHistorySupportsRecovery(body []byte) (hasEncryptedReasoning, complete bool) {
	return responsesHistorySupportsRecovery(body, false)
}

// Callers opt into preserving opaque checkpoints only when their rewrite leaves
// them intact. Ordinary complete-history callers retain the stricter default.
func responsesHistorySupportsRecovery(body []byte, preserveCompaction bool) (hasEncryptedReasoning, complete bool) {
	return responsesHistorySupportsRecoveryWithAgents(body, preserveCompaction, false)
}

func responsesHistorySupportsRecoveryWithAgents(body []byte, preserveCompaction, preserveAgentMessages bool) (hasEncryptedReasoning, complete bool) {
	if !gjson.ValidBytes(body) || gjson.GetBytes(body, "previous_response_id").String() != "" ||
		gjson.GetBytes(body, "conversation").String() != "" {
		return false, false
	}
	input := gjson.GetBytes(body, "input")
	if !input.IsArray() {
		return false, false
	}

	hasUserHistory := false
	toolCalls := make(map[string]string)
	for _, item := range input.Array() {
		if !item.IsObject() {
			return false, false
		}
		itemType := item.Get("type").String()
		compaction := itemType == remoteCompactionItemType || itemType == legacyRemoteCompactionSummaryType
		if itemType != "reasoning" && (!preserveCompaction || !compaction) && item.Get("encrypted_content").String() != "" {
			return false, false
		}
		if item.Get("encrypted_function_args").Exists() {
			return false, false
		}
		switch itemType {
		case "additional_tools":
			if !item.Get("tools").IsArray() {
				return false, false
			}
		case "configuration_update":
		case "compaction_trigger":
			// Codex appends this empty control to an explicit history to ask the
			// backend for a new compaction. It carries no opaque prior state.
			if len(item.Map()) != 1 {
				return false, false
			}
		case "message", "":
			content := item.Get("content")
			if item.Get("role").String() == "user" &&
				((content.Type == gjson.String && content.String() != "") || (content.IsArray() && len(content.Array()) > 0)) {
				hasUserHistory = true
			}
		case "reasoning":
			encrypted := item.Get("encrypted_content")
			if encrypted.Exists() && encrypted.Type != gjson.String && encrypted.Type != gjson.Null {
				return false, false
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
				return false, false
			}
		case "agent_message":
			if preserveAgentMessages {
				if !responsesAgentMessageSupportsPreservation(item) {
					return false, false
				}
				continue
			}
			for _, content := range item.Get("content").Array() {
				if content.Get("type").String() == "encrypted_content" {
					return false, false
				}
			}
		case "compaction", "compaction_summary":
			if !preserveCompaction || item.Get("encrypted_content").Type != gjson.String || item.Get("encrypted_content").String() == "" {
				return false, false
			}
		case "context_compaction", "item_reference":
			return false, false
		default:
			return false, false
		}
	}
	return hasEncryptedReasoning, hasUserHistory
}

// These items are independent opaque messages, not missing tool results. A
// targeted reasoning/checkpoint repair can leave them verbatim without claiming
// to decrypt or materialize them. Generic encryption/resource errors still use
// the strict history guard. Never treat visible text as a substitute for the
// encrypted portion of an agent message.
func responsesAgentMessageSupportsPreservation(item gjson.Result) bool {
	content := item.Get("content")
	if !content.IsArray() || len(content.Array()) == 0 ||
		item.Get("author").Type != gjson.String || item.Get("author").String() == "" ||
		item.Get("recipient").Type != gjson.String || item.Get("recipient").String() == "" {
		return false
	}
	for _, part := range content.Array() {
		if !part.IsObject() || part.Get("file_id").Exists() || part.Get("encrypted_function_args").Exists() {
			return false
		}
		switch part.Get("type").String() {
		case "input_text", "output_text":
			if part.Get("text").Type != gjson.String || part.Get("encrypted_content").Exists() {
				return false
			}
		case "encrypted_content":
			if part.Get("encrypted_content").Type != gjson.String || part.Get("encrypted_content").String() == "" {
				return false
			}
		default:
			return false
		}
	}
	return true
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
