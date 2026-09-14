package orchestrator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"maps"
	"sync"
	"time"

	"github.com/samber/lo"
	"github.com/tidwall/gjson"

	lru "github.com/hashicorp/golang-lru/v2"

	entchannel "github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

const (
	responsesReasoningRecoveryTTL       = 6 * time.Hour
	responsesReasoningRecoveryMaxScopes = 256
	responsesReasoningRecoveryMaxHashes = 1024
)

// A successful recovery applies only to the same authenticated owner, thread,
// context window, destination, model, and final upstream credential.
type responsesReasoningRecoveryScope struct {
	provider responsesMetadataCapabilityKey
	owner    [sha256.Size]byte
	threadID string
	windowID string
}

type responsesReasoningRecoveryEntry struct {
	hashes    map[[sha256.Size]byte]struct{}
	expiresAt time.Time
}

type responsesReasoningRecovery struct {
	scope  responsesReasoningRecoveryScope
	hashes map[[sha256.Size]byte]struct{}
}

var (
	responsesReasoningRecoveries = lo.Must(lru.New[responsesReasoningRecoveryScope, responsesReasoningRecoveryEntry](responsesReasoningRecoveryMaxScopes))
	responsesReasoningRecoveryMu sync.Mutex
)

func isResponsesReasoningRecoveryFormat(format string) bool {
	return format == string(llm.APIFormatOpenAIResponse) || format == string(llm.APIFormatOpenAIResponseCompact)
}

func responsesReasoningScope(ctx context.Context, channel *biz.Channel, request *httpclient.Request) (responsesReasoningRecoveryScope, bool) {
	if channel == nil || channel.Type != entchannel.TypeCodex || request == nil || request.URL == "" ||
		!isResponsesReasoningRecoveryFormat(request.APIFormat) || !bytes.Contains(request.Body, []byte("encrypted_content")) {
		return responsesReasoningRecoveryScope{}, false
	}
	owner, ok := shared.GetSessionScope(ctx)
	if !ok || owner == "" {
		return responsesReasoningRecoveryScope{}, false
	}
	identity := shared.ReadCodexRequestMetadata(request.Headers, request.Body)
	if identity.ThreadID == "" || identity.WindowID == "" {
		return responsesReasoningRecoveryScope{}, false
	}
	key := responsesMetadataKey(channel.ID, request)
	if key.model == "" || request.Headers.Get("Authorization") == "" && (request.Auth == nil || request.Auth.APIKey == "") {
		return responsesReasoningRecoveryScope{}, false
	}
	return responsesReasoningRecoveryScope{
		provider: key,
		owner:    sha256.Sum256([]byte(owner)),
		threadID: identity.ThreadID,
		windowID: identity.WindowID,
	}, true
}

func encryptedResponsesReasoningHashes(body []byte) map[[sha256.Size]byte]struct{} {
	var hashes map[[sha256.Size]byte]struct{}
	gjson.GetBytes(body, "input").ForEach(func(_, item gjson.Result) bool {
		value := item.Get("encrypted_content")
		if item.Get("type").String() == "reasoning" && value.Type == gjson.String && value.String() != "" {
			if hashes == nil {
				hashes = make(map[[sha256.Size]byte]struct{})
			}
			hashes[responsesReasoningItemHash(item)] = struct{}{}
		}
		return true
	})
	return hashes
}

func responsesReasoningItemHash(item gjson.Result) [sha256.Size]byte {
	// Bind the content to its item ID without retaining either opaque value.
	encoded := lo.Must(json.Marshal([2]string{item.Get("id").String(), item.Get("encrypted_content").String()}))
	return sha256.Sum256(encoded)
}

func newResponsesReasoningRecovery(scope responsesReasoningRecoveryScope, before, after []byte) *responsesReasoningRecovery {
	hashes := encryptedResponsesReasoningHashes(before)
	for hash := range encryptedResponsesReasoningHashes(after) {
		delete(hashes, hash)
	}
	if len(hashes) == 0 || len(hashes) > responsesReasoningRecoveryMaxHashes {
		return nil
	}
	return &responsesReasoningRecovery{scope: scope, hashes: hashes}
}

func (recovery *responsesReasoningRecovery) confirm() {
	if recovery == nil {
		return
	}
	now := time.Now()
	responsesReasoningRecoveryMu.Lock()
	defer responsesReasoningRecoveryMu.Unlock()

	entry, exists := responsesReasoningRecoveries.Peek(recovery.scope)
	if !exists || !now.Before(entry.expiresAt) {
		entry = responsesReasoningRecoveryEntry{
			hashes:    make(map[[sha256.Size]byte]struct{}),
			expiresAt: now.Add(responsesReasoningRecoveryTTL),
		}
	}
	for hash := range recovery.hashes {
		if len(entry.hashes) >= responsesReasoningRecoveryMaxHashes {
			break
		}
		entry.hashes[hash] = struct{}{}
	}
	// Repeated successful requests do not extend the original absolute TTL.
	responsesReasoningRecoveries.Add(recovery.scope, entry)
}

func rememberedResponsesReasoningRule(scope responsesReasoningRecoveryScope, body []byte) (responsesRejectedStatusRule, bool) {
	responsesReasoningRecoveryMu.Lock()
	entry, found := responsesReasoningRecoveries.Get(scope)
	var knownHashes map[[sha256.Size]byte]struct{}
	if found && time.Now().Before(entry.expiresAt) {
		knownHashes = maps.Clone(entry.hashes)
	} else if found {
		responsesReasoningRecoveries.Remove(scope)
	}
	responsesReasoningRecoveryMu.Unlock()
	if len(knownHashes) == 0 {
		return responsesRejectedStatusRule{}, false
	}
	var matched map[[sha256.Size]byte]struct{}
	for hash := range encryptedResponsesReasoningHashes(body) {
		if _, known := knownHashes[hash]; known {
			if matched == nil {
				matched = make(map[[sha256.Size]byte]struct{})
			}
			matched[hash] = struct{}{}
		}
	}
	if len(matched) == 0 {
		return responsesRejectedStatusRule{}, false
	}
	rule, safe := responsesRejectedReasoningRule(body, "")
	if !safe {
		return responsesRejectedStatusRule{}, false
	}
	rule.reasoningScope = &scope
	rule.reasoningHashes = matched
	return rule, true
}

func (m *responsesRejectedStatusCompatibilityMiddleware) OnOutboundRawResponse(
	_ context.Context,
	response *httpclient.Response,
) (*httpclient.Response, error) {
	if response != nil && response.StatusCode >= 200 && response.StatusCode < 300 &&
		responsesReasoningRecoverySucceeded(response.Body) {
		m.recoveredReasoning.confirm()
	}
	return response, nil
}

func responsesReasoningRecoverySucceeded(body []byte) bool {
	if !gjson.ValidBytes(body) {
		return false
	}
	if value := gjson.GetBytes(body, "error"); value.Exists() && value.Type != gjson.Null {
		return false
	}
	if gjson.GetBytes(body, "status").String() == "completed" {
		return true
	}
	return gjson.GetBytes(body, "object").String() == "response.compaction" &&
		gjson.GetBytes(body, "output").IsArray() && gjson.GetBytes(body, "output.#").Int() > 0
}

func (m *responsesRejectedStatusCompatibilityMiddleware) OnOutboundRawStream(
	_ context.Context,
	stream streams.Stream[*httpclient.StreamEvent],
) (streams.Stream[*httpclient.StreamEvent], error) {
	if m.recoveredReasoning == nil {
		return stream, nil
	}
	return &responsesReasoningRecoveryStream{Stream: stream, recovery: m.recoveredReasoning}, nil
}

type responsesReasoningRecoveryStream struct {
	streams.Stream[*httpclient.StreamEvent]

	recovery  *responsesReasoningRecovery
	current   *httpclient.StreamEvent
	confirmed bool
	failed    bool
}

func (s *responsesReasoningRecoveryStream) Next() bool {
	if !s.Stream.Next() {
		s.current = nil
		return false
	}
	event := s.Stream.Current()
	// Some source streams advance when Current is called. Read once, then
	// expose the same event to downstream transformers without consuming more.
	s.current = event
	if event == nil {
		return true
	}
	eventType := gjson.GetBytes(event.Data, "type").String()
	switch eventType {
	case "error", "response.failed", "response.incomplete", "response.canceled", "response.cancelled":
		s.failed = true
	}
	// Stream errors are only safe to read after exhaustion. A successful
	// terminal event is authoritative even if the transport closes afterward.
	if !s.confirmed && !s.failed && eventType == "response.completed" &&
		responsesReasoningRecoverySucceeded([]byte(gjson.GetBytes(event.Data, "response").Raw)) {
		s.recovery.confirm()
		s.confirmed = true
	}
	return true
}

func (s *responsesReasoningRecoveryStream) Current() *httpclient.StreamEvent {
	return s.current
}
