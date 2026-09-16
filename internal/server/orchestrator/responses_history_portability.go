package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/samber/lo"

	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent"
	entchannel "github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/ent/requestexecution"
	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
)

type responsesHistoryPolicy struct {
	detachIDs     bool
	dropReasoning bool
	expiresAt     time.Time
}

type responsesHistoryPolicyKey struct {
	provider  responsesMetadataCapabilityKey
	service   *biz.RequestService
	updatedAt time.Time
	apiKeyID  int
	projectID int
}

var responsesHistoryPolicies = lo.Must(lru.New[responsesHistoryPolicyKey, responsesHistoryPolicy](256))

// Apply an evidence-backed destination policy before the first upstream call.
// It neither sends a probe nor schedules a retry. Retained successful correction
// pairs restore the policy after restart, including records made by older builds.
func applyResponsesHistoryPortability(outbound *PersistentOutboundTransformer) pipeline.Middleware {
	return &responsesHistoryPortabilityMiddleware{outbound: outbound}
}

type responsesHistoryPortabilityMiddleware struct {
	pipeline.DummyMiddleware
	outbound *PersistentOutboundTransformer
}

func (m *responsesHistoryPortabilityMiddleware) Name() string {
	return "responses-history-portability"
}

func (m *responsesHistoryPortabilityMiddleware) OnOutboundRawRequest(ctx context.Context, request *httpclient.Request) (*httpclient.Request, error) {
	if m.outbound == nil || m.outbound.state == nil || request == nil || request.APIFormat != string(llm.APIFormatOpenAIResponse) {
		return request, nil
	}
	state := m.outbound.state
	channel := m.outbound.GetCurrentChannel()
	if channel == nil || channel.Type != entchannel.TypeCodex || channel.Credentials.IsOAuth() || channel.UpdatedAt.IsZero() ||
		state.RequestService == nil || state.APIKey == nil || state.APIKey.ID <= 0 || state.APIKey.ProjectID <= 0 {
		return request, nil
	}
	hasIDs, safe := responsesResourceHistorySupportsRecovery(request.Body)
	hasReasoning, explicit := responsesExplicitHistorySupportsRecovery(request.Body)
	if !safe || !explicit || !hasIDs && !hasReasoning {
		return request, nil
	}
	suffix, ok := responsesHistoryCredentialSuffix(ctx, channel, request)
	if !ok {
		return request, nil
	}
	key := responsesHistoryPolicyKey{
		provider: responsesMetadataKey(channel.ID, request), service: state.RequestService,
		updatedAt: channel.UpdatedAt, apiKeyID: state.APIKey.ID, projectID: state.APIKey.ProjectID,
	}
	if key.provider.model == "" || key.provider.url == "" {
		return request, nil
	}
	policy, found := responsesHistoryPolicies.Get(key)
	if !found || !time.Now().Before(policy.expiresAt) {
		var err error
		policy, err = loadResponsesHistoryPolicy(ctx, key, suffix)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			log.Warn(ctx, "could not read Responses history compatibility evidence", log.Cause(err), log.Int("channel_id", channel.ID))
			return request, nil
		}
		responsesHistoryPolicies.Add(key, policy)
	}
	body, changed, err := portableResponsesHistory(request.Body, policy)
	if err != nil {
		return nil, err
	}
	if changed {
		request.Body = body
		if len(request.JSONBody) > 0 {
			request.JSONBody = body
		}
		request.Headers.Del(codexTurnStateHeader)
		log.Debug(ctx, "prepared portable Responses history before upstream execution", log.Int("channel_id", channel.ID))
	}
	return request, nil
}

func responsesHistoryCredentialSuffix(ctx context.Context, channel *biz.Channel, request *httpclient.Request) (string, bool) {
	selected, ok := contexts.GetChannelAPIKey(ctx)
	if !ok || len([]rune(selected)) <= 4 {
		return "", false
	}
	// Historical suffixes describe the selected channel key, not arbitrary auth
	// overrides. Never use that evidence for a different final wire credential.
	if request.Auth != nil && request.Auth.APIKey != "" {
		if request.Auth.APIKey != selected {
			return "", false
		}
	} else if request.Headers.Get("Authorization") != "Bearer "+selected {
		return "", false
	}
	runes := []rune(selected)
	suffix := string(runes[len(runes)-4:])
	matches := 0
	found := false
	for _, credential := range channel.Credentials.GetAllAPIKeys() {
		if strings.HasSuffix(credential, suffix) {
			matches++
			found = found || credential == selected
		}
	}
	return suffix, found && matches == 1
}

func portableResponsesHistory(body []byte, policy responsesHistoryPolicy) ([]byte, bool, error) {
	if _, safe := responsesResourceHistorySupportsRecovery(body); !safe {
		return body, false, nil
	}
	if _, explicit := responsesExplicitHistorySupportsRecovery(body); !explicit {
		return body, false, nil
	}
	var rules []responsesRejectedStatusRule
	if policy.detachIDs {
		rules = append(rules, responsesRejectedStatusRule{index: -1, field: "id"})
	}
	if policy.dropReasoning {
		rules = append(rules, responsesRejectedStatusRule{itemType: "reasoning", index: -1, field: "encrypted_content", dropItem: true})
	}
	return stripResponsesRejectedStatus(body, rules)
}

func loadResponsesHistoryPolicy(ctx context.Context, key responsesHistoryPolicyKey, suffix string) (responsesHistoryPolicy, error) {
	lookupCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	now := time.Now()
	since := now.Add(-responsesReasoningRecoveryTTL)
	if key.updatedAt.After(since) {
		since = key.updatedAt
	}
	policy := responsesHistoryPolicy{expiresAt: now.Add(time.Minute)}
	executions, err := key.service.FindResponsesHistoryEvidence(lookupCtx, biz.ResponsesHistoryEvidenceScope{
		ChannelID: key.provider.channelID, APIKeyID: key.apiKeyID, ProjectID: key.projectID,
		Model: key.provider.model, URL: key.provider.url, CredentialSuffix: suffix, Since: since,
	})
	if err != nil {
		return policy, err
	}
	completed := make(map[int]*ent.RequestExecution)
	for _, execution := range executions {
		if execution.Status == requestexecution.StatusCompleted && completed[execution.RequestID] == nil {
			completed[execution.RequestID] = execution
		}
	}
	idRequests := make(map[int]bool)
	reasoningRequests := make(map[int]bool)
	loadedBytes := 0
	for _, failed := range executions {
		success := completed[failed.RequestID]
		if failed.Status != requestexecution.StatusFailed || success == nil || success.ID <= failed.ID || failed.ErrorMessage == "" {
			continue
		}
		message := strings.TrimSpace(failed.ErrorMessage)
		if !responsesResourceMismatch("", message) && !responsesRejectedReasoningMessagePattern.MatchString(message) {
			continue
		}
		before, err := key.service.LoadResponsesHistoryEvidenceBody(lookupCtx, failed)
		if err != nil {
			return policy, err
		}
		after, err := key.service.LoadResponsesHistoryEvidenceBody(lookupCtx, success)
		if err != nil {
			return policy, err
		}
		loadedBytes += len(before) + len(after)
		if loadedBytes > 32<<20 {
			break
		}
		correction := confirmedResponsesHistoryCorrection(before, after, message)
		idRequests[failed.RequestID] = idRequests[failed.RequestID] || correction.detachIDs
		reasoningRequests[failed.RequestID] = reasoningRequests[failed.RequestID] || correction.dropReasoning
		policy.detachIDs = lo.Count(lo.Values(idRequests), true) >= 2
		policy.dropReasoning = lo.Count(lo.Values(reasoningRequests), true) >= 2
		if policy.detachIDs && policy.dropReasoning {
			break
		}
	}
	if policy.detachIDs && policy.dropReasoning {
		policy.expiresAt = now.Add(responsesReasoningRecoveryTTL)
	}
	return policy, nil
}

func confirmedResponsesHistoryCorrection(before, after []byte, message string) responsesHistoryPolicy {
	_, rejectedReasoning := responsesRejectedReasoningMessageRule(before, "", message, "")
	beforeIDs, beforeSafe := responsesResourceHistorySupportsRecovery(before)
	afterIDs, afterSafe := responsesResourceHistorySupportsRecovery(after)
	beforeReasoning, beforeExplicit := responsesExplicitHistorySupportsRecovery(before)
	afterReasoning, afterExplicit := responsesExplicitHistorySupportsRecovery(after)
	if !beforeSafe || !afterSafe || !beforeExplicit || !afterExplicit {
		return responsesHistoryPolicy{}
	}
	policy := responsesHistoryPolicy{
		detachIDs:     beforeIDs && !afterIDs && responsesResourceMismatch("", message),
		dropReasoning: beforeReasoning && !afterReasoning && (responsesResourceMismatch("", message) || rejectedReasoning),
	}
	if !policy.detachIDs && !policy.dropReasoning {
		return responsesHistoryPolicy{}
	}
	// Validate the correction against the exact successful body. No deletion of
	// other input, tools, options, or instructions may justify a learned policy.
	normalized, changed, err := portableResponsesHistory(before, policy)
	if err != nil || !changed {
		return responsesHistoryPolicy{}
	}
	var expected, actual any
	expectedDecoder := json.NewDecoder(bytes.NewReader(normalized))
	expectedDecoder.UseNumber()
	actualDecoder := json.NewDecoder(bytes.NewReader(after))
	actualDecoder.UseNumber()
	if expectedDecoder.Decode(&expected) != nil || actualDecoder.Decode(&actual) != nil || !reflect.DeepEqual(expected, actual) {
		return responsesHistoryPolicy{}
	}
	return policy
}
