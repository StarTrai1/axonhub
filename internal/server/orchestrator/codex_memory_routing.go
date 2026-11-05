package orchestrator

import (
	"context"
	"errors"
	"strings"

	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

const codexMemoryMaxAttempts = 3

func codexMemoryRoutingContext(ctx context.Context, request *httpclient.Request, store *responsesSessionStore) context.Context {
	task := shared.CodexMemoryNone
	key, authenticated := contexts.GetAPIKey(ctx)
	if shared.IsResponsesAPI(ctx) && request != nil && authenticated && key.ID > 0 && key.ProjectID > 0 {
		var known bool
		task, known = shared.ReadCodexMemoryTask(request.Headers, request.Body)
		if !known && store != nil {
			// Inherit only from an owned response in the same explicit thread/window.
			// Missing metadata never makes a shared session a memory task by itself.
			identity := shared.ReadCodexRequestMetadata(nil, request.Body)
			if identity.ThreadID == "" {
				identity = shared.ReadCodexRequestMetadata(request.Headers, request.Body)
			}
			previous := gjson.GetBytes(request.Body, "previous_response_id").String()
			if record := store.retainedRecord(ctx, previous); record != nil && identity.ThreadID != "" &&
				record.memoryThread == identity.ThreadID && (identity.WindowID == "" || record.windowID == "" || identity.WindowID == record.windowID) {
				task = record.memoryTask
			}
		}
	}
	return shared.WithCodexMemoryTask(ctx, task)
}

type CodexMemorySelector struct{ wrapped CandidateSelector }

func WithCodexMemorySelector(wrapped CandidateSelector) *CodexMemorySelector {
	return &CodexMemorySelector{wrapped: wrapped}
}

func (s *CodexMemorySelector) Select(ctx context.Context, req *llm.Request) ([]*ChannelModelsCandidate, error) {
	candidates, err := s.wrapped.Select(ctx, req)
	if err != nil {
		return nil, err
	}
	memory := shared.GetCodexMemoryTask(ctx) != shared.CodexMemoryNone
	var selected []*ChannelModelsCandidate
	for _, candidate := range candidates {
		if candidate != nil && candidate.Channel != nil && candidate.Channel.Policies.AllowsCodexMemoryTask(memory) {
			selected = append(selected, candidate)
		}
	}
	return selected, nil
}

// AllowRetry applies one budget across channel switches, same-channel retries
// and continuation repair. It never enables retries disabled by global policy.
func (p *PersistentOutboundTransformer) AllowRetry(err error) bool {
	if p.state == nil || p.state.memoryTask == shared.CodexMemoryNone {
		return true
	}
	if isContentPolicyRefusal(err) {
		log.Info(context.Background(), "Codex memory request refused by upstream content policy",
			log.String("memory_task", string(p.state.memoryTask)))
		return false
	}
	return p.state.memoryAttempts < codexMemoryMaxAttempts
}

func isContentPolicyRefusal(err error) bool {
	if err == nil {
		return false
	}
	codeIsRefusal := func(code string) bool {
		switch strings.ToLower(strings.TrimSpace(code)) {
		case "content_filter", "content_blocked", "content-blocked", "content_policy_violation", "content_filter_error", "responsibleaipolicyviolation":
			return true
		default:
			return false
		}
	}
	var responseErr *llm.ResponseError
	if errors.As(err, &responseErr) && (codeIsRefusal(responseErr.Detail.Code) || codeIsRefusal(responseErr.Detail.Type)) {
		return true
	}
	var httpErr *httpclient.Error
	if errors.As(err, &httpErr) {
		for _, path := range []string{"error.code", "error.type", "error.innererror.code", "response.error.code"} {
			if codeIsRefusal(gjson.GetBytes(httpErr.Body, path).String()) {
				return true
			}
		}
	}
	message := strings.ToLower(strings.TrimSpace(ExtractErrorMessage(err)))
	return message == "content-blocked" || strings.HasPrefix(message, "content-blocked (") || strings.HasPrefix(message, "content-blocked [")
}
