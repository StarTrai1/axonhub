package orchestrator

import (
	"context"
	"crypto/sha256"
	"net/http"
	"sync"
	"time"

	"github.com/samber/lo"
	"github.com/tidwall/gjson"

	lru "github.com/hashicorp/golang-lru/v2"

	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

const codexMemoryPauseDuration = 5 * time.Minute

var errCodexMemoryPaused = &llm.ResponseError{
	StatusCode: http.StatusBadRequest,
	Detail: llm.ErrorDetail{
		Code: "codex_memory_paused",
		Type: "invalid_request_error",
		Message: "Background memory consolidation is paused after a foreground failure in this conversation. It resumes after a successful new foreground request or after 5 minutes.",
	},
}

type codexMemoryScope struct {
	projectID, apiKeyID int
	thread             [sha256.Size]byte
}

type codexMemoryPause struct {
	until    time.Time
	sequence uint64
}

// Active registrations live only as long as their requests. The separate,
// bounded failure cache never retains request bodies or credentials.
type codexMemoryGuard struct {
	mu       sync.Mutex
	sequence uint64
	active   map[codexMemoryScope]map[*codexMemoryLease]context.CancelCauseFunc
	paused   *lru.Cache[codexMemoryScope, codexMemoryPause]
	clock    func() time.Time
}

type codexMemoryLease struct {
	once     sync.Once
	guard    *codexMemoryGuard
	scope    codexMemoryScope
	sequence uint64
	memory   bool
}

var codexBackgroundMemory = newCodexMemoryGuard()

func newCodexMemoryGuard() *codexMemoryGuard {
	return &codexMemoryGuard{
		active: make(map[codexMemoryScope]map[*codexMemoryLease]context.CancelCauseFunc),
		paused: lo.Must(lru.New[codexMemoryScope, codexMemoryPause](1024)),
		clock: time.Now,
	}
}

func codexMemoryRequestScope(ctx context.Context, request *httpclient.Request) (codexMemoryScope, bool, bool) {
	apiKey, authenticated := contexts.GetAPIKey(ctx)
	if !shared.IsResponsesAPI(ctx) || request == nil || !authenticated || apiKey.ID <= 0 || apiKey.ProjectID <= 0 {
		return codexMemoryScope{}, false, false
	}
	metadata := shared.ReadCodexRequestMetadata(request.Headers, request.Body)
	turn := metadata.RawTurnMetadata
	// A WebSocket handshake can describe prewarm while this frame is a turn or
	// memory request. Per-request metadata is authoritative for its purpose.
	if embedded := gjson.GetBytes(request.Body, "client_metadata.x-codex-turn-metadata"); embedded.Type == gjson.String && gjson.Valid(embedded.String()) {
		turn = embedded.String()
		metadata.RequestKind = gjson.Get(turn, "request_kind").String()
	}
	if thread := gjson.GetBytes(request.Body, "client_metadata.thread_id").String(); thread != "" {
		metadata.ThreadID = thread
	}
	if metadata.ThreadID == "" || metadata.Subagent != "" {
		return codexMemoryScope{}, false, false
	}
	memory := metadata.RequestKind == "memory"
	if !memory {
		source, agent := gjson.Get(turn, "thread_source").String(), gjson.Get(turn, "agent_name").String()
		if metadata.RequestKind != "turn" || (source != "" && source != "user") || (agent != "" && agent != "/root") {
			return codexMemoryScope{}, false, false
		}
	}
	return codexMemoryScope{projectID: apiKey.ProjectID, apiKeyID: apiKey.ID, thread: sha256.Sum256([]byte(metadata.ThreadID))}, memory, true
}

func (g *codexMemoryGuard) begin(ctx context.Context, request *httpclient.Request, cancel context.CancelCauseFunc) (*codexMemoryLease, error) {
	scope, memory, eligible := codexMemoryRequestScope(ctx, request)
	if !eligible {
		return nil, nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if pause, found := g.paused.Get(scope); found {
		if !g.clock().Before(pause.until) {
			g.paused.Remove(scope)
		} else if memory {
			return nil, errCodexMemoryPaused
		}
	}
	g.sequence++
	lease := &codexMemoryLease{guard: g, scope: scope, memory: memory, sequence: g.sequence}
	if memory {
		if g.active[scope] == nil {
			g.active[scope] = make(map[*codexMemoryLease]context.CancelCauseFunc)
		}
		g.active[scope][lease] = cancel
	}
	return lease, nil
}

func (lease *codexMemoryLease) finish(success bool) {
	if lease == nil {
		return
	}
	lease.once.Do(func() {
		g := lease.guard
		g.mu.Lock()
		var cancelMemory []context.CancelCauseFunc
		switch {
		case lease.memory:
			delete(g.active[lease.scope], lease)
			if len(g.active[lease.scope]) == 0 {
				delete(g.active, lease.scope)
			}
		case success:
			if pause, found := g.paused.Peek(lease.scope); found && lease.sequence > pause.sequence {
				g.paused.Remove(lease.scope)
			}
		default:
			sequence := lease.sequence
			if pause, found := g.paused.Peek(lease.scope); found && pause.sequence > sequence {
				sequence = pause.sequence
			}
			g.paused.Add(lease.scope, codexMemoryPause{until: g.clock().Add(codexMemoryPauseDuration), sequence: sequence})
			for _, cancel := range g.active[lease.scope] {
				cancelMemory = append(cancelMemory, cancel)
			}
		}
		g.mu.Unlock()
		for _, cancel := range cancelMemory {
			cancel(errCodexMemoryPaused)
		}
	})
}
