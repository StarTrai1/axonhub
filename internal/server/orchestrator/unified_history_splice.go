package orchestrator

import (
	"bytes"
	"encoding/json"
	"slices"

	"github.com/tidwall/gjson"
)

// Reconcile against the immediate authenticated source before unfolding its
// ancestors. Codex reinserts current client context before the last retained
// user message, refreshes additional_tools, and may assign new message IDs.
// Visible message identity is compared separately from transport-local IDs;
// tool IDs, arguments, results and opaque state are never normalized away.
func spliceUnifiedCompactionSource(body, source []byte, ref *remoteCompactionReference) ([]byte, bool, error) {
	envelope, input, err := decodeResponsesInput(body)
	if err != nil {
		return nil, false, err
	}
	prior, original, err := decodeResponsesInput(source)
	if err != nil {
		return nil, false, err
	}
	for _, field := range []string{"model", "instructions", "tools"} {
		var left, right any
		if len(envelope[field]) > 0 {
			if err := json.Unmarshal(envelope[field], &left); err != nil {
				return nil, false, err
			}
		}
		if len(prior[field]) > 0 {
			if err := json.Unmarshal(prior[field], &right); err != nil {
				return nil, false, err
			}
		}
		x, _ := json.Marshal(left)
		y, _ := json.Marshal(right)
		if !bytes.Equal(x, y) {
			return body, false, nil
		}
	}
	if ref == nil || ref.Index < 0 || ref.Index >= len(input) || len(original) == 0 || rawInputItemType(original[len(original)-1]) != remoteCompactionTriggerType {
		return body, false, nil
	}
	checkpoint := gjson.ParseBytes(input[ref.Index])
	if checkpoint.Get("id").String() != ref.ID || checkpoint.Get("encrypted_content").String() != ref.EncryptedContent {
		return body, false, nil
	}
	original = original[:len(original)-1]
	canonical := make([]string, len(original))
	byContent := make(map[string][]int)
	byID := make(map[string][]int)
	for i, item := range original {
		canonical[i], err = unifiedRetainedIdentity(item)
		if err != nil {
			return nil, false, err
		}
		byContent[canonical[i]] = append(byContent[canonical[i]], i)
		if id := gjson.GetBytes(item, "id").String(); id != "" {
			byID[id] = append(byID[id], i)
		}
	}
	// Source order is authoritative. Greedy earliest subsequence alignment
	// handles repeated identical user messages without discarding either copy.
	result := make([]json.RawMessage, 0, len(original)+len(input))
	var additions []json.RawMessage
	position := 0
	for _, item := range input[:ref.Index] {
		identity, err := unifiedRetainedIdentity(item)
		if err != nil {
			return nil, false, err
		}
		match := -1
		id := gjson.GetBytes(item, "id").String()
		if ids := byID[id]; id != "" && len(ids) > 0 {
			for _, index := range ids {
				if canonical[index] != identity && rawInputItemType(item) != "additional_tools" {
					// A changed message under the same ID is not reinsertion.
					return body, false, nil
				}
				if canonical[index] == identity && index >= position && match < 0 {
					match = index
				}
			}
		} else {
			for _, index := range byContent[identity] {
				if index >= position {
					match = index
					break
				}
			}
		}
		if match < 0 {
			role := gjson.GetBytes(item, "role").String()
			typ := rawInputItemType(item)
			if (typ != "message" && typ != "" && typ != "additional_tools") || !slices.Contains([]string{"user", "system", "developer"}, role) {
				return body, false, nil
			}
			additions = append(additions, item)
			continue
		}
		if match < position {
			return body, false, nil
		}
		result = append(result, original[position:match]...)
		result = append(result, additions...)
		additions = nil
		result = append(result, original[match])
		position = match + 1
	}
	result = append(result, original[position:]...)
	result = append(result, additions...)
	result = append(result, input[ref.Index+1:]...)
	if err := setResponseEnvelopeInput(envelope, result); err != nil {
		return nil, false, err
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return nil, false, err
	}
	if len(encoded) > nativeCompactionMaxSourceBytes {
		return body, false, nil
	}
	if _, complete := responsesResourceHistorySupportsRecoveryWithAgents(encoded, true); !complete {
		return body, false, nil
	}
	return encoded, true, nil
}

func unifiedRetainedIdentity(raw json.RawMessage) (string, error) {
	canonical, err := compactionReplayItem(raw)
	if err != nil {
		return "", err
	}
	var item map[string]json.RawMessage
	if err := json.Unmarshal(canonical, &item); err != nil {
		return "", err
	}
	switch rawInputItemType(raw) {
	case "", "message", "additional_tools":
		delete(item, "id")
		delete(item, "status")
	}
	canonical, err = json.Marshal(item)
	return string(canonical), err
}
