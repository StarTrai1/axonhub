package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
)

type responsesCompactionRecoveryKey struct {
	scope     responsesReasoningRecoveryScope
	cacheKey  string
	threadID  string
	apiKeyID  int
	projectID int
}

var responsesRejectedCompactionMessagePattern = regexp.MustCompile(
	`^(?:OpenAI Responses bad request: )?The encrypted content for item (cmp_[A-Za-z0-9_-]+) could not be verified\. Reason: Encrypted content could not be decrypted or parsed\.(?: \[trace_id=[A-Za-z0-9_-]+\])?$`,
)

// A native checkpoint can be valid at its source and rejected by another
// provider. Match the exact checkpoint, never an unrelated encrypted item.
func responsesRejectedCompactionMessage(body []byte, ref *remoteCompactionReference, code, message, param string) bool {
	if ref == nil || isLocalCompactionReference(ref) ||
		(code != "" && code != "bad_request" && code != "invalid_request_error" && code != "invalid_encrypted_content") {
		return false
	}
	match := responsesRejectedCompactionMessagePattern.FindStringSubmatch(message)
	if len(match) != 2 || match[1] != ref.ID {
		return false
	}
	index := -1
	for i, item := range gjson.GetBytes(body, "input").Array() {
		if item.Get("id").String() != ref.ID {
			continue
		}
		if index >= 0 || (item.Get("type").String() != remoteCompactionItemType && item.Get("type").String() != legacyRemoteCompactionSummaryType) ||
			item.Get("encrypted_content").String() != ref.EncryptedContent {
			return false
		}
		index = i
	}
	return index >= 0 && (param == "" || param == fmt.Sprintf("input[%d].encrypted_content", index))
}

type responsesCompactionRecoveryMiddleware struct {
	pipeline.DummyMiddleware

	outbound *PersistentOutboundTransformer
	adapter  *remoteCompactionAdapter
	executor pipeline.Executor
	rejected map[responsesCompactionRecoveryKey]struct{}
}

func recoverRejectedRemoteCompaction(
	outbound *PersistentOutboundTransformer,
	adapter *remoteCompactionAdapter,
	executor pipeline.Executor,
) pipeline.Middleware {
	return &responsesCompactionRecoveryMiddleware{outbound: outbound, adapter: adapter, executor: executor}
}

func (m *responsesCompactionRecoveryMiddleware) Name() string {
	return "responses-rejected-compaction-recovery"
}

func (m *responsesCompactionRecoveryMiddleware) OnOutboundRawError(ctx context.Context, err error) {
	if ctx.Err() != nil || m.outbound == nil || m.outbound.state == nil || m.adapter == nil || m.executor == nil {
		return
	}
	code, message, param, ok := responsesBadRequestDetails(err)
	if !ok {
		return
	}
	request := m.outbound.state.RawProviderRequest
	if request == nil {
		return
	}
	hasIDs, safe := responsesResourceHistorySupportsRecovery(request.Body)
	if !safe {
		return
	}
	key, ref, ok := m.recoveryKey(ctx, request)
	if !ok {
		return
	}
	if param == "" && responsesResourceMismatch(code, message) {
		// An ambiguous resource rejection first tries the smaller ID correction.
		if hasIDs {
			return
		}
	} else if !responsesRejectedCompactionMessage(request.Body, ref, code, message, param) {
		return
	}
	if _, alreadyRejected := m.rejected[key]; alreadyRejected {
		return
	}
	if m.rejected == nil {
		m.rejected = make(map[responsesCompactionRecoveryKey]struct{})
	}
	m.rejected[key] = struct{}{}
	m.outbound.state.responsesRejectedStatusRetryChannel = key.scope.provider.channelID
	log.Info(ctx, "native compaction rejected; scheduling retained-history recovery",
		log.Int("channel_id", key.scope.provider.channelID),
		log.String("thread_id", key.threadID))
}

func (m *responsesCompactionRecoveryMiddleware) OnOutboundRawRequest(
	ctx context.Context,
	request *httpclient.Request,
) (*httpclient.Request, error) {
	if len(m.rejected) == 0 {
		return request, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	key, ref, ok := m.recoveryKey(ctx, request)
	if !ok {
		return request, nil
	}
	if _, rejected := m.rejected[key]; !rejected {
		return request, nil
	}
	if _, safe := responsesResourceHistorySupportsRecovery(request.Body); !safe {
		return nil, &remoteCompactionPreparationError{cause: errors.New("cannot recover rejected compaction with unresolved Responses history")}
	}
	state := m.outbound.state
	candidate := *state.CurrentCandidate
	if state.CurrentModelIndex < 0 || state.CurrentModelIndex >= len(candidate.Models) {
		return nil, &remoteCompactionPreparationError{cause: errors.New("rejected compaction has no selected model")}
	}
	// The summary is a separate request on the selected route. Its mutable
	// context and model selection must not replace the continuation's credentials
	// or try an unrelated candidate while preparing a same-channel retry.
	candidate.Models = candidate.Models[state.CurrentModelIndex : state.CurrentModelIndex+1]
	candidate.APIFormat = string(llm.APIFormatOpenAIResponse)
	bridgeState := &PersistenceState{
		APIKey:                  state.APIKey,
		ChannelService:          state.ChannelService,
		RetryPolicyProvider:     state.RetryPolicyProvider,
		Proxy:                   state.Proxy,
		ChannelModelsCandidates: []*ChannelModelsCandidate{&candidate},
	}
	summary, err := m.adapter.summaryForCompaction(
		contexts.CloneRequestContext(ctx), key.cacheKey, ref, key.threadID,
		key.scope.provider.model, bridgeState, m.executor,
	)
	if err != nil {
		return nil, &remoteCompactionPreparationError{cause: fmt.Errorf("restore upstream-rejected compaction from retained history: %w", err)}
	}
	body, err := replaceRemoteCompactionWithLocalSummary(request.Body, summary)
	if err != nil {
		return nil, &remoteCompactionPreparationError{cause: err}
	}
	request.Body = body
	if len(request.JSONBody) > 0 {
		request.JSONBody = body
	}
	request.Headers.Del(codexTurnStateHeader)
	return request, nil
}

func (m *responsesCompactionRecoveryMiddleware) recoveryKey(
	ctx context.Context,
	request *httpclient.Request,
) (responsesCompactionRecoveryKey, *remoteCompactionReference, bool) {
	if m.outbound == nil || m.outbound.state == nil || request == nil {
		return responsesCompactionRecoveryKey{}, nil, false
	}
	state := m.outbound.state
	if state.APIKey == nil || state.APIKey.ID <= 0 || state.APIKey.ProjectID <= 0 || state.RawRequest == nil {
		return responsesCompactionRecoveryKey{}, nil, false
	}
	scope, ok := responsesReasoningScope(ctx, m.outbound.GetCurrentChannel(), request)
	if !ok {
		return responsesCompactionRecoveryKey{}, nil, false
	}
	ref, _, _, err := parseRemoteCompactionRequest(request.Body)
	if err != nil || ref == nil || ref.ID == "" || ref.EncryptedContent == "" || isLocalCompactionReference(ref) {
		return responsesCompactionRecoveryKey{}, nil, false
	}
	count := 0
	for _, item := range gjson.GetBytes(request.Body, "input").Array() {
		if item.Get("type").String() == remoteCompactionItemType || item.Get("type").String() == legacyRemoteCompactionSummaryType {
			count++
		}
	}
	if count != 1 {
		return responsesCompactionRecoveryKey{}, nil, false
	}
	// Look up the original client thread, before an OAuth identity policy can
	// rewrite its wire identity. Explicit overrides cannot substitute another
	// checkpoint and thereby select a different retained source.
	original, threadID, _, err := parseRemoteCompactionRequest(rawRequestPayload(state.RawRequest))
	if err != nil || original == nil || threadID == "" ||
		remoteCompactionCacheKey(original) != remoteCompactionCacheKey(ref) {
		return responsesCompactionRecoveryKey{}, nil, false
	}
	return responsesCompactionRecoveryKey{
		scope: scope, cacheKey: remoteCompactionCacheKey(ref), threadID: threadID,
		apiKeyID: state.APIKey.ID, projectID: state.APIKey.ProjectID,
	}, original, true
}
