package orchestrator

import (
	"regexp"

	"github.com/tidwall/gjson"
)

var responsesOpaqueRelayBadRequest = regexp.MustCompile(`^bad response status code 400(?: \(request id: [A-Za-z0-9_-]+\))?$`)

// Compatible relays can reject stored item IDs without exposing the upstream
// resource error. Retry self-contained history as new input, retaining every
// encrypted reasoning byte, message, tool call, result, and client option.
func responsesRelayReplayRule(body []byte, code, message, param string) (responsesRejectedStatusRule, bool) {
	if param != "" || code != "" && code != "bad_request" && code != "invalid_request_error" ||
		!responsesOpaqueRelayBadRequest.MatchString(message) {
		return responsesRejectedStatusRule{}, false
	}
	hasReasoning, explicit := responsesExplicitHistorySupportsRecovery(body)
	_, materialized := responsesResourceHistorySupportsRecovery(body)
	if !hasReasoning || !explicit || !materialized {
		return responsesRejectedStatusRule{}, false
	}
	for _, item := range gjson.GetBytes(body, "input").Array() {
		if item.Get("id").String() != "" &&
			(item.Get("type").String() == "reasoning" || responsesInputSupportsPortableID(item.Get("type").String())) {
			return responsesRejectedStatusRule{index: -1, field: "relay_history_replay"}, true
		}
	}
	return responsesRejectedStatusRule{}, false
}

func replayResponsesRelayHistory(body []byte, rules []responsesRejectedStatusRule) ([]byte, bool, error) {
	// Recheck at application time: an override or retry may have changed input.
	// In particular, never apply this ID correction to native checkpoints or
	// unresolved references, including encrypted tool results and arguments.
	if _, ok := responsesRelayReplayRule(body, "", "bad response status code 400", ""); !ok {
		return body, false, nil
	}
	retainedRules := make([]responsesRejectedStatusRule, 0, len(rules)+1)
	for _, rule := range rules {
		if rule.fieldName() != "relay_history_replay" {
			retainedRules = append(retainedRules, rule)
		}
	}
	retainedRules = append(retainedRules, responsesRejectedStatusRule{index: -1, field: "id", detachReasoningID: true})
	return stripResponsesRejectedStatus(body, retainedRules)
}

func hasResponsesRelayReplayRule(rules []responsesRejectedStatusRule) bool {
	for _, rule := range rules {
		if rule.fieldName() == "relay_history_replay" {
			return true
		}
	}
	return false
}
