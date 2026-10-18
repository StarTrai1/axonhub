package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/llm/pipeline"
)

// Bound work, memory and upstream prefill independently of the request-log
// retention limit. JSON bytes deliberately overestimate text tokens. Unknown
// model budgets and multimodal/opaque sources do not qualify for this path.
const nativeCompactionReplayMaxBytes = 64 << 10

// Restore the rejected ancestor's exact source before asking for a summary of
// that source. When it fits, the newest checkpoint needs only one generation;
// no summary is fed into another summary. The existing strict replay checks
// still reject uncertain retained boundaries, opaque items and large sources.
func (a *remoteCompactionAdapter) restoreRejectedCompactionSource(ctx context.Context, source *remoteCompactionSource, ref *remoteCompactionReference, state *PersistenceState, executor pipeline.Executor, ancestors map[string]struct{}) (*remoteCompactionSource, error) {
	if state != nil && len(state.ChannelModelsCandidates) == 1 {
		replayState := *state
		replayState.CurrentCandidate = state.ChannelModelsCandidates[0]
		budget := nativeCompactionReplayBudget(&replayState)
		if budget == 0 || len(source.body) > budget {
			return a.adaptStoredCompactionSource(ctx, source, state, executor, ancestors)
		}
		visited := make(map[string]bool, len(ancestors))
		for key := range ancestors {
			visited[key] = true
		}
		body, restored, err := a.replayNativeCompactionChain(ctx, source.body, ref, &replayState, budget, visited)
		if err != nil {
			return nil, err
		}
		if restored {
			log.Info(ctx, "restored exact ancestor source before compaction without an intermediate summary",
				log.String("compaction_id", ref.ID))
			return &remoteCompactionSource{body: body, headers: source.headers}, nil
		}
	}
	return a.adaptStoredCompactionSource(ctx, source, state, executor, ancestors)
}

func (a *remoteCompactionAdapter) replayNativeCompaction(ctx context.Context, body []byte, ref *remoteCompactionReference, state *PersistenceState) ([]byte, bool, error) {
	budget := nativeCompactionReplayBudget(state)
	if budget == 0 || len(body) > budget {
		return body, false, nil
	}
	return a.replayNativeCompactionChain(ctx, body, ref, state, budget, make(map[string]bool))
}

func nativeCompactionReplayBudget(state *PersistenceState) int {
	if state == nil || state.CurrentCandidate == nil || state.CurrentCandidate.ContextTokens <= 0 {
		return 0
	}
	return min(nativeCompactionReplayMaxBytes, state.CurrentCandidate.ContextTokens/2)
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
		if len(envelope[field]) != 0 {
			if err := json.Unmarshal(envelope[field], &current); err != nil {
				return nil, false, err
			}
		}
		if len(sourceEnvelope[field]) != 0 {
			if err := json.Unmarshal(sourceEnvelope[field], &prior); err != nil {
				return nil, false, err
			}
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
		if !value.IsArray() {
			continue
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
