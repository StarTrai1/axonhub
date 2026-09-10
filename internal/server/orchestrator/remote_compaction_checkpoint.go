package orchestrator

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/llm"
)

const (
	localCompactionSealedReferencePrefix = "axonhub-local-v2."
	localCompactionMaxSummaryBytes       = 4 * 1024 * 1024
)

func newLocalCompactionCipher(secret string) (cipher.AEAD, error) {
	if secret == "" {
		return nil, errors.New("local compaction encryption key is unavailable")
	}
	// Derive a separate key from the installation secret; never use the JWT key
	// directly as an encryption key or expose it in a client reference.
	key := hmac.New(sha256.New, []byte(secret))
	_, _ = key.Write([]byte("axonhub/local-compaction/v2"))
	block, err := aes.NewCipher(key.Sum(nil))
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func (a *remoteCompactionAdapter) localCompactionCipher(ctx context.Context) (cipher.AEAD, error) {
	secret, err := a.systemService.SecretKey(authz.WithSystemBypass(ctx, "local-compaction-encryption-key"))
	if err != nil {
		return nil, fmt.Errorf("load local compaction encryption key: %w", err)
	}
	return newLocalCompactionCipher(secret)
}

func localCompactionAssociatedData(ref *remoteCompactionReference, state *PersistenceState) ([]byte, error) {
	if ref == nil || ref.ID == "" || state == nil || state.APIKey == nil || state.APIKey.ID <= 0 || state.APIKey.ProjectID <= 0 {
		return nil, errors.New("request history is unavailable without an authenticated API key and project")
	}
	return fmt.Appendf(nil, "axonhub/local-compaction/v2:%d:%d:%s", state.APIKey.ProjectID, state.APIKey.ID, ref.ID), nil
}

func sealLocalCompactionSummary(aead cipher.AEAD, associatedData []byte, summary string) (string, error) {
	if aead == nil || len(associatedData) == 0 {
		return "", errors.New("local compaction encryption was not initialized")
	}
	if strings.TrimSpace(summary) == "" || len(summary) > localCompactionMaxSummaryBytes || !utf8.ValidString(summary) {
		return "", errors.New("local compaction summary is empty or exceeds the supported size")
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("create local compaction nonce: %w", err)
	}
	sealed := aead.Seal(nonce, nonce, []byte(summary), associatedData)
	return localCompactionSealedReferencePrefix + base64.RawURLEncoding.EncodeToString(sealed), nil
}

func openLocalCompactionSummary(aead cipher.AEAD, associatedData []byte, sealed string) (string, error) {
	if aead == nil || len(associatedData) == 0 || !strings.HasPrefix(sealed, localCompactionSealedReferencePrefix) {
		return "", invalidLocalCompactionReference()
	}
	encoded := strings.TrimPrefix(sealed, localCompactionSealedReferencePrefix)
	maxBytes := localCompactionMaxSummaryBytes + aead.NonceSize() + aead.Overhead()
	if len(encoded) > base64.RawURLEncoding.EncodedLen(maxBytes) {
		return "", invalidLocalCompactionReference()
	}
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(payload) < aead.NonceSize()+aead.Overhead() {
		return "", invalidLocalCompactionReference()
	}
	nonce := payload[:aead.NonceSize()]
	plaintext, err := aead.Open(nil, nonce, payload[aead.NonceSize():], associatedData)
	if err != nil || !utf8.Valid(plaintext) || len(bytes.TrimSpace(plaintext)) == 0 {
		return "", invalidLocalCompactionReference()
	}
	return string(plaintext), nil
}

func invalidLocalCompactionReference() error {
	return &llm.ResponseError{
		StatusCode: http.StatusBadRequest,
		Detail: llm.ErrorDetail{
			Code:    "invalid_local_compaction_reference",
			Type:    "invalid_request_error",
			Param:   "input",
			Message: "Local compaction reference could not be authenticated for this API key and project",
		},
	}
}

func isSealedLocalCompactionReference(ref *remoteCompactionReference) bool {
	return ref != nil && strings.HasPrefix(ref.EncryptedContent, localCompactionSealedReferencePrefix)
}

func isLegacyLocalCompactionReference(ref *remoteCompactionReference) bool {
	return ref != nil && strings.HasPrefix(ref.EncryptedContent, localCompactionReferencePrefix)
}

func (a *remoteCompactionAdapter) openCompactionCheckpoint(ctx context.Context, ref *remoteCompactionReference, state *PersistenceState, sealed string) (string, error) {
	associatedData, err := localCompactionAssociatedData(ref, state)
	if err != nil {
		return "", err
	}
	aead, err := a.localCompactionCipher(ctx)
	if err != nil {
		return "", err
	}
	return openLocalCompactionSummary(aead, associatedData, sealed)
}

func (a *remoteCompactionAdapter) retainLegacyCompactionSummary(ctx context.Context, cacheKey string, ref *remoteCompactionReference, state *PersistenceState, summary string) error {
	associatedData, err := localCompactionAssociatedData(ref, state)
	if err != nil {
		return err
	}
	aead, err := a.localCompactionCipher(ctx)
	if err != nil {
		return err
	}
	sealed, err := sealLocalCompactionSummary(aead, associatedData, summary)
	if err != nil {
		return err
	}
	return a.systemService.SaveLocalCompactionCheckpoint(ctx, state.APIKey.ProjectID, state.APIKey.ID, cacheKey, sealed)
}

func (g *localCompactionGeneration) sealSummary(summary string) error {
	sealed, err := sealLocalCompactionSummary(g.summaryCipher, g.associatedData, summary)
	if err != nil {
		return err
	}
	g.ref.EncryptedContent = sealed
	return nil
}

// Recover only a verified continuation of the same legacy compaction window.
// The request log contains the expanded summary, not the original opaque item.
// Comparing the complete retained prefix (including tool IDs and arguments)
// prevents borrowing a summary from a different branch or compaction generation.
func retainedLocalCompactionSummary(body []byte, ref *remoteCompactionReference) string {
	return newLocalCompactionReplay(ref).summaryFromBody(body)
}

type localCompactionReplay struct {
	threadID string
	windowID string
	index    int
	items    []json.RawMessage
}

// Parse the potentially large live history once per lookup, not once for every
// retained request examined after a cold start.
func newLocalCompactionReplay(ref *remoteCompactionReference) *localCompactionReplay {
	if !isLegacyLocalCompactionReference(ref) || len(ref.requestBody) == 0 {
		return nil
	}
	currentEnvelope, current, err := decodeResponsesInput(ref.requestBody)
	if err != nil || ref.Index < 0 || ref.Index >= len(current) {
		return nil
	}
	threadID := responseEnvelopeThreadID(currentEnvelope)
	windowID := compactionContextWindowID(currentEnvelope)
	if threadID == "" || windowID == "" {
		return nil
	}
	replay := &localCompactionReplay{threadID: threadID, windowID: windowID, index: ref.Index, items: make([]json.RawMessage, len(current))}
	for index, item := range current {
		if index == ref.Index {
			continue
		}
		if typ := rawInputItemType(item); typ == remoteCompactionItemType || typ == legacyRemoteCompactionSummaryType {
			return nil
		}
		canonical, err := compactionReplayItem(item)
		if err != nil {
			return nil
		}
		replay.items[index] = canonical
	}
	return replay
}

func (r *localCompactionReplay) summaryFromBody(body []byte) string {
	if r == nil {
		return ""
	}
	priorEnvelope, prior, err := decodeResponsesInput(body)
	if err != nil || len(prior) <= r.index+1 || len(prior) > len(r.items) ||
		responseEnvelopeThreadID(priorEnvelope) != r.threadID || compactionContextWindowID(priorEnvelope) != r.windowID {
		return ""
	}
	var item struct {
		Type    string `json:"type"`
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if json.Unmarshal(prior[r.index], &item) != nil || item.Type != "message" || item.Role != "user" || len(item.Content) != 1 || item.Content[0].Type != "input_text" {
		return ""
	}
	prefix := localCompactionSummaryPrefix + "\n"
	if !strings.HasPrefix(item.Content[0].Text, prefix) {
		return ""
	}
	summary := strings.TrimPrefix(item.Content[0].Text, prefix)
	if strings.TrimSpace(summary) == "" {
		return ""
	}
	matchedContinuationID := false
	for index, previous := range prior {
		if index == r.index {
			continue
		}
		previousItem, previousErr := compactionReplayItem(previous)
		if previousErr != nil || !bytes.Equal(previousItem, r.items[index]) {
			return ""
		}
		if index > r.index {
			var identity struct {
				ID     string `json:"id"`
				CallID string `json:"call_id"`
			}
			if json.Unmarshal(previous, &identity) == nil && (identity.ID != "" || identity.CallID != "") {
				matchedContinuationID = true
			}
		}
	}
	if !matchedContinuationID {
		return ""
	}
	return summary
}

func compactionContextWindowID(envelope map[string]json.RawMessage) string {
	var metadata map[string]json.RawMessage
	if json.Unmarshal(envelope["client_metadata"], &metadata) != nil {
		return ""
	}
	var encoded string
	if json.Unmarshal(metadata["x-codex-turn-metadata"], &encoded) != nil {
		return ""
	}
	var turn struct {
		ContextWindowID string `json:"context_window_id"`
	}
	if json.Unmarshal([]byte(encoded), &turn) != nil {
		return ""
	}
	return turn.ContextWindowID
}

func compactionReplayItem(raw json.RawMessage) ([]byte, error) {
	var item map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&item); err != nil {
		return nil, err
	}
	// Client upgrades can change diagnostic execution metadata and serialize an
	// absent reasoning content field as null. No model content or IDs are removed.
	delete(item, "internal_chat_message_metadata_passthrough")
	if item["type"] == "reasoning" && item["content"] == nil {
		delete(item, "content")
	}
	return json.Marshal(item)
}
