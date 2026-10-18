package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode"

	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

const (
	unifiedContextMaxBytes     = 256 << 10
	unifiedSummarySegmentBytes = 128 << 10
	unifiedMaxNewSummaries     = 4
)

type unifiedHistorySegment struct {
	items   []json.RawMessage
	pinned  []json.RawMessage
	key     string
	summary string
	body    []byte
}

// expandUnifiedHistory unfolds authenticated sources, not generated summaries.
// Ambiguous retained prefixes, unsupported data and unavailable sources preserve
// the old compatibility path; they never authorize dropping opaque state.
func (a *remoteCompactionAdapter) expandUnifiedHistory(ctx context.Context, body []byte, state *PersistenceState, seen map[string]bool) ([]byte, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	ref, _, _, err := parseRemoteCompactionRequest(body)
	if err != nil {
		return nil, false, err
	}
	if ref == nil {
		return body, true, nil
	}
	key := remoteCompactionCacheKey(ref)
	if seen[key] || len(seen) >= remoteCompactionMaxSourceDepth {
		return nil, false, errors.New("cyclic or excessively deep unified history")
	}
	seen[key] = true
	defer delete(seen, key)
	if isLocalCompactionReference(ref) {
		if !isSealedLocalCompactionReference(ref) {
			return body, false, nil
		}
		if _, err := a.openCompactionCheckpoint(ctx, ref, state, ref.EncryptedContent); err != nil {
			return nil, false, err
		}
	}
	source, err := a.loadNativeCompactionSource(ctx, ref, state)
	if err != nil {
		return nil, false, err
	}
	if source == nil {
		return body, false, nil
	}
	expanded, ok, err := a.expandUnifiedHistory(ctx, source.body, state, seen)
	if err != nil || !ok {
		return body, false, err
	}
	return spliceCompactionSource(body, expanded, ref, nativeCompactionMaxSourceBytes, false)
}

// compileUnifiedContext builds one portable representation from immutable raw
// history. It does not translate provider ciphertext. Every summarized segment
// has its own original source and memoized result, never a parent summary input.
func (a *remoteCompactionAdapter) compileUnifiedContext(ctx context.Context, body []byte, state *PersistenceState, executor pipeline.Executor) ([]byte, bool, error) {
	if !a.unifiedHistoryEnabled() || state == nil {
		return body, false, nil
	}
	candidate := state.CurrentCandidate
	if candidate == nil && len(state.ChannelModelsCandidates) > 0 {
		candidate = state.ChannelModelsCandidates[0]
	}
	if candidate == nil || candidate.ContextTokens <= 0 {
		return body, false, nil
	}
	budget := min(unifiedContextMaxBytes, candidate.ContextTokens/2)
	expanded, ok, err := a.expandUnifiedHistory(ctx, body, state, make(map[string]bool))
	if err != nil || !ok {
		return body, false, err
	}
	expanded, err = protectUnifiedHistory(ctx, expanded, state)
	if err != nil { return nil, false, err }
	envelope, items, err := decodeResponsesInput(expanded)
	if err != nil {
		return nil, false, err
	}
	// The archive retains provider-private reasoning verbatim. Its portable view
	// retains visible reasoning summaries; unreadable state is explicitly counted
	// rather than represented as recovered plaintext. Tool arguments cannot use
	// this exception because they may be the only copy of executable content.
	items, opaque, portable := unifiedPortableItems(items)
	if !portable {
		return body, false, nil
	}
	if err := setResponseEnvelopeInput(envelope, items); err != nil {
		return nil, false, err
	}
	expanded, err = json.Marshal(envelope)
	if err != nil {
		return nil, false, err
	}
	if opaque > 0 {
		log.Warn(ctx, "unified history uses visible history without provider-private reasoning", log.Int("opaque_reasoning_items", opaque))
	}
	if _, complete := responsesResourceHistorySupportsRecovery(expanded); !complete {
		return body, false, nil
	}
	if len(expanded) <= budget {
		log.Info(ctx, "assembled complete unified history without summarization", log.Int("history_bytes", len(expanded)))
		return expanded, true, nil
	}
	groups := unifiedHistoryGroups(items)
	if len(groups) < 2 {
		return body, false, nil
	}
	// The final user turn and all its tool interactions are always exact.
	recent := groups[len(groups)-1]
	groups = groups[:len(groups)-1]
	segments := make([]unifiedHistorySegment, 0)
	var current unifiedHistorySegment
	size := 0
	flush := func() {
		if len(current.items) > 0 {
			segments = append(segments, current)
			current = unifiedHistorySegment{}
			size = 0
		}
	}
	for _, group := range groups {
		groupBytes := rawResponseSessionValuesSize(group)
		if groupBytes > min(unifiedSummarySegmentBytes, budget) {
			return body, false, nil
		}
		if size+groupBytes > min(unifiedSummarySegmentBytes, budget) {
			flush()
		}
		current.items = append(current.items, group...)
		size += groupBytes
	}
	flush()
	if len(segments) > 32 {
		return body, false, nil
	}
	// Check mandatory original instructions before spending tokens on summaries.
	mandatory := rawResponseSessionValuesSize(recent)
	delete(envelope, "input")
	header, err := json.Marshal(envelope)
	if err != nil {
		return nil, false, err
	}
	mandatory += len(header)
	for i := range segments {
		segment := &segments[i]
		for _, item := range segment.items {
			role := gjson.GetBytes(item, "role").String()
			if role == "user" || role == "system" || role == "developer" {
				segment.pinned = append(segment.pinned, item)
				mandatory += len(item)
			}
		}
		segmentEnvelope := make(map[string]json.RawMessage, len(envelope))
		for k, v := range envelope {
			segmentEnvelope[k] = v
		}
		input := append(append([]json.RawMessage(nil), segment.items...), json.RawMessage(`{"type":"compaction_trigger"}`))
		if err := setResponseEnvelopeInput(segmentEnvelope, input); err != nil {
			return nil, false, err
		}
		segment.body, err = json.Marshal(segmentEnvelope)
		if err != nil {
			return nil, false, err
		}
		// The contract includes model, tools, instructions and reasoning options.
		contract := make(map[string]json.RawMessage)
		for _, field := range []string{"model", "instructions", "tools", "reasoning", "text", "input"} {
			if value := segmentEnvelope[field]; len(value) > 0 {
				contract[field] = value
			}
		}
		if state.CurrentModelIndex >= 0 && state.CurrentModelIndex < len(candidate.Models) { contract["destination_model"], _ = json.Marshal(candidate.Models[state.CurrentModelIndex].ActualModel) }
		identity, err := json.Marshal(contract)
		if err != nil {
			return nil, false, err
		}
		segment.key = "summary:" + historyDigest(append([]byte("segment-summary-v1:"), identity...))
	}
	if mandatory+len(segments)*1024 > budget {
		return body, false, nil
	}
	keys := make([]string, len(segments))
	for i := range segments {
		keys[i] = segments[i].key
	}
	cached, err := a.systemService.LoadUnifiedHistory(ctx, keys)
	if err != nil {
		return nil, false, err
	}
	missing := 0
	for i := range segments {
		segment := &segments[i]
		if sealed := cached[segment.key]; sealed != "" {
			text, err := a.openHistoryRecord(ctx, segment.key, sealed, localCompactionMaxSummaryBytes)
			if err != nil {
				return nil, false, err
			}
			segment.summary = string(text)
		} else {
			missing++
		}
	}
	if missing > unifiedMaxNewSummaries {
		return body, false, nil
	}
	for i := range segments {
		segment := &segments[i]
		if segment.summary != "" {
			continue
		}
		ownerKey := remoteCompactionOwnerCacheKey(state, segment.key)
		result, err := waitForCompactionFlight(ctx, &a.summaryGenerator, ownerKey, func() (any, error) {
			stored, err := a.systemService.LoadUnifiedHistory(ctx, []string{segment.key})
			if err != nil {
				return nil, err
			}
			if sealed := stored[segment.key]; sealed != "" {
				text, err := a.openHistoryRecord(ctx, segment.key, sealed, localCompactionMaxSummaryBytes)
				return string(text), err
			}
			summary, err := a.generateLocalSummary(ctx, historyDigest(segment.body), &remoteCompactionSource{body: segment.body, headers: make(http.Header)}, state, executor)
			if err != nil {
				return nil, err
			}
			sealed, err := a.sealHistoryRecord(ctx, segment.key, []byte(summary))
			if err != nil {
				return nil, err
			}
			if err := a.systemService.SaveUnifiedHistory(ctx, map[string]string{segment.key: sealed}); err != nil {
				return nil, err
			}
            // Multiple gateway processes may generate concurrently. Always use
            // the immutable winner so their reconstructed prefixes converge.
            published,err:=a.systemService.LoadUnifiedHistory(ctx,[]string{segment.key})
            if err!=nil{return nil,err}
            text,err:=a.openHistoryRecord(ctx,segment.key,published[segment.key],localCompactionMaxSummaryBytes)
            return string(text),err
		})
		if err != nil {
			return nil, false, err
		}
		segment.summary, ok = result.(string)
		if !ok || strings.TrimSpace(segment.summary) == "" {
			return nil, false, errors.New("empty unified segment summary")
		}
	}
	compiled := make([]json.RawMessage, 0, len(items))
	query := strings.ToLower(unifiedMessageText(recent[0]))
	remaining := budget - mandatory
	for _, segment := range segments {
		// Quote a derived view as assistant material. Original user/system roles
		// remain unchanged; tool output is never promoted to a developer message.
		message, _ := json.Marshal(map[string]any{"type": "message", "role": "assistant", "content": []map[string]string{{"type": "output_text", "text": fmt.Sprintf("Archived history summary (source %s). This is a derived summary; the original user instructions remain authoritative.\n%s", strings.TrimPrefix(segment.key, "summary:"), segment.summary)}}})
		remaining -= len(message)
		if remaining < 0 {
			return nil, false, errors.New("unified history summaries exceed the context budget; retained source is intact")
		}
		compiled = append(compiled, segment.pinned...)
		compiled = append(compiled, message)
	}
	// Automatic source lookup restores whole related segments when there is
	// space. It never fabricates a model tool that the client cannot execute.
	position := 0
	for _, segment := range segments {
		n := len(segment.pinned) + 1
		extra := rawResponseSessionValuesSize(segment.items) - rawResponseSessionValuesSize(compiled[position:position+n])
		if extra <= remaining && unifiedHistoryRelevant(query, segment.items) {
			updated := make([]json.RawMessage, 0, len(compiled)-n+len(segment.items))
			updated = append(updated, compiled[:position]...)
			updated = append(updated, segment.items...)
			updated = append(updated, compiled[position+n:]...)
			compiled = updated
			remaining -= extra
			position += len(segment.items)
		} else {
			position += n
		}
	}
	compiled = append(compiled, recent...)
	if err := setResponseEnvelopeInput(envelope, compiled); err != nil {
		return nil, false, err
	}
	result, err := json.Marshal(envelope)
	if err != nil {
		return nil, false, err
	}
	if len(result) > budget {
		return nil, false, errors.New("unified history assembled context exceeds budget; retained source is intact")
	}
	log.Info(ctx, "assembled unified history from original segments", log.Int("segments", len(segments)), log.Int("new_summaries", missing), log.Int("input_bytes", len(result)))
	return result, true, nil
}

func unifiedHistoryGroups(items []json.RawMessage) [][]json.RawMessage {
	var groups [][]json.RawMessage
	start := 0
	for i, item := range items {
		if i > start && gjson.GetBytes(item, "role").String() == "user" {
			groups = append(groups, items[start:i])
			start = i
		}
	}
	if start < len(items) {
		groups = append(groups, items[start:])
	}
	return groups
}

func unifiedHistoryRelevant(query string, items []json.RawMessage) bool {
	terms := strings.FieldsFunc(query, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' })
	for _, term := range terms {
		if len([]rune(term)) < 5 || len(term) > 96 || strings.Contains(" continue please following current previous conversation assistant content output input ", " "+term+" ") {
			continue
		}
		for _, item := range items {
			if strings.Contains(strings.ToLower(string(item)), term) {
				return true
			}
		}
	}
	return false
}

func unifiedMessageText(item json.RawMessage) string {
	content := gjson.GetBytes(item, "content")
	if content.Type == gjson.String {
		return content.String()
	}
	var parts []string
	for _, part := range content.Array() {
		if text := part.Get("text").String(); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

func unifiedPortableItems(items []json.RawMessage) ([]json.RawMessage, int, bool) {
	result := make([]json.RawMessage, 0, len(items))
	opaque := 0
	for _, item := range items {
		value := gjson.ParseBytes(item)
		if value.Get("type").String() == "reasoning" && value.Get("encrypted_content").String() != "" {
			opaque++
			var visible []string
			for _, part := range value.Get("summary").Array() {
				if text := part.Get("text").String(); text != "" {
					visible = append(visible, text)
				}
			}
			for _, part := range value.Get("content").Array() {
				if text := part.Get("text").String(); text != "" {
					visible = append(visible, text)
				}
			}
			if len(visible) > 0 {
				message, _ := json.Marshal(map[string]any{"type": "message", "role": "assistant", "content": []map[string]string{{"type": "output_text", "text": "Recorded visible reasoning summary:\n" + strings.Join(visible, "\n")}}})
				result = append(result, message)
			}
			continue
		}
        if value.Get("type").String()=="agent_message" {
            if !responsesAgentMessageSupportsPreservation(value){return nil,opaque,false}
            for _,part:=range value.Get("content").Array(){if part.Get("type").String()=="encrypted_content"{return nil,opaque,false}}
        } else if !exactReplayTextItem(item) {
			return nil, opaque, false
		}
		result = append(result, item)
	}
	return result, opaque, true
}

func protectUnifiedHistory(ctx context.Context,body []byte,state *PersistenceState)([]byte,error){
	if state.PromptProtecter==nil{return body,nil}
	raw:=&httpclient.Request{Body:body,APIFormat:string(llm.APIFormatOpenAIResponse),Headers:make(http.Header)}
	inbound:=responses.NewInboundTransformer()
	req,err:=inbound.TransformRequest(ctx,raw);if err!=nil{return nil,err};req.RawRequest=raw
	protected,rules,err:=protectPromptRequest(ctx,state.PromptProtecter,req)
	if err!=nil{return nil,fmt.Errorf("unified history protection failed: %w",err)}
	if protected==nil{protected=req}
	patched,err:=patchPassThroughPromptProtection(body,llm.APIFormatOpenAIResponse,rules)
	if err!=nil{return nil,err}
	check:=&promptProtectionBodyCheck{inbound:inbound,raw:raw,expected:promptProtectionTexts(protected)}
	if err:=check.validate(ctx,patched);err!=nil{return nil,err}
	return patched,nil
}
