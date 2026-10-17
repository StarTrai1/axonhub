package biz

import (
	"context"
	"hash/fnv"
	"math/rand/v2"
	"slices"
	"strconv"

	lru "github.com/hashicorp/golang-lru/v2"

	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/llm/auth"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

// traceStickyLRUSize is the default LRU cache size for trace-to-key mappings.
const traceStickyLRUSize = 1024

type channelAPIKeyContextProvider struct {
	inner auth.APIKeyProvider
}

func NewChannelAPIKeyContextProvider(inner auth.APIKeyProvider) auth.APIKeyProvider {
	return &channelAPIKeyContextProvider{inner: inner}
}

func (p *channelAPIKeyContextProvider) Get(ctx context.Context) string {
	key := p.inner.Get(ctx)
	if key != "" {
		contexts.WithChannelAPIKey(ctx, key)
	}
	return key
}

// TraceStickyKeyProvider selects an API key deterministically per trace or session,
// using cached enabled keys from the channel snapshot.
//
// An LRU cache remembers previous traceID→key selections so that, as long as
// the previously chosen key is still enabled, the same key is returned even when
// the enabled-key set changes (e.g. a new key is added). This improves sticky
// stability compared to pure rendezvous hashing alone.
//
//nolint:revive // exported for use in transformers via interface.
type TraceStickyKeyProvider struct {
	channel *Channel
	cache   *lru.Cache[string, string]
}

func NewTraceStickyKeyProvider(channel *Channel) *TraceStickyKeyProvider {
	cache, _ := lru.New[string, string](traceStickyLRUSize)

	return &TraceStickyKeyProvider{
		channel: channel,
		cache:   cache,
	}
}

func (p *TraceStickyKeyProvider) Get(ctx context.Context) string {
	enabled := p.channel.cachedEnabledAPIKeys
	if len(enabled) == 0 {
		return p.channel.Credentials.APIKeys[0]
	}

	if len(enabled) == 1 {
		return enabled[0]
	}

	var selectedKey string

	if seed := channelAPIKeyAffinitySeed(ctx); seed != "" {
		if cached, ok := p.cache.Get(seed); ok && slices.Contains(enabled, cached) {
			selectedKey = cached
		} else {
			selectedKey = rendezvousSelect(enabled, seed)
			p.cache.Add(seed, selectedKey)
		}

		if log.DebugEnabled(ctx) {
			log.Debug(ctx, "Session sticky key selected",
				log.String("key_prefix", safeAPIKeyPrefix(selectedKey)),
			)
		}
	} else {
		//nolint:gosec // not a security issue, just a random selection.
		selectedKey = enabled[rand.IntN(len(enabled))]
		if log.DebugEnabled(ctx) {
			log.Debug(ctx, "Random key selected",
				log.String("key_prefix", safeAPIKeyPrefix(selectedKey)),
			)
		}
	}

	contexts.WithChannelAPIKey(ctx, selectedKey)

	return selectedKey
}

func channelAPIKeyAffinitySeed(ctx context.Context) string {
	// Preserve existing trace assignments across upgrades. Tracing is optional;
	// transport sessions must not rotate credentials merely because it is off.
	if trace, ok := contexts.GetTrace(ctx); ok && trace.TraceID != "" {
		return trace.TraceID
	}
	if thread, ok := contexts.GetThread(ctx); ok && thread.ID != 0 {
		return "thread:" + strconv.Itoa(thread.ID)
	}
	if sessionID, ok := shared.GetSessionID(ctx); ok && sessionID != "" {
		if scope, ok := shared.GetSessionScope(ctx); ok && scope != "" {
			// A trusted owner separates equal client-supplied IDs. NUL separators
			// keep scope/session pairs unambiguous without exposing credentials.
			return "session:" + scope + "\x00" + sessionID
		}
	}
	return ""
}

// rendezvousSelect picks a key using Highest Random Weight (Rendezvous) hashing.
// This is stable when the key set changes (minimal remapping compared to modulo).
func rendezvousSelect(keys []string, seed string) string {
	bestKey := keys[0]
	bestScore := hashAPIKey(seed + "|" + bestKey)

	for i := 1; i < len(keys); i++ {
		k := keys[i]

		s := hashAPIKey(seed + "|" + k)
		if s > bestScore {
			bestScore = s
			bestKey = k
		}
	}

	return bestKey
}

func hashAPIKey(s string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))

	return h.Sum64()
}

func safeAPIKeyPrefix(key string) string {
	if len(key) >= 2 {
		return key[:2]
	}

	return key
}
