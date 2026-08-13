package orchestrator

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
)

const (
	codexInstallationIDHeader  = "X-Codex-Installation-Id"
	codexSessionIDHeader       = "Session-Id"
	codexThreadIDHeader        = "Thread-Id"
	codexWindowIDHeader        = "X-Codex-Window-Id"
	codexClientRequestIDHeader = "X-Client-Request-Id"
	codexTurnMetadataHeader    = "X-Codex-Turn-Metadata"
)

type codexIdentityValues struct {
	policy         objects.CodexIdentityPolicy
	installationID string
	sessionID      string
	threadID       string
	turnID         string
	windowID       string
	turnStartedAt  int64
}

func deriveCodexIdentityUUID(parts ...any) string {
	hash := sha256.Sum256(fmt.Append(nil, parts...))
	id := uuid.UUID(hash[:16])
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80

	return id.String()
}

func resolveCodexIdentityValues(
	channelID int,
	accountID string,
	clientSessionID string,
	clientThreadID string,
	policy objects.CodexIdentityPolicy,
) *codexIdentityValues {
	if policy == objects.CodexIdentityPolicyOff || channelID <= 0 {
		return nil
	}

	seed := fmt.Sprintf("axonhub:codex-identity:v1:%d", channelID)
	if accountID != "" {
		seed += ":" + accountID
	}
	values := &codexIdentityValues{
		policy:         policy,
		installationID: deriveCodexIdentityUUID(seed, ":installation"),
	}
	if policy == objects.CodexIdentityPolicyDevice {
		return values
	}

	values.sessionID = deriveCodexIdentityUUID(seed, ":session")
	threadSeed := clientThreadID
	if threadSeed == "" {
		threadSeed = clientSessionID
	}
	values.threadID = deriveCodexIdentityUUID(seed, ":thread:", threadSeed)
	if threadSeed == "" || policy == objects.CodexIdentityPolicyFull {
		values.threadID = values.sessionID
	}
	values.turnID = uuid.NewString()
	values.windowID = values.threadID + ":0"
	values.turnStartedAt = time.Now().UnixMilli()

	return values
}

func applyCodexIdentityPolicy(outbound *PersistentOutboundTransformer) pipeline.Middleware {
	return pipeline.OnRawRequest("codex-oauth-identity", func(_ context.Context, request *httpclient.Request) (*httpclient.Request, error) {
		current := outbound.GetCurrentChannel()
		if current == nil || current.Channel == nil || current.Channel.Type != channel.TypeCodex ||
			!current.Channel.Credentials.IsOAuth() {
			return request, nil
		}

		policy := current.Channel.Policies.EffectiveCodexIdentityPolicy()
		if policy == objects.CodexIdentityPolicyOff || request == nil {
			return request, nil
		}
		if request.Headers == nil {
			request.Headers = make(http.Header)
		}

		accountID := strings.TrimSpace(request.Headers.Get("Chatgpt-Account-Id"))
		clientSessionID := originalCodexSessionID(outbound)
		clientThreadID := originalCodexThreadID(outbound)
		values := resolveCodexIdentityValues(current.ID, accountID, clientSessionID, clientThreadID, policy)
		if values == nil {
			return request, nil
		}

		applyCodexIdentityHeaders(request.Headers, values)
		body, err := applyCodexIdentityBody(request.Body, values)
		if err != nil {
			return nil, fmt.Errorf("apply Codex OAuth identity policy: %w", err)
		}
		request.Body = body

		return request, nil
	})
}

func originalCodexSessionID(outbound *PersistentOutboundTransformer) string {
	if outbound == nil || outbound.state == nil || outbound.state.LlmRequest == nil ||
		outbound.state.LlmRequest.RawRequest == nil {
		return ""
	}

	headers := outbound.state.LlmRequest.RawRequest.Headers
	if headers == nil {
		return ""
	}
	if value := strings.TrimSpace(headers.Get(codexSessionIDHeader)); value != "" {
		return value
	}

	return strings.TrimSpace(headers.Get("Session_id"))
}

func originalCodexThreadID(outbound *PersistentOutboundTransformer) string {
	if outbound == nil || outbound.state == nil || outbound.state.LlmRequest == nil ||
		outbound.state.LlmRequest.RawRequest == nil {
		return ""
	}

	headers := outbound.state.LlmRequest.RawRequest.Headers
	if headers == nil {
		return ""
	}
	if value := strings.TrimSpace(headers.Get(codexThreadIDHeader)); value != "" {
		return value
	}

	return strings.TrimSpace(headers.Get("Thread_id"))
}

func applyCodexIdentityHeaders(headers http.Header, values *codexIdentityValues) {
	headers.Set(codexInstallationIDHeader, values.installationID)

	headerFields := map[string]any{"installation_id": values.installationID}
	if values.policy != objects.CodexIdentityPolicyDevice {
		headers.Set(codexSessionIDHeader, values.sessionID)
		headers.Set(codexThreadIDHeader, values.threadID)
		headers.Set(codexWindowIDHeader, values.windowID)
		headers.Set(codexClientRequestIDHeader, values.threadID)
		headerFields["session_id"] = values.sessionID
		headerFields["thread_id"] = values.threadID
		headerFields["turn_id"] = values.turnID
		headerFields["window_id"] = values.windowID
		headerFields["turn_started_at_unix_ms"] = values.turnStartedAt
	}

	if raw := strings.TrimSpace(headers.Get(codexTurnMetadataHeader)); raw != "" {
		if rewritten, ok := rewriteCodexTurnMetadata(raw, headerFields); ok {
			headers.Set(codexTurnMetadataHeader, rewritten)
		}
	}
}

func applyCodexIdentityBody(body []byte, values *codexIdentityValues) ([]byte, error) {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return body, nil
	}

	metadata := make(map[string]any)
	rawMetadata := gjson.GetBytes(body, "client_metadata")
	if !rawMetadata.IsObject() {
		return body, nil
	}
	if err := json.Unmarshal([]byte(rawMetadata.Raw), &metadata); err != nil {
		return nil, err
	}

	metadata["x-codex-installation-id"] = values.installationID
	embeddedFields := map[string]any{"installation_id": values.installationID}
	if values.policy != objects.CodexIdentityPolicyDevice {
		metadata["session_id"] = values.sessionID
		metadata["thread_id"] = values.threadID
		metadata["turn_id"] = values.turnID
		metadata["x-codex-window-id"] = values.windowID
		embeddedFields["session_id"] = values.sessionID
		embeddedFields["thread_id"] = values.threadID
		embeddedFields["turn_id"] = values.turnID
		embeddedFields["window_id"] = values.windowID
		embeddedFields["turn_started_at_unix_ms"] = values.turnStartedAt
	}
	if raw, ok := metadata["x-codex-turn-metadata"].(string); ok {
		if rewritten, valid := rewriteCodexTurnMetadata(raw, embeddedFields); valid {
			metadata["x-codex-turn-metadata"] = rewritten
		}
	}

	raw, err := json.Marshal(metadata)
	if err != nil {
		return nil, err
	}

	return sjson.SetRawBytes(body, "client_metadata", raw)
}

func rewriteCodexTurnMetadata(raw string, fields map[string]any) (string, bool) {
	var metadata map[string]any
	if err := json.Unmarshal([]byte(raw), &metadata); err != nil {
		return "", false
	}
	maps.Copy(metadata, fields)
	rewritten, err := json.Marshal(metadata)
	if err != nil {
		return "", false
	}

	return string(rewritten), true
}
