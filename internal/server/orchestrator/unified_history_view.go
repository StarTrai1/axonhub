package orchestrator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"time"

	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/llm/httpclient"
)

// A view is the context actually accepted by one destination, not an archival
// source. Its immutable key binds every covered client item and the execution
// contract. A sibling or an edited prefix therefore cannot inherit the view.
func unifiedViewContract(envelope map[string]json.RawMessage) ([]byte, error) {
	contract := make(map[string]json.RawMessage)
	for _, field := range []string{"model", "instructions", "tools", "tool_choice", "parallel_tool_calls", "reasoning", "text", "include", "store"} {
		if value := envelope[field]; len(value) > 0 {
			contract[field] = value
		}
	}
	raw, err := json.Marshal(contract)
	if err != nil {
		return nil, err
	}
	return canonicalHistoryJSON(raw)
}

func unifiedViewKeys(key responsesCompactionRecoveryKey, body []byte) ([]string, error) {
	if len(body) > nativeCompactionMaxSourceBytes || gjson.GetBytes(body, "previous_response_id").String() != "" {
		return nil, nil
	}
	envelope, input, err := decodeResponsesInput(body)
	if err != nil {
		return nil, err
	}
	if len(input) > 65536 {
		return nil, errors.New("too many unified view input items")
	}
	canonical, err := unifiedViewContract(envelope)
	if err != nil {
		return nil, err
	}
	scope := key.digest()
	hash := sha256.New()
	hash.Write([]byte("unified-accepted-view-v1:"))
	hash.Write(scope[:])
	hash.Write(canonical)
	keys := make([]string, len(input))
	for i, item := range input {
		identity, err := unifiedRetainedIdentity(item)
		if err != nil {
			return nil, err
		}
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(len(identity)))
		hash.Write(size[:])
		hash.Write([]byte(identity))
		keys[i] = "view:" + historyDigest(hash.Sum(nil))
	}
	return keys, nil
}

func appendUnifiedView(body, original, view []byte, covered int) ([]byte, bool, error) {
	envelope, wire, err := decodeResponsesInput(body)
	if err != nil {
		return nil, false, err
	}
	_, source, err := decodeResponsesInput(original)
	if err != nil {
		return nil, false, err
	}
	priorEnvelope, prior, err := decodeResponsesInput(view)
	if err != nil {
		return nil, false, err
	}
	if covered <= 0 || covered > len(source) || len(source) != len(wire) || gjson.GetBytes(view, "previous_response_id").String() != "" {
		return body, false, nil
	}
	currentContract, err := unifiedViewContract(envelope)
	if err != nil {
		return nil, false, err
	}
	priorContract, err := unifiedViewContract(priorEnvelope)
	if err != nil {
		return nil, false, err
	}
	if !bytes.Equal(currentContract, priorContract) {
		return body, false, nil
	}
	// Take the prepared wire suffix, never bypass agent argument decryption or
	// request protection by copying unchecked client payloads to the upstream.
	for i, item := range wire {
		left, err := unifiedRetainedIdentity(item)
		if err != nil {
			return nil, false, err
		}
		right, err := unifiedRetainedIdentity(source[i])
		if err != nil {
			return nil, false, err
		}
		if left != right {
			return body, false, nil
		}
	}
	input := append(prior, wire[covered:]...)
	if err := setResponseEnvelopeInput(envelope, input); err != nil {
		return nil, false, err
	}
	delete(envelope, "previous_response_id")
	result, err := json.Marshal(envelope)
	if err != nil {
		return nil, false, err
	}
	if len(result) > nativeCompactionMaxSourceBytes {
		return body, false, nil
	}
	return result, true, nil
}

func (m *responsesCompactionRecoveryMiddleware) restoreUnifiedView(ctx context.Context, request *httpclient.Request, key responsesCompactionRecoveryKey) (bool, error) {
	if !m.adapter.unifiedHistoryEnabled() {
		return false, nil
	}
	key = m.unifiedViewScope(key, request)
	original := rawRequestPayload(m.outbound.state.RawRequest)
	keys, err := unifiedViewKeys(key, original)
	if err != nil || len(keys) == 0 {
		return false, err
	}
	// Newest matching prefix first. Bounded batches avoid an ever-growing
	// mutable thread pointer and preserve independently continued branches.
	for end := len(keys); end > 0; {
		start := max(0, end-128)
		entries, err := m.adapter.systemService.LoadUnifiedHistory(ctx, keys[start:end])
		if err != nil {
			return false, err
		}
		for i := end-1; i >= start; i-- {
			if entries[keys[i]] == "" {
				continue
			}
			view, _, found, err := m.adapter.loadHistorySnapshot(ctx, keys[i])
			if err != nil {
				return false, err
			}
			if found {
				body, matched, err := appendUnifiedView(request.Body, original, view, i+1)
				if err != nil || !matched {
					return false, err
				}
				return true, m.applyUnifiedView(ctx, request, body)
			}
		}
		end = start
	}
	view, covered, err := m.importUnifiedView(ctx, request, key, keys)
	if err != nil || covered == 0 {
		return false, err
	}
	body, matched, err := appendUnifiedView(request.Body, original, view, covered)
	if err != nil || !matched {
		return false, err
	}
	return true, m.applyUnifiedView(ctx, request, body)
}

func (m *responsesCompactionRecoveryMiddleware) applyUnifiedView(ctx context.Context, request *httpclient.Request, body []byte) error {
	body, err := protectUnifiedHistory(ctx, body, m.outbound.state)
	if err != nil {
		return err
	}
	request.Body = body
	if len(request.JSONBody) > 0 {
		request.JSONBody = body
	}
	request.Headers.Del(codexTurnStateHeader)
	return nil
}

func (m *responsesCompactionRecoveryMiddleware) unifiedViewScope(key responsesCompactionRecoveryKey, request *httpclient.Request) responsesCompactionRecoveryKey {
	channel := m.outbound.GetCurrentChannel()
	if channel != nil && channel.Credentials.IsOAuth() && request != nil {
		if account := request.Headers.Get("Chatgpt-Account-Id"); account != "" {
			// These views contain no provider-private state. Refreshing the access
			// token of the same account must not discard its accepted text prefix.
			key.scope.provider.credential = sha256.Sum256([]byte("unified-view-oauth-account:" + account))
		}
	}
	return key
}

func (m *responsesCompactionRecoveryMiddleware) prepareUnifiedView(key responsesCompactionRecoveryKey, request *httpclient.Request) {
	if !m.adapter.unifiedHistoryEnabled() {
		return
	}
	keys, err := unifiedViewKeys(m.unifiedViewScope(key, request), rawRequestPayload(m.outbound.state.RawRequest))
	if err == nil && len(keys) > 0 {
		m.pendingView = keys[len(keys)-1]
	}
}

func portableUnifiedView(body []byte) bool {
	_, input, err := decodeResponsesInput(body)
	if err != nil {
		return false
	}
	_, opaque, portable := unifiedPortableItems(input)
	return portable && opaque == 0
}

func (m *responsesCompactionRecoveryMiddleware) confirmUnifiedView(ctx context.Context) {
	alias := m.pendingView
	m.pendingView = ""
	request := m.outbound.state.RawProviderRequest
	if alias == "" || request == nil || gjson.GetBytes(request.Body, "previous_response_id").String() != "" ||
		gjson.GetBytes(request.Body, `input.#(type=="compaction_trigger")`).Exists() || !portableUnifiedView(request.Body) {
		return
	}
	// Completion is the publication boundary. Canceled/failed preparations are
	// never recorded as accepted contexts; no model work continues after cancel.
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := m.adapter.saveHistorySnapshot(persistCtx, alias, request.Body, nil); err != nil {
		log.Warn(ctx, "could not retain accepted unified history view", log.Cause(err))
	}
}
