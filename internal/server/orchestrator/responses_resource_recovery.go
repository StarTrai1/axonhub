package orchestrator

import (
	"fmt"
	"regexp"
	"strconv"

	"github.com/tidwall/gjson"
)

var (
	// Compatible relays may redact the resource name and append request/trace
	// identifiers. Keep the full rejection anchored and accept only these known
	// wrappers; arbitrary resource mentions must not authorize history recovery.
	responsesResourceMismatchMessagePattern = regexp.MustCompile(
		`^(?:OpenAI Responses bad request: )?The requested item was created under a different (?:(?:Azure|\*\*\*) OpenAI|the upstream service) resource\. Use the same resource that created the item to access it\.(?: \(request id: [A-Za-z0-9_-]+\))?(?: \[trace_id=[A-Za-z0-9_-]+\])?$`,
	)
	responsesResourceItemIDParamPattern = regexp.MustCompile(`^input\[(\d+)\]\.id$`)
)

// A resource mismatch is distinct from a reasoning decryption rejection: a
// fully materialized message or tool call can still carry a stored item ID.
// Detach only those optional IDs first, preserving call_id and opaque reasoning.
func responsesRejectedResourceRule(body []byte, code, message, param string) (responsesRejectedStatusRule, bool) {
	if !responsesResourceMismatch(code, message) {
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
		return responsesRejectedStatusRule{index: -1, field: "id", detachReasoningID: param == "" && responsesResourceHasNativeReasoningIDs(body)}, true
	}
	if responsesResourceHasNativeReasoningIDs(body) {
		return responsesRejectedStatusRule{index: -1, field: "id", detachReasoningID: true}, true
	}
	// When no portable IDs remain, this explicit rejection permits the existing
	// complete-history reasoning recovery. Never infer this
	// from a 500, or discard native compaction to make a request pass validation.
	return responsesRejectedReasoningRule(body, "")
}

// A generic resource error does not identify the checkpoint as invalid. When
// the current window also has stored reasoning IDs, try their self-contained
// ciphertext first. Keep the checkpoint, summaries and every tool byte intact.
func responsesResourceHasNativeReasoningIDs(body []byte) bool {
	if _, safe := responsesResourceHistorySupportsRecovery(body); !safe {
		return false
	}
	ref, _, _, err := parseRemoteCompactionRequest(body)
	if err != nil || ref == nil || isLocalCompactionReference(ref) {
		return false
	}
	checkpoints := 0
	hasReasoningID := false
	for _, item := range gjson.GetBytes(body, "input").Array() {
		switch item.Get("type").String() {
		case remoteCompactionItemType, legacyRemoteCompactionSummaryType:
			checkpoints++
		case "reasoning":
			hasReasoningID = hasReasoningID || item.Get("id").String() != "" && item.Get("encrypted_content").String() != ""
		}
	}
	return checkpoints == 1 && hasReasoningID
}

func responsesResourceMismatch(code, message string) bool {
	return (code == "" || code == "bad_request" || code == "invalid_request_error") &&
		responsesResourceMismatchMessagePattern.MatchString(message)
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
	return responsesResourceHistorySupportsRecoveryWithAgents(body, false)
}

func responsesResourceHistorySupportsRecoveryWithAgents(body []byte, preserveAgentMessages bool) (hasIDs, safe bool) {
	if _, complete := responsesHistorySupportsRecoveryWithAgents(body, true, preserveAgentMessages); !complete {
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
		case "additional_tools", "reasoning", "configuration_update", "compaction_trigger", "compaction", "compaction_summary":
		case "agent_message":
			if !responsesAgentMessageSupportsPreservation(item) {
				return false, false
			}
			// Plaintext agent messages are already materialized, including those
			// restored by the preceding agent-message middleware. The history
			// guard above still requires preserveAgentMessages for ciphertext.
			// Keep the entire agent item and its delivery ID unchanged.
			continue
		default:
			return false, false
		}
		// Only message content uses Responses content-part semantics. Tool output
		// is opaque application data and may legitimately contain fields named
		// file_id or encrypted_content; never reinterpret it as protocol state.
		if itemType == "" || itemType == "message" {
			for _, part := range item.Get("content").Array() {
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
