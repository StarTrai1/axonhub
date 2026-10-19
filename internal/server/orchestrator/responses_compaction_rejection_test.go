package orchestrator

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/pipeline"
)

func TestResponsesCompactionRejectionSurvivesIncompleteRecoveryAndWindowChange(t *testing.T) {
	ctx, client, owner, adapter := rejectedCompactionStorage(t)
	adapter.systemService.UnifiedHistory.Enabled = true
	req := rejectedCompactionPipelineRequest(t, llm.APIFormatOpenAIResponse)
	executor := &responsesReasoningPipelineExecutor{failures: []error{rejectedNativeCheckpointError()}}
	var observed *responsesCompactionRecoveryMiddleware
	configure := func(state *PersistenceState, outbound *PersistentOutboundTransformer) pipeline.Middleware {
		state.APIKey = owner
		observed = recoverRejectedRemoteCompaction(outbound, adapter, executor).(*responsesCompactionRecoveryMiddleware)
		return observed
	}
	_, result, err := runRejectedReasoningPipeline(t, ctx, req, executor, "rejected-target-key", true, 0, configure)
	require.Error(t, err)
	require.Nil(t, result)
	require.Len(t, executor.requests, 1)
	require.Empty(t, adapter.recoveries.Keys(), "rejection does not prove successful recovery")
	key, ref, ok := observed.recoveryKey(ctx, executor.requests[0])
	require.True(t, ok)
	require.True(t, observed.hasCheckpointRejection(ctx, key))
	require.NoError(t, adapter.retainLegacyCompactionSummary(ctx, remoteCompactionCacheKey(ref), ref, &PersistenceState{APIKey: owner}, "verified original source summary"))
	service := biz.NewSystemService(biz.SystemServiceParams{Ent: client})
	service.UnifiedHistory.Enabled = true
	adapter = newRemoteCompactionAdapter(adapter.requestService, nil, service)
	executor = &responsesReasoningPipelineExecutor{events: rejectedReasoningCompactionEvents()}
	req.Headers.Set("X-Codex-Window-Id", "recovery-thread:reconnected")
	_, result, err = runRejectedReasoningPipeline(t, ctx, req, executor, "rejected-target-key", true, 0, configure)
	require.NoError(t, err)
	drainRejectedReasoningPipeline(t, result)
	require.Len(t, executor.requests, 1)
	require.False(t, gjson.GetBytes(executor.requests[0].Body, `input.#(id=="cmp_native")`).Exists())
	require.Contains(t, string(executor.requests[0].Body), "verified original source summary")
	for _, mutate := range []func(*responsesCompactionRecoveryKey){
		func(k *responsesCompactionRecoveryKey) { k.scope.provider.channelID++ },
		func(k *responsesCompactionRecoveryKey) { k.scope.provider.url += "/other" },
		func(k *responsesCompactionRecoveryKey) { k.scope.provider.model = "other" },
		func(k *responsesCompactionRecoveryKey) { k.scope.provider.credential[0]++ },
		func(k *responsesCompactionRecoveryKey) { k.scope.owner[0]++ },
		func(k *responsesCompactionRecoveryKey) { k.scope.threadID = "other" },
		func(k *responsesCompactionRecoveryKey) { k.cacheKey = "other" },
		func(k *responsesCompactionRecoveryKey) { k.threadID = "other" },
		func(k *responsesCompactionRecoveryKey) { k.apiKeyID++ },
		func(k *responsesCompactionRecoveryKey) { k.projectID++ },
	} {
		changed := key
		mutate(&changed)
		require.False(t, observed.hasCheckpointRejection(ctx, changed))
	}
	missing := key
	missing.cacheKey = "expired-checkpoint"
	adapter.checkpointRejections.Add(missing.rejectionDigest(), time.Now().Add(-time.Second))
	require.False(t, observed.hasCheckpointRejection(ctx, missing))
	recovered, err := service.LoadResponsesCompactionRecovery(ctx, key.rejectionDigest())
	require.NoError(t, err)
	require.True(t, recovered.IsZero(), "negative and successful recovery evidence must be separate")
}
