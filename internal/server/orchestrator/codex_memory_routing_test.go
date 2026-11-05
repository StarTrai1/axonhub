package orchestrator

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

func TestCodexMemoryRoutingPoliciesAndProfiles(t *testing.T) {
	var candidates []*ChannelModelsCandidate
	for i, policy := range []objects.CodexMemoryPolicy{objects.CodexMemoryNormal, objects.CodexMemoryExclude, objects.CodexMemoryOnly} {
		candidates = append(candidates, &ChannelModelsCandidate{
			Channel: &biz.Channel{Channel: &ent.Channel{ID: i + 1, Policies: objects.ChannelPolicies{CodexMemory: policy}}},
			Models:  []biz.ChannelModelEntry{{RequestModel: "gpt-6.1-terra", ActualModel: "gpt-6.1-terra"}},
		})
	}
	for _, task := range []shared.CodexMemoryTask{shared.CodexMemoryNone, shared.CodexMemoryExtract, shared.CodexMemoryConsolidate} {
		ctx := shared.WithCodexMemoryTask(t.Context(), task)
		selector := WithCodexMemorySelector(&mockSelector{candidates: candidates})
		req := &llm.Request{Model: "gpt-6.1-terra"}
		selected, err := selector.Select(ctx, req)
		require.NoError(t, err)
		require.Len(t, selected, 2)
		require.Same(t, candidates[0], selected[0])
		if task == shared.CodexMemoryNone {
			require.Same(t, candidates[1], selected[1])
		} else {
			require.Same(t, candidates[2], selected[1])
		}
		profile := WithSelectedChannelsSelector(selector, []int{2})
		selected, err = profile.Select(ctx, req)
		require.NoError(t, err)
		if task != shared.CodexMemoryNone {
			require.Empty(t, selected)
		}
		require.Len(t, candidates, 3, "filter must not mutate the cached candidate list")
	}
}

func TestCodexMemoryRoutingContinuationIsolation(t *testing.T) {
	ctx := shared.WithSessionScope(codexMemoryTestContext(t, 1, 2), "owner-key-project")
	store := newResponsesSessionStore()
	root := []byte(`{"client_metadata":{"thread_id":"memory-thread","x-codex-turn-metadata":"{\"request_kind\":\"turn\",\"thread_source\":\"memory_consolidation\"}"},"input":"original"}`)
	store.record(ctx, root, []byte(`{"id":"resp_memory","status":"completed","output":[]}`))
	for _, tc := range []struct {
		name, thread, scope, purpose string
		want                         shared.CodexMemoryTask
	}{
		{"same thread", "memory-thread", "owner-key-project", "", shared.CodexMemoryConsolidate},
		{"other thread", "other", "owner-key-project", "", shared.CodexMemoryNone},
		{"other key or project", "memory-thread", "another-owner", "", shared.CodexMemoryNone},
		{"explicit foreground", "memory-thread", "owner-key-project", `,"x-codex-turn-metadata":"{\"request_kind\":\"turn\",\"thread_source\":\"user\"}"`, shared.CodexMemoryNone},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &httpclient.Request{Body: []byte(fmt.Sprintf(`{"previous_response_id":"resp_memory","client_metadata":{"thread_id":%q%s},"input":[]}`, tc.thread, tc.purpose))}
			got := codexMemoryRoutingContext(shared.WithSessionScope(ctx, tc.scope), r, store)
			require.Equal(t, tc.want, shared.GetCodexMemoryTask(got))
		})
	}
}

func TestCodexMemoryFinalDispatchCannotBypassPolicy(t *testing.T) {
	for _, task := range []shared.CodexMemoryTask{shared.CodexMemoryNone, shared.CodexMemoryExtract} {
		policy := objects.CodexMemoryOnly
		if task != shared.CodexMemoryNone {
			policy = objects.CodexMemoryExclude
		}
		state := &PersistenceState{memoryTask: task, ChannelModelsCandidates: []*ChannelModelsCandidate{{Channel: &biz.Channel{Channel: &ent.Channel{Policies: objects.ChannelPolicies{CodexMemory: policy}}}}}}
		p := &PersistentOutboundTransformer{state: state}
		_, err := p.TransformRequest(t.Context(), &llm.Request{})
		require.ErrorIs(t, err, biz.ErrInvalidModel)
		require.Zero(t, state.memoryAttempts)
	}
}

func TestCodexMemoryRefusalAndRetryBudget(t *testing.T) {
	blocked := &httpclient.Error{StatusCode: 400, Body: []byte(`{"error":{"message":"content-blocked (request id: synthetic)"}}`)}
	quota := &httpclient.Error{StatusCode: 429, Body: []byte(`{"error":{"message":"token rate limit"}}`)}
	for _, task := range []shared.CodexMemoryTask{shared.CodexMemoryNone, shared.CodexMemoryExtract, shared.CodexMemoryConsolidate} {
		state := &PersistenceState{memoryTask: task, memoryAttempts: 1}
		p := &PersistentOutboundTransformer{state: state}
		require.Equal(t, task == shared.CodexMemoryNone, p.AllowRetry(blocked))
		require.True(t, p.AllowRetry(quota))
		state.memoryAttempts = codexMemoryMaxAttempts
		require.Equal(t, task == shared.CodexMemoryNone, p.AllowRetry(quota))
	}
	for _, code := range []string{"content_filter", "content_policy_violation", "ResponsibleAIPolicyViolation"} {
		require.True(t, isContentPolicyRefusal(&llm.ResponseError{StatusCode: 400, Detail: llm.ErrorDetail{Code: code}}))
	}
	require.False(t, isContentPolicyRefusal(quota))
	require.False(t, isContentPolicyRefusal(&httpclient.Error{StatusCode: http.StatusBadRequest, Body: []byte(`{"error":{"code":"invalid_encrypted_content","message":"encrypted content could not be verified"}}`)}))
}

func TestCodexMemoryRetryRefreshRespectsPolicyChanges(t *testing.T) {
	ctx, client := setupTest(t)
	ch := createRetryGuardChannel(t, ctx, client, "memory-policy-refresh", objects.ChannelCredentials{APIKey: "synthetic"})
	svc := newTestChannelServiceForChannels(client)
	p := newRetryGuardTransformer(svc, retryGuardSnapshot(t, ctx, svc, ch.ID), "synthetic")
	p.state.memoryTask = shared.CodexMemoryExtract
	_, err := client.Channel.UpdateOneID(ch.ID).SetPolicies(objects.ChannelPolicies{CodexMemory: objects.CodexMemoryExclude}).Save(ctx)
	require.NoError(t, err)
	require.ErrorIs(t, p.refreshChannelBeforeRetry(ctx), errChannelUnavailableForRetry)
}
