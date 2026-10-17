package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/tidwall/gjson"
)

// Bound work, memory and upstream prefill independently of the request-log
// retention limit. JSON bytes deliberately overestimate text tokens. Unknown
// model budgets and multimodal/opaque sources do not qualify for this path.
const nativeCompactionReplayMaxBytes = 64 << 10

func (a *remoteCompactionAdapter) replayNativeCompaction(ctx context.Context, body []byte, ref *remoteCompactionReference, state *PersistenceState) ([]byte, bool, error) {
	if state == nil || state.CurrentCandidate == nil || state.CurrentCandidate.ContextTokens <= 0 {
		return body, false, nil
	}
	budget := min(nativeCompactionReplayMaxBytes, state.CurrentCandidate.ContextTokens/2)
	if len(body) > budget {
		return body, false, nil
	}
	return a.replayNativeCompactionChain(ctx, body, ref, state, budget, make(map[string]bool))
}

func (a *remoteCompactionAdapter) replayNativeCompactionChain(ctx context.Context, body []byte, ref *remoteCompactionReference, state *PersistenceState, budget int, ancestors map[string]bool) ([]byte, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	key := remoteCompactionCacheKey(ref)
	if ancestors[key] || len(ancestors) >= remoteCompactionMaxSourceDepth {
		return nil, false, errors.New("cyclic or excessively deep native compaction source")
	}
	ancestors[key] = true
	defer delete(ancestors, key)
	// Only use authenticated source capsules; an unbounded log scan is not
	// justified just to discover that a large source cannot fit the budget.
	source, err := a.loadNativeCompactionSource(ctx, ref, state)
	if err != nil || source == nil {
		return body, false, err
	}
	if len(source.body) > budget {
		return body, false, nil
	}
	sourceBody := source.body
	previous, _, _, err := parseRemoteCompactionRequest(sourceBody)
	if err != nil {
		return nil, false, err
	}
	if previous != nil {
		var restored bool
		sourceBody, restored, err = a.replayNativeCompactionChain(ctx, sourceBody, previous, state, budget, ancestors)
		if err != nil || !restored {
			return body, false, err
		}
	}
	return spliceNativeCompactionSource(body, sourceBody, ref, budget)
}

// Codex V2 places retained messages before its checkpoint. They are an ordered
// subset of the source, not additional turns to concatenate with that source.
// Require a unique exact match for every retained item before replacing that
// whole prefix, then preserve every post-checkpoint item and request option.
func spliceNativeCompactionSource(body, source []byte, ref *remoteCompactionReference, budget int) ([]byte, bool, error) {
	envelope, input, err := decodeResponsesInput(body)
	if err != nil {
		return nil, false, err
	}
	sourceEnvelope, original, err := decodeResponsesInput(source)
	if err != nil {
		return nil, false, err
	}
	for _, field := range []string{"model", "instructions", "tools"} {
		var current, prior any
		if len(envelope[field]) != 0 && json.Unmarshal(envelope[field], &current) != nil ||
			len(sourceEnvelope[field]) != 0 && json.Unmarshal(sourceEnvelope[field], &prior) != nil {
			return body, false, nil
		}
		left, _ := json.Marshal(current)
		right, _ := json.Marshal(prior)
		if !bytes.Equal(left, right) {
			return body, false, nil
		}
	}
	if ref == nil || ref.Index < 0 || ref.Index >= len(input) || len(original) == 0 ||
		rawInputItemType(original[len(original)-1]) != remoteCompactionTriggerType {
		return body, false, nil
	}
	checkpoint := gjson.ParseBytes(input[ref.Index])
	if checkpoint.Get("id").String() != ref.ID || checkpoint.Get("encrypted_content").String() != ref.EncryptedContent {
		return body, false, nil
	}
	original = original[:len(original)-1]
	canonical := make([][]byte, len(original))
	for i, item := range original {
		if !exactReplayTextItem(item) {
			return body, false, nil
		}
		canonical[i], err = compactionReplayItem(item)
		if err != nil {
			return nil, false, err
		}
	}
	position := 0
	for _, retained := range input[:ref.Index] {
		wanted, err := compactionReplayItem(retained)
		if err != nil {
			return nil, false, err
		}
		match := -1
		for i := position; i < len(canonical); i++ {
			if bytes.Equal(wanted, canonical[i]) {
				if match >= 0 {
					return body, false, nil
				}
				match = i
			}
		}
		if match < 0 {
			return body, false, nil
		}
		position = match + 1
	}
	restored := append(append([]json.RawMessage(nil), original...), input[ref.Index+1:]...)
	for _, item := range restored {
		if !exactReplayTextItem(item) {
			return body, false, nil
		}
	}
	if err := setResponseEnvelopeInput(envelope, restored); err != nil {
		return nil, false, err
	}
	result, err := json.Marshal(envelope)
	if err != nil {
		return nil, false, err
	}
	if len(result) > budget {
		return body, false, nil
	}
	if _, complete := responsesResourceHistorySupportsRecovery(result); !complete {
		return body, false, nil
	}
	return result, true, nil
}

func exactReplayTextItem(raw json.RawMessage) bool {
	item := gjson.ParseBytes(raw)
	if item.Get("encrypted_function_args").Exists() || item.Get("encrypted_content").String() != "" {
		return false
	}
	switch rawInputItemType(raw) {
	case "", "message", "function_call", "function_call_output", "custom_tool_call", "custom_tool_call_output", "additional_tools", "configuration_update", "reasoning", "compaction_trigger":
	default:
		return false
	}
	for _, field := range []string{"content", "output"} {
		value := item.Get(field)
		if value.Exists() && !value.IsArray() && value.Type != gjson.String && value.Type != gjson.Null {
			return false
		}
		for _, part := range value.Array() {
			if part.Get("file_id").Exists() || part.Get("encrypted_content").Exists() || part.Get("encrypted_function_args").Exists() {
				return false
			}
			switch part.Get("type").String() {
			case "input_text", "output_text", "text", "reasoning_text":
			default:
				return false
			}
		}
	}
	return true
}
