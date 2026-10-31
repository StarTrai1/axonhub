package orchestrator

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	entchannel "github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

// Error adapters classify an explicit loss of the server session. The lifecycle
// below is independent of provider-specific numeric codes and message wording.
var (
	responsesSessionInvalidatedPrefix  = regexp.MustCompile(`(?i)^(?:the )?(?:session|conversation)(?: binding)? (?:has expired|has been invalidated|(?:is )?expired|(?:is )?invalidated)(?:[:.;]|$)`)
	responsesSessionRestartInstruction = regexp.MustCompile(`(?i)\b(?:restart|start|create|use|retry|begin)\b.{0,160}\b(?:new|fresh) (?:session|conversation)\b`)
)

func isResponsesSessionBindingInvalidation(err error) bool {
	var raw *httpclient.Error
	var response *llm.ResponseError
	var code, message, param string
	var status int
	switch {
	case errors.As(err, &raw):
		status = raw.StatusCode
		code, message, param = gjson.GetBytes(raw.Body, "error.code").String(), gjson.GetBytes(raw.Body, "error.message").String(), gjson.GetBytes(raw.Body, "error.param").String()
	case errors.As(err, &response):
		status = response.StatusCode
		code, message, param = response.Detail.Code, response.Detail.Message, response.Detail.Param
	default:
		return false
	}
	if status != http.StatusBadRequest && status != http.StatusNotFound && status != http.StatusConflict && status != http.StatusGone {
		return false
	}
	if param != "" && param != "session_id" && param != "conversation_id" {
		return false
	}
	switch code {
	case "session_expired", "session_invalidated", "session_binding_invalidated", "session_not_found", "invalid_session_id", "conversation_expired", "conversation_invalidated", "conversation_not_found":
		return true
	case "", "bad_request", "invalid_request_error":
	default:
		// A numeric relay code is opaque. It supplies no recovery authority: the
		// explicit session-loss and restart instruction must both be present.
		for _, digit := range code {
			if digit < '0' || digit > '9' {
				return false
			}
		}
	}
	return (status == http.StatusConflict || status == http.StatusGone) && len(message) <= 1024 &&
		responsesSessionInvalidatedPrefix.MatchString(message) && responsesSessionRestartInstruction.MatchString(message) &&
		!strings.ContainsAny(message, "\r\n")
}

type responsesBindingAttempt struct {
	scope [sha256.Size]byte
	alias string
}

type responsesSessionBindingMiddleware struct {
	pipeline.DummyMiddleware
	outbound *PersistentOutboundTransformer
	service  *biz.SystemService
	attempt  *responsesBindingAttempt
	renewed  map[[sha256.Size]byte]bool
}

func recoverResponsesSessionBinding(outbound *PersistentOutboundTransformer, service *biz.SystemService) pipeline.Middleware {
	return &responsesSessionBindingMiddleware{outbound: outbound, service: service, renewed: make(map[[sha256.Size]byte]bool)}
}

func (m *responsesSessionBindingMiddleware) Name() string { return "responses-session-binding" }

func responsesSessionBindingScope(ctx context.Context, channel *biz.Channel, request, original *httpclient.Request) ([sha256.Size]byte, bool) {
	if channel == nil || (channel.Type != entchannel.TypeCodex && channel.Type != entchannel.TypeOpenaiResponses) || channel.Credentials.IsOAuth() || request == nil ||
		request.APIFormat != string(llm.APIFormatOpenAIResponse) {
		return [sha256.Size]byte{}, false
	}
	destination, err := url.Parse(request.URL)
	if err != nil || destination.Hostname() == "" || strings.EqualFold(destination.Hostname(), "chatgpt.com") || strings.EqualFold(destination.Hostname(), "api.openai.com") {
		return [sha256.Size]byte{}, false
	}
	owner, ok := shared.GetSessionScope(ctx)
	if original == nil {
		return [sha256.Size]byte{}, false
	}
	identity := shared.ReadCodexRequestMetadata(original.Headers, original.Body)
	// Fabricated provider session headers can be random for body-only clients.
	// Only logical client identity may seed the stable transport namespace.
	if identity.ThreadID == "" {
		identity.ThreadID = gjson.GetBytes(original.Body, "prompt_cache_key").String()
	}
	if identity.SessionID == "" {
		identity.SessionID = identity.ThreadID
	}
	if identity.ThreadID == "" {
		identity.ThreadID = identity.SessionID
	}
	if !ok || owner == "" || identity.SessionID == "" || identity.ThreadID == "" ||
		request.Headers.Get("Authorization") == "" && (request.Auth == nil || request.Auth.APIKey == "") {
		return [sha256.Size]byte{}, false
	}
	provider := responsesMetadataKey(channel.ID, request)
	if provider.model == "" {
		return [sha256.Size]byte{}, false
	}
	encoded, err := json.Marshal([]any{
		"responses-session-binding-v1", owner, provider.channelID, provider.url, provider.model,
		historyReaderCredential(request, false), identity.SessionID, identity.ThreadID,
	})
	return sha256.Sum256(encoded), err == nil
}

func (m *responsesSessionBindingMiddleware) OnOutboundRawRequest(ctx context.Context, request *httpclient.Request) (*httpclient.Request, error) {
	m.attempt = nil
	m.outbound.state.responsesBindingOriginalBody = nil
	original := m.outbound.state.RawRequest
	if m.outbound.state.LlmRequest != nil && m.outbound.state.LlmRequest.RawRequest != nil {
		original = m.outbound.state.LlmRequest.RawRequest
	}
	scope, eligible := responsesSessionBindingScope(ctx, m.outbound.GetCurrentChannel(), request, original)
	if !eligible {
		return request, nil
	}
	alias, err := m.service.LoadResponsesSessionBinding(ctx, scope)
	if err != nil {
		return nil, fmt.Errorf("load Responses session binding: %w", err)
	}
	m.attempt = &responsesBindingAttempt{scope: scope, alias: alias}
	wireAlias := alias
	if wireAlias == "" {
		// Namespace the transport session from its first request. An unchanged
		// route/model/caller reuses this deterministic ID without a database write.
		wireAlias = deriveCodexIdentityUUID("axonhub-upstream-session-v1:", fmt.Sprintf("%x", scope))
	}
	// A server-only previous response cannot be replayed on a new binding.
	// The normal continuation layer must authenticate and materialize it first.
	if _, safe := responsesResourceHistorySupportsRecoveryWithAgents(request.Body, true); !safe {
		if alias == "" {
			// Legacy server-only state and prewarm frames cannot establish a
			// portable binding. Preserve their existing transport unchanged.
			m.attempt = nil
			return request, nil
		}
		return nil, shared.ResponsesRecoveryError()
	}
	rewritten, err := applyResponsesSessionAlias(request, wireAlias)
	if err != nil {
		return nil, err
	}
	m.outbound.state.responsesBindingOriginalBody = request.Body
	return rewritten, nil
}

func (m *responsesSessionBindingMiddleware) OnOutboundRawError(ctx context.Context, err error) {
	if !isResponsesSessionBindingInvalidation(err) {
		return
	}
	state := m.outbound.state
	state.responsesRejectedStatusRetryChannel = 0
	if ctx.Err() != nil || m.attempt == nil || m.renewed[m.attempt.scope] || state.RawProviderRequest == nil {
		return
	}
	if _, safe := responsesResourceHistorySupportsRecoveryWithAgents(state.RawProviderRequest.Body, true); !safe {
		return
	}
	// Validate all identity carriers before persisting a change. The input and
	// opaque state are never rewritten by this transport-only recovery.
	if _, rewriteErr := applyResponsesSessionAlias(state.RawProviderRequest, "00000000-0000-4000-8000-000000000001"); rewriteErr != nil {
		return
	}
	m.renewed[m.attempt.scope] = true
	if _, saveErr := m.service.RenewResponsesSessionBinding(ctx, m.attempt.scope, m.attempt.alias); saveErr != nil {
		log.Warn(ctx, "could not renew invalidated Responses session binding", log.Cause(saveErr))
		return
	}
	state.responsesRejectedStatusRetryChannel = m.outbound.GetCurrentChannel().ID
	log.Info(ctx, "upstream session binding invalidated; scheduling one scoped renewal", log.Int("channel_id", m.outbound.GetCurrentChannel().ID))
}

func applyResponsesSessionAlias(request *httpclient.Request, alias string) (*httpclient.Request, error) {
	result := *request
	result.Headers = request.Headers.Clone()
	if result.Headers == nil {
		result.Headers = make(http.Header)
	}
	result.Body = append([]byte(nil), request.Body...)
	// Per-frame metadata is newer than a WebSocket's prewarm handshake.
	identity := shared.ReadCodexRequestMetadata(nil, request.Body)
	if identity.WindowID == "" {
		identity = shared.ReadCodexRequestMetadata(request.Headers, request.Body)
	}
	window := alias + ":0"
	if _, suffix, found := strings.Cut(identity.WindowID, ":"); found {
		window = alias + ":" + suffix
	} else if identity.WindowID != "" {
		window = deriveCodexIdentityUUID(alias, ":window:", identity.WindowID)
	}
	fields := map[string]any{"session_id": alias, "thread_id": alias, "window_id": window}
	turn := gjson.GetBytes(request.Body, "client_metadata.x-codex-turn-metadata").String()
	if turn == "" {
		turn = request.Headers.Get(codexTurnMetadataHeader)
	}
	if turn != "" {
		rewritten, ok := rewriteCodexTurnMetadata(turn, fields)
		if !ok {
			return nil, fmt.Errorf("invalid Responses session turn metadata")
		}
		result.Headers.Set(codexTurnMetadataHeader, rewritten)
		if gjson.GetBytes(result.Body, "client_metadata.x-codex-turn-metadata").Exists() {
			var err error
			result.Body, err = sjson.SetBytes(result.Body, "client_metadata.x-codex-turn-metadata", rewritten)
			if err != nil {
				return nil, err
			}
		}
	}
	for _, header := range []string{codexSessionIDHeader, codexThreadIDHeader, "Conversation_id"} {
		result.Headers.Set(header, alias)
	}
	for _, header := range []string{"Session_id", "Thread_id", "Conversation-Id", codexClientRequestIDHeader} {
		if result.Headers.Get(header) != "" {
			result.Headers.Set(header, alias)
		}
	}
	result.Headers.Set(codexWindowIDHeader, window)
	result.Headers.Del(codexTurnStateHeader)
	cacheKey := gjson.GetBytes(result.Body, "prompt_cache_key").String()
	headerIdentity := shared.ReadCodexRequestMetadata(request.Headers, request.Body)
	values := map[string]string{"client_metadata.session_id": alias, "client_metadata.thread_id": alias, "client_metadata.x-codex-window-id": window}
	// An independent operator-selected cache key is not a session identity.
	// Remap only keys that actually carry the original session or thread.
	if cacheKey != "" && (cacheKey == identity.ThreadID || cacheKey == identity.SessionID || cacheKey == headerIdentity.ThreadID || cacheKey == headerIdentity.SessionID) {
		values["prompt_cache_key"] = alias
	}
	for path, value := range values {
		if gjson.GetBytes(result.Body, path).Exists() {
			var err error
			result.Body, err = sjson.SetBytes(result.Body, path, value)
			if err != nil {
				return nil, err
			}
		}
	}
	if len(result.JSONBody) > 0 {
		result.JSONBody = result.Body
	}
	return &result, nil
}

// The provider sees its alias, while immutable archives and downstream delta
// lookups keep the original logical identity and the final submitted input.
func responsesLogicalSessionBody(state *PersistenceState, body []byte) []byte {
	if state.responsesBindingOriginalBody == nil {
		return body
	}
	for _, path := range []string{"client_metadata", "prompt_cache_key"} {
		original := gjson.GetBytes(state.responsesBindingOriginalBody, path)
		if original.Exists() {
			restored, err := sjson.SetRawBytes(body, path, []byte(original.Raw))
			if err == nil {
				body = restored
			}
		}
	}
	return body
}
