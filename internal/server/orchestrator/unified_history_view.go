package orchestrator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
 "fmt"
	"time"

	"github.com/tidwall/gjson"
 "github.com/tidwall/sjson"

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

// A completed status and the dedicated agent-message encryption hint are
// transport metadata rewritten by our own Responses pipeline. Preserve every
// content field, call ID, opaque value and unknown/incomplete status.
func unifiedViewItemIdentity(raw json.RawMessage) (string, error) {
 var err error
 switch rawInputItemType(raw) {
 case "function_call", "custom_tool_call", "function_call_output", "custom_tool_call_output", "reasoning":
  if gjson.GetBytes(raw, "status").String() == "completed" {
   raw, err = sjson.DeleteBytes(raw, "status")
   if err != nil { return "", err }
  }
 case "additional_tools":
  for i, namespace := range gjson.GetBytes(raw, "tools").Array() {
   if namespace.Get("type").String() != "namespace" { continue }
   for j, tool := range namespace.Get("tools").Array() {
    flag := tool.Get("parameters.properties.message.encrypted")
    if tool.Get("type").String() != "function" || !responsesAgentTool(namespace.Get("name").String(), tool.Get("name").String()) ||
     tool.Get("parameters.properties.message.type").String() != "string" || (flag.Type != gjson.True && flag.Type != gjson.False) { continue }
    raw, err = sjson.SetBytes(raw, fmt.Sprintf("tools.%d.tools.%d.parameters.properties.message.encrypted", i, j), false)
    if err != nil { return "", err }
   }
  }
 }
 return unifiedRetainedIdentity(raw)
}

func unifiedViewKeys(key responsesCompactionRecoveryKey, body []byte) ([]string, error) {
 // A portable accepted view does not depend on a connection-local window.
 // Exact source prefixes still isolate edited histories and sibling branches.
 key.scope.windowID = ""
 return unifiedViewKeysVersion(key, body, false)
}

func unifiedViewKeysVersion(key responsesCompactionRecoveryKey, body []byte, legacy bool) ([]string, error) {

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
	version := "unified-accepted-view-v2:"
 if legacy { version = "unified-accepted-view-v1:" }
 hash.Write([]byte(version))
	hash.Write(scope[:])
	hash.Write(canonical)
	keys := make([]string, len(input))
	for i, item := range input {
		identityFn := unifiedViewItemIdentity
  if legacy { identityFn = unifiedRetainedIdentity }
  identity, err := identityFn(item)
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
		left, err := unifiedViewItemIdentity(item)
		if err != nil {
			return nil, false, err
		}
		right, err := unifiedViewItemIdentity(source[i])
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
 view, covered, err := m.loadUnifiedView(ctx, keys)
 if err != nil { return false, err }
 upgrade := covered == 0
 if covered == 0 {
  legacyKeys, err := unifiedViewKeysVersion(key, original, true)
  if err != nil { return false, err }
  view, covered, err = m.loadUnifiedView(ctx, legacyKeys)
  if err != nil { return false, err }
 }
 if covered == 0 {
  view, covered, err = m.importUnifiedView(ctx, request, key, keys, original)
  if err != nil || covered == 0 { return false, err }
 }
 body, matched, err := appendUnifiedView(request.Body, original, view, covered)
 if err != nil || !matched { return false, err }
 if err := m.applyUnifiedView(ctx, request, body); err != nil { return false, err }
 // Upgrade a verified accepted view, never replace its text or source archive.
 if upgrade {
  if err := m.adapter.saveHistorySnapshot(ctx, keys[covered-1], view, nil); err != nil { return false, err }
 }
 log.Info(ctx, "reused accepted unified history view", log.Int("covered_items", covered))
 return true, nil
}

func (m *responsesCompactionRecoveryMiddleware) loadUnifiedView(ctx context.Context, keys []string) ([]byte, int, error) {
 // Newest matching prefix first, in bounded batches.
 for end := len(keys); end > 0; {
  start := max(0, end-128)
  entries, err := m.adapter.systemService.LoadUnifiedHistory(ctx, keys[start:end])
  if err != nil { return nil, 0, err }
  for i := end-1; i >= start; i-- {
   if entries[keys[i]] == "" { continue }
   view, _, found, err := m.adapter.loadHistorySnapshot(ctx, keys[i])
   if err != nil { return nil, 0, err }
   if found { return view, i+1, nil }
  }
  end = start
 }
 return nil, 0, nil
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
