package orchestrator

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/tidwall/gjson"
)

// Some upstreams redact the item identity to gAAA...AA==. This is an explicit
// decryption rejection, but cannot tell reasoning from a native checkpoint.
// Keep it distinct from an exact item rejection throughout staged recovery.
var responsesRejectedCiphertextMessagePattern = regexp.MustCompile(
	`^(?:OpenAI Responses bad request: )?The encrypted content ([A-Za-z0-9_+/=-]+(?:(?:\.{3}|…)[A-Za-z0-9_+/=-]+)?) could not be verified\. Reason: Encrypted content could not be decrypted or parsed\.(?: \[trace_id=[A-Za-z0-9_-]+\])?$`,
)

// Some relays replace the original encrypted-item error with this exact
// message. It does not identify whether reasoning or the checkpoint failed:
// use the same bounded two-stage recovery as a redacted ciphertext rejection.
var responsesRejectedEncryptedSummaryMessagePattern = regexp.MustCompile(
	`^(?:OpenAI Responses bad request: )?invalid encrypted summary(?: \(request id: [A-Za-z0-9_-]+\))?$`,
)

func isResponsesUnidentifiedEncryptionRejection(message string) bool {
	return responsesRejectedCiphertextMessagePattern.MatchString(message) ||
		responsesRejectedEncryptedSummaryMessagePattern.MatchString(message)
}

func responsesRejectedCiphertextIndex(body []byte, code, message, param string) (int, bool) {
	if !responsesEncryptedContentRejectionCode(code) {
		return -1, false
	}
	match := responsesRejectedCiphertextMessagePattern.FindStringSubmatch(message)
	summary := responsesRejectedEncryptedSummaryMessagePattern.MatchString(message)
	if len(match) != 2 && !summary {
		return -1, false
	}
	redacted := summary || strings.Contains(match[1], "...") || strings.Contains(match[1], "…")
	index := -1
	if !redacted {
		for i, item := range gjson.GetBytes(body, "input").Array() {
			if item.Get("encrypted_content").Type == gjson.String && item.Get("encrypted_content").String() == match[1] {
				if index >= 0 {
					return -1, false
				}
				index = i
			}
		}
		if index < 0 {
			return -1, false
		}
	}
	if param == "" || param == "input" {
		return index, true
	}
	for i, item := range gjson.GetBytes(body, "input").Array() {
		if param == fmt.Sprintf("input[%d].encrypted_content", i) && (redacted || index == i) &&
			item.Get("encrypted_content").Type == gjson.String && item.Get("encrypted_content").String() != "" {
			return i, true
		}
	}
	return -1, false
}

func responsesRejectedCiphertextReasoningRule(body []byte, code, message, param string) (responsesRejectedStatusRule, bool) {
	index, ok := responsesRejectedCiphertextIndex(body, code, message, param)
	if !ok {
		return responsesRejectedStatusRule{}, false
	}
	if index >= 0 {
		return responsesRejectedReasoningRule(body, fmt.Sprintf("input[%d].encrypted_content", index))
	}
	// Stage one replays complete visible history while leaving the sole native
	// checkpoint verbatim. Opaque agent messages remain ambiguous and are not
	// eligible. This does not establish that the checkpoint is decryptable.
	hasReasoning, complete := responsesReasoningHistorySupportsRecovery(body, true)
	if !complete || !hasReasoning {
		return responsesRejectedStatusRule{}, false
	}
	return responsesRejectedStatusRule{itemType: "reasoning", index: -1, field: "encrypted_content", dropItem: true, preserveCompaction: true}, true
}

func responsesRejectedCiphertextCompaction(body []byte, ref *remoteCompactionReference, code, message, param string) bool {
	index, ok := responsesRejectedCiphertextIndex(body, code, message, param)
	if !ok || ref == nil || isLocalCompactionReference(ref) {
		return false
	}
	if index < 0 {
		// Stage two is only eligible after all encrypted reasoning has gone.
		// The middleware still requires the authenticated retained source and
		// original checkpoint identity before replacing anything or retrying.
		hasReasoning, complete := responsesReasoningHistorySupportsRecovery(body, true)
		if !complete || hasReasoning {
			return false
		}
	}
	found := false
	for i, item := range gjson.GetBytes(body, "input").Array() {
		if item.Get("id").String() != ref.ID {
			continue
		}
		if found || (index >= 0 && index != i) ||
			(item.Get("type").String() != remoteCompactionItemType && item.Get("type").String() != legacyRemoteCompactionSummaryType) ||
			item.Get("encrypted_content").String() != ref.EncryptedContent {
			return false
		}
		found = true
	}
	return found
}
