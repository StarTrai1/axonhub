package orchestrator

import (
	"context"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/transformer/openai/codex"
)

const (
	codexTurnStateHeader = "X-Codex-Turn-State"
	codexTurnStateTTL    = time.Hour
)

type codexTurnStateOrigin struct {
	channelID int
	expiresAt time.Time
}

// codexTurnStateTracker binds the opaque turn-state returned to a client
// session to the official Codex OAuth channel that minted it. It stores
// provenance only; the sensitive header value remains exclusively between the
// client and upstream.
type codexTurnStateTracker struct {
	origins sync.Map
	writes  atomic.Uint64
	ttl     time.Duration
}

func newCodexTurnStateTracker() *codexTurnStateTracker {
	return &codexTurnStateTracker{ttl: codexTurnStateTTL}
}

func applyCodexTurnStateIsolation(outbound *PersistentOutboundTransformer, tracker *codexTurnStateTracker) pipeline.Middleware {
	return pipeline.OnRawRequest("codex-turn-state-isolation", func(_ context.Context, request *httpclient.Request) (*httpclient.Request, error) {
		if tracker == nil || outbound == nil || outbound.state == nil || request == nil || request.Headers == nil {
			return request, nil
		}
		current := outbound.GetCurrentChannel()
		if current == nil || current.Channel == nil || current.Channel.Type != channel.TypeCodex ||
			!current.Channel.Credentials.IsOAuth() {
			return request, nil
		}
		if request.Headers.Get(codexTurnStateHeader) == "" {
			return request, nil
		}

		key := codexTurnStateSessionKey(outbound.state, request.Headers)
		if key == "" {
			return request, nil
		}
		outbound.state.codexTurnStateSessionKey = key
		origin, ok := tracker.load(key)
		if ok && origin.channelID != current.ID {
			request.Headers.Del(codexTurnStateHeader)
		}

		return request, nil
	})
}

func (t *codexTurnStateTracker) noteSuccessfulResponse(state *PersistenceState, headers http.Header) {
	if t == nil || state == nil || state.RawRequest == nil || state.CurrentCandidate == nil || state.CurrentCandidate.Channel == nil ||
		state.CurrentCandidate.Channel.Channel == nil || state.CurrentCandidate.Channel.Type != channel.TypeCodex ||
		!state.CurrentCandidate.Channel.Credentials.IsOAuth() ||
		headers.Get(codexTurnStateHeader) == "" {
		return
	}

	key := state.codexTurnStateSessionKey
	if key == "" {
		key = codexTurnStateSessionKey(state, state.RawRequest.Headers)
	}
	if key == "" {
		return
	}
	ttl := t.ttl
	if ttl <= 0 {
		ttl = codexTurnStateTTL
	}
	t.origins.Store(key, codexTurnStateOrigin{
		channelID: state.CurrentCandidate.Channel.ID,
		expiresAt: time.Now().Add(ttl),
	})
	if t.writes.Add(1)%256 == 0 {
		t.sweep()
	}
}

func (t *codexTurnStateTracker) load(key string) (codexTurnStateOrigin, bool) {
	raw, ok := t.origins.Load(key)
	if !ok {
		return codexTurnStateOrigin{}, false
	}
	origin, ok := raw.(codexTurnStateOrigin)
	if !ok || (!origin.expiresAt.IsZero() && time.Now().After(origin.expiresAt)) {
		t.origins.Delete(key)
		return codexTurnStateOrigin{}, false
	}

	return origin, true
}

func (t *codexTurnStateTracker) sweep() {
	now := time.Now()
	t.origins.Range(func(key, value any) bool {
		origin, ok := value.(codexTurnStateOrigin)
		if !ok || (!origin.expiresAt.IsZero() && now.After(origin.expiresAt)) {
			t.origins.Delete(key)
		}
		return true
	})
}

func codexTurnStateSessionKey(state *PersistenceState, headers http.Header) string {
	sessionID := codex.GetSessionIDFromHeaders(headers)
	if sessionID == "" {
		return ""
	}
	apiKeyID := 0
	if state != nil && state.APIKey != nil {
		apiKeyID = state.APIKey.ID
	}

	return strconv.Itoa(apiKeyID) + "\x00" + sessionID
}
