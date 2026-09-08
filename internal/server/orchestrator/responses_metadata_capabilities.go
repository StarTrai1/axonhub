package orchestrator

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"sync"
	"time"

	"github.com/samber/lo"
	"github.com/tidwall/gjson"

	lru "github.com/hashicorp/golang-lru/v2"

	"github.com/looplj/axonhub/internal/log"
)

const (
	responsesMetadataLookupTTL      = time.Minute
	responsesMetadataStorageTimeout = 2 * time.Second
)

var (
	responsesMetadataLookupMisses = lo.Must(lru.New[responsesMetadataCapabilityKey, time.Time](1024))
	responsesMetadataCacheMu      sync.Mutex
)

func (key responsesMetadataCapabilityKey) digest() [sha256.Size]byte {
	encoded := lo.Must(json.Marshal([]any{key.channelID, key.url, key.model, key.credential}))
	return sha256.Sum256(encoded)
}

func rememberResponsesMetadataRejection(key responsesMetadataCapabilityKey, expiresAt time.Time) {
	responsesMetadataCacheMu.Lock()
	defer responsesMetadataCacheMu.Unlock()

	if current, found := rejectedResponsesMetadata.Peek(key); !found || expiresAt.After(current) {
		rejectedResponsesMetadata.Add(key, expiresAt)
	}
	responsesMetadataLookupMisses.Remove(key)
}

func (middleware *responsesRejectedStatusCompatibilityMiddleware) hasMetadataRejection(
	ctx context.Context,
	key responsesMetadataCapabilityKey,
	body []byte,
) bool {
	now := time.Now()
	if expiresAt, found := rejectedResponsesMetadata.Get(key); found && now.Before(expiresAt) {
		return true
	}
	if middleware.outbound.state.SystemService == nil {
		return false
	}
	if retryAfter, found := responsesMetadataLookupMisses.Get(key); found && now.Before(retryAfter) {
		return false
	}
	if !hasResponsesInternalMetadata(body) {
		return false
	}

	lookupCtx, cancel := context.WithTimeout(ctx, responsesMetadataStorageTimeout)
	defer cancel()
	expiresAt, err := middleware.outbound.state.SystemService.LoadResponsesMetadataRejection(lookupCtx, key.digest())
	if err != nil {
		if ctx.Err() == nil {
			responsesMetadataLookupMisses.Add(key, time.Now().Add(responsesMetadataLookupTTL))
			log.Warn(ctx, "failed to restore Responses metadata compatibility; retaining native metadata", log.Cause(err))
		}
		return false
	}
	if !time.Now().Before(expiresAt) {
		responsesMetadataLookupMisses.Add(key, time.Now().Add(responsesMetadataLookupTTL))
		return false
	}
	rememberResponsesMetadataRejection(key, expiresAt)
	return true
}

func (middleware *responsesRejectedStatusCompatibilityMiddleware) persistMetadataRejection(
	ctx context.Context,
	key responsesMetadataCapabilityKey,
) {
	expiresAt := time.Now().Add(responsesMetadataCapabilityTTL)
	rememberResponsesMetadataRejection(key, expiresAt)
	if middleware.outbound.state.SystemService == nil {
		return
	}

	persistCtx, cancel := context.WithTimeout(ctx, responsesMetadataStorageTimeout)
	defer cancel()
	if err := middleware.outbound.state.SystemService.SaveResponsesMetadataRejection(persistCtx, key.digest(), expiresAt); err != nil {
		log.Warn(ctx, "failed to persist Responses metadata compatibility; retaining in-memory recovery", log.Cause(err))
	}
}

func hasResponsesInternalMetadata(body []byte) bool {
	found := false
	gjson.GetBytes(body, "input").ForEach(func(_, item gjson.Result) bool {
		if item.Get("type").String() == "message" && item.Get("internal_chat_message_metadata_passthrough").Exists() {
			found = true
			return false
		}
		return true
	})
	return found
}
