package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

const unifiedContextMaxTokens = 1 << 20

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
	expanded, ok, err := spliceUnifiedCompactionSource(body, source.body, ref)
	if err != nil || !ok {
		return body, false, err
	}
	return a.expandUnifiedHistory(ctx, expanded, state, seen)
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
		if value.Get("type").String() == "agent_message" {
			if !responsesAgentMessageSupportsPreservation(value) {
				return nil, opaque, false
			}
			for _, part := range value.Get("content").Array() {
				if part.Get("type").String() == "encrypted_content" {
					return nil, opaque, false
				}
			}
		} else if !unifiedPortableItem(item) {
			return nil, opaque, false
		}
		// These are complete reconstructed records, not upstream stored-item
		// references. Avoid resource hydration and duplicate IDs after reinsertion.
		portableItem, err := unifiedPortableIdentity(item)
		if err != nil {
			return nil, opaque, false
		}
		result = append(result, portableItem)
	}
	return result, opaque, true
}

func protectUnifiedHistory(ctx context.Context, body []byte, state *PersistenceState) ([]byte, error) {
	if state.PromptProtecter == nil {
		return body, nil
	}
	raw := &httpclient.Request{Body: body, APIFormat: string(llm.APIFormatOpenAIResponse), Headers: make(http.Header)}
	inbound := responses.NewInboundTransformer()
	req, err := inbound.TransformRequest(ctx, raw)
	if err != nil {
		return nil, err
	}
	req.RawRequest = raw
	protected, rules, err := protectPromptRequest(ctx, state.PromptProtecter, req)
	if err != nil {
		return nil, fmt.Errorf("unified history protection failed: %w", err)
	}
	if protected == nil {
		protected = req
	}
	patched := body
	if len(rules) > 0 {
		patched, err = patchPassThroughPromptProtection(body, llm.APIFormatOpenAIResponse, rules)
		if err != nil {
			return nil, err
		}
	}
	check := &promptProtectionBodyCheck{inbound: inbound, raw: raw, expected: promptProtectionTexts(protected)}
	if err := check.validate(ctx, patched); err != nil {
		return nil, err
	}
	return patched, nil
}
