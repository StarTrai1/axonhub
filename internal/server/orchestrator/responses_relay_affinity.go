package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	lru "github.com/hashicorp/golang-lru/v2"

	entchannel "github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/streams"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

const responsesRelayAffinityTTL = 2 * time.Hour

type responsesRelayAffinityKey struct {
	provider  responsesMetadataCapabilityKey
	updatedAt time.Time
	apiKeyID  int
	projectID int
	sessionID string
	threadID  string
}

type responsesRelayAffinity struct {
	id        string
	expiresAt time.Time
}

var responsesRelayAffinities = lo.Must(lru.New[responsesRelayAffinityKey, responsesRelayAffinity](2048))

var responsesRelayStickyRoutePattern = regexp.MustCompile(`\(([0-9]+),([0-9]+)\)`)

// A relay may keep sending an old conversation to an exhausted internal route
// while new conversations succeed. A failed, explicitly sticky route permits
// one new upstream affinity, not removal of any conversation content. The
// ordinary retry budget and downstream thread identity remain authoritative.
func recoverResponsesRelayAffinity(outbound *PersistentOutboundTransformer) pipeline.Middleware {
	return &responsesRelayAffinityMiddleware{outbound: outbound}
}

// Apply a previously completed migration before checkpoint recovery computes
// its destination scope. A new migration remains after checkpoint expansion,
// where its full-history precondition can be checked without losing context.
func prepareResponsesRelayAffinity(outbound *PersistentOutboundTransformer) pipeline.Middleware {
	middleware := &responsesRelayAffinityMiddleware{outbound: outbound}
	return pipeline.OnRawRequest("responses-confirmed-relay-affinity", func(_ context.Context, request *httpclient.Request) (*httpclient.Request, error) {
		key, ok := middleware.scope(request)
		if !ok {
			return request, nil
		}
		affinity, found := responsesRelayAffinities.Get(key)
		if !found || !time.Now().Before(affinity.expiresAt) {
			return request, nil
		}
		updated, err := applyResponsesRelayAffinity(request, affinity.id)
		if err == nil {
			outbound.state.responsesRelayAffinityApplied = true
		}
		return updated, err
	})
}

type responsesRelayAffinityMiddleware struct {
	pipeline.DummyMiddleware

	outbound *PersistentOutboundTransformer
	pending  map[responsesRelayAffinityKey]responsesRelayAffinity
	applied  *responsesRelayAffinityKey
	current  responsesRelayAffinity
}

func (m *responsesRelayAffinityMiddleware) Name() string { return "responses-relay-affinity-recovery" }

func (m *responsesRelayAffinityMiddleware) scope(request *httpclient.Request) (responsesRelayAffinityKey, bool) {
	if m.outbound == nil || m.outbound.state == nil || request == nil ||
		request.APIFormat != string(llm.APIFormatOpenAIResponse) {
		return responsesRelayAffinityKey{}, false
	}
	state := m.outbound.state
	channel := m.outbound.GetCurrentChannel()
	if channel == nil || channel.Type != entchannel.TypeCodex || channel.Credentials.IsOAuth() ||
		state.APIKey == nil || state.APIKey.ID <= 0 || state.APIKey.ProjectID <= 0 || state.RawRequest == nil {
		return responsesRelayAffinityKey{}, false
	}
	destination, err := url.Parse(request.URL)
	if err != nil {
		return responsesRelayAffinityKey{}, false
	}
	host := strings.ToLower(destination.Hostname())
	if host == "" || host == "chatgpt.com" || strings.HasSuffix(host, ".chatgpt.com") ||
		host == "openai.com" || strings.HasSuffix(host, ".openai.com") {
		return responsesRelayAffinityKey{}, false
	}
	client := shared.ReadCodexRequestMetadata(state.RawRequest.Headers, rawRequestPayload(state.RawRequest))
	if client.ThreadID == "" || len(client.ThreadID) > 512 || len(client.SessionID) > 512 {
		return responsesRelayAffinityKey{}, false
	}
	key := responsesRelayAffinityKey{
		provider: responsesMetadataKey(channel.ID, request), updatedAt: channel.UpdatedAt,
		apiKeyID: state.APIKey.ID, projectID: state.APIKey.ProjectID,
		sessionID: client.SessionID, threadID: client.ThreadID,
	}
	return key, key.provider.model != ""
}

func responsesRelayAffinityReplayable(body []byte) bool {
	// Full input is essential: server-side references cannot be moved to a new
	// session. Opaque agent messages are allowed only because they stay intact.
	if _, explicit := responsesHistorySupportsRecoveryWithAgents(body, false, true); !explicit {
		return false
	}
	if _, materialized := responsesResourceHistorySupportsRecoveryWithAgents(body, true); !materialized {
		return false
	}
	return true
}

func responsesRelayAffinityHistory(body []byte) bool {
	if !responsesRelayAffinityReplayable(body) {
		return false
	}
	for _, item := range gjson.GetBytes(body, "input").Array() {
		if item.Get("role").String() == "assistant" || item.Get("type").String() == "reasoning" ||
			item.Get("type").String() == "function_call" || item.Get("type").String() == "custom_tool_call" {
			return true
		}
	}
	return false
}

func (m *responsesRelayAffinityMiddleware) OnOutboundRawRequest(ctx context.Context, request *httpclient.Request) (*httpclient.Request, error) {
	m.applied = nil
	key, ok := m.scope(request)
	if !ok {
		return request, nil
	}
	affinity, found := m.pending[key]
	if found && !responsesRelayAffinityReplayable(request.Body) {
		return request, nil
	}
	if !found {
		affinity, found = responsesRelayAffinities.Get(key)
		if found && !time.Now().Before(affinity.expiresAt) {
			responsesRelayAffinities.Remove(key)
			found = false
		}
	}
	if !found {
		return request, nil
	}
	// A confirmed alias keeps the already established upstream session for
	// later native checkpoints or deltas; it is not a fresh history migration.
	rewritten, err := applyResponsesRelayAffinity(request, affinity.id)
	if err != nil {
		return nil, err
	}
	m.applied, m.current = &key, affinity
	m.outbound.state.responsesRelayAffinityApplied = true
	log.Debug(ctx, "using recovered upstream Responses affinity", log.Int("channel_id", key.provider.channelID))
	return rewritten, nil
}

func (m *responsesRelayAffinityMiddleware) OnOutboundRawError(ctx context.Context, err error) {
	m.applied = nil
	if ctx.Err() != nil || m.outbound == nil || m.outbound.state == nil {
		return
	}
	state := m.outbound.state
	state.responsesRelayAffinityRetryChannel = 0
	state.responsesRelayAffinityExhaustedChannel = 0
	key, ok := m.scope(state.RawProviderRequest)
	if !ok || !responsesRelayAffinityHistory(state.RawProviderRequest.Body) || !responsesRelayStickyOverload(err, key.provider.model) {
		return
	}
	if _, attempted := m.pending[key]; attempted {
		// Repeating the same exhausted route with the same new identity cannot
		// recover. Allow the normal alternative-channel path, without burning
		// the rest of the same-channel budget on identical requests.
		state.responsesRelayAffinityExhaustedChannel = key.provider.channelID
		return
	}
	if m.pending == nil {
		m.pending = make(map[responsesRelayAffinityKey]responsesRelayAffinity)
	}
	m.pending[key] = responsesRelayAffinity{id: uuid.NewString()}
	state.responsesRelayAffinityRetryChannel = key.provider.channelID
	log.Info(ctx, "sticky relay route overloaded; scheduling one Responses affinity recovery",
		log.Int("channel_id", key.provider.channelID))
}

func responsesRelayStickyOverload(err error, model string) bool {
	var failure *httpclient.Error
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || !canRetryTransientRateLimit(err) ||
		!errors.As(err, &failure) || (failure.StatusCode != http.StatusInternalServerError && failure.StatusCode != http.StatusServiceUnavailable) {
		return false
	}
	// Some relays use 500 for capacity errors, but their explicit wait hint has
	// the same meaning as a 503. Do not rotate around a long cooldown.
	capacityFailure := *failure
	capacityFailure.StatusCode = http.StatusServiceUnavailable
	if !canRetryTransientRateLimit(&capacityFailure) {
		return false
	}
	// Repeated (selected,selected) pairs are the relay's explicit sticky-route
	// diagnostic. A generic 500/503 without it is not evidence of stale affinity.
	route := strings.TrimSpace(failure.Headers.Get("X-New-Api-Routed-Channel-Id"))
	matches := responsesRelayStickyRoutePattern.FindAllStringSubmatch(route, -1)
	if len(matches) == 0 || len(matches) > 8 {
		return false
	}
	parts := make([]string, 0, len(matches))
	for _, match := range matches {
		if match[1] != match[2] {
			return false
		}
		parts = append(parts, match[0])
	}
	if strings.Join(parts, ",") != route {
		return false
	}
	message := strings.TrimSpace(gjson.GetBytes(failure.Body, "error.message").String())
	if prefix, _, found := strings.Cut(message, " (request id: "); found {
		message = prefix
	}
	return message == "当前模型 "+model+" 负载已经达到上限，请稍后重试" ||
		strings.TrimSuffix(strings.ReplaceAll(message, "’", "'"), ".") == "We're currently experiencing high demand, which may cause temporary errors"
}

func applyResponsesRelayAffinity(request *httpclient.Request, affinity string) (*httpclient.Request, error) {
	updated := *request
	updated.Headers = request.Headers.Clone()
	if updated.Headers == nil {
		updated.Headers = make(http.Header)
	}
	metadata := shared.ReadCodexRequestMetadata(request.Headers, request.Body)
	window := affinity + ":0"
	if _, suffix, found := strings.Cut(metadata.WindowID, ":"); found {
		window = affinity + ":" + suffix
	}
	for _, header := range []string{"Session_id", "Session-Id", "Thread_id", "Thread-Id", "Conversation_id", "Conversation-Id", "X-Client-Request-Id"} {
		if updated.Headers.Get(header) != "" || header == "Session-Id" || header == "Thread-Id" {
			updated.Headers.Set(header, affinity)
		}
	}
	updated.Headers.Set(codexWindowIDHeader, window)
	updated.Headers.Del(codexTurnStateHeader)
	updated.Headers.Del("X-Codex-Routing-Hint")
	fields := map[string]any{"session_id": affinity, "thread_id": affinity, "window_id": window}
	if raw := updated.Headers.Get(codexTurnMetadataHeader); raw != "" {
		if rewritten, ok := rewriteCodexTurnMetadata(raw, fields); ok {
			updated.Headers.Set(codexTurnMetadataHeader, rewritten)
		} else {
			return nil, errors.New("cannot recover relay affinity with malformed Codex turn metadata")
		}
	}
	body := request.Body
	patches := map[string]string{"prompt_cache_key": affinity}
	if gjson.GetBytes(body, "client_metadata").IsObject() {
		patches["client_metadata.session_id"] = affinity
		patches["client_metadata.thread_id"] = affinity
		patches["client_metadata.x-codex-window-id"] = window
		if raw := gjson.GetBytes(body, "client_metadata.x-codex-turn-metadata").String(); raw != "" {
			rewritten, ok := rewriteCodexTurnMetadata(raw, fields)
			if !ok {
				return nil, errors.New("cannot recover relay affinity with malformed embedded Codex turn metadata")
			}
			patches["client_metadata.x-codex-turn-metadata"] = rewritten
		}
	}
	for path, value := range patches {
		var err error
		body, err = sjson.SetBytes(body, path, value)
		if err != nil {
			return nil, fmt.Errorf("rewrite Responses relay affinity: %w", err)
		}
	}
	updated.Body = body
	if len(request.JSONBody) > 0 {
		updated.JSONBody = body
	}
	return &updated, nil
}

func (m *responsesRelayAffinityMiddleware) confirm() {
	if m.applied != nil {
		responsesRelayAffinities.Add(*m.applied, responsesRelayAffinity{id: m.current.id, expiresAt: time.Now().Add(responsesRelayAffinityTTL)})
	}
}

func (m *responsesRelayAffinityMiddleware) OnOutboundRawResponse(_ context.Context, response *httpclient.Response) (*httpclient.Response, error) {
	if response != nil && response.StatusCode >= 200 && response.StatusCode < 300 && responsesReasoningRecoverySucceeded(response.Body) {
		m.confirm()
	}
	return response, nil
}

func (m *responsesRelayAffinityMiddleware) OnOutboundRawStream(_ context.Context, stream streams.Stream[*httpclient.StreamEvent]) (streams.Stream[*httpclient.StreamEvent], error) {
	if m.applied == nil {
		return stream, nil
	}
	key, affinity := *m.applied, m.current.id
	return &responsesReasoningRecoveryStream{Stream: stream, onSuccess: func() {
		responsesRelayAffinities.Add(key, responsesRelayAffinity{id: affinity, expiresAt: time.Now().Add(responsesRelayAffinityTTL)})
	}}, nil
}

// The upstream alias is transport state, not a new downstream context window.
// Cache the actual submitted input with the client's metadata so that a later
// previous_response_id can still expand within the original Codex window.
func responsesSessionProviderBody(state *PersistenceState) []byte {
	if state == nil || state.RawProviderRequest == nil {
		return nil
	}
	body := state.RawProviderRequest.Body
	if !state.responsesRelayAffinityApplied || state.RawRequest == nil {
		return body
	}
	body, err := biz.ResponsesSessionClientBody(body, rawRequestPayload(state.RawRequest), state.RawRequest.Headers)
	if err != nil {
		return nil
	}
	return body
}
