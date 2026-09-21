package orchestrator

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/streams"
)

func rejectedNativeCheckpointError() error {
	return &httpclient.Error{StatusCode: 400, Body: []byte(`{"error":{"type":"invalid_request_error","message":"The encrypted content for item cmp_native could not be verified. Reason: Encrypted content could not be decrypted or parsed."}}`)}
}

func TestResponsesCompactionRecoverySurvivesNextRequestAndRestart(t *testing.T) {
	for _, passThrough := range []bool{true, false} {
		for _, format := range []llm.APIFormat{llm.APIFormatOpenAIResponse, llm.APIFormatOpenAIResponseCompact} {
			t.Run(string(format)+map[bool]string{true: "/raw", false: "/converted"}[passThrough], func(t *testing.T) {
				ctx, client, apiKey, adapter := rejectedCompactionStorage(t)
				req := rejectedCompactionPipelineRequest(t, format)
				ref, _, _, err := parseRemoteCompactionRequest(req.Body)
				require.NoError(t, err)
				require.NoError(t, adapter.retainLegacyCompactionSummary(ctx, remoteCompactionCacheKey(ref), ref, &PersistenceState{APIKey: apiKey}, "verified retained source summary"))
				var lastMiddleware *responsesCompactionRecoveryMiddleware
				run := func(current *remoteCompactionAdapter, reject bool) {
					t.Helper()
					executor := &responsesReasoningPipelineExecutor{
						events:   rejectedReasoningCompactionEvents(),
						response: &httpclient.Response{StatusCode: http.StatusOK, Body: []byte(`{"id":"cmp_output","object":"response.compaction","output":[{"type":"compaction","id":"cmp_next","encrypted_content":"new-native-state"}]}`)},
					}
					budget := 0
					if reject {
						executor.failures = []error{rejectedNativeCheckpointError()}
						budget = 1
					}
					_, result, err := runRejectedReasoningPipeline(t, ctx, req, executor, "confirmed-target-key", passThrough, budget,
						func(state *PersistenceState, outbound *PersistentOutboundTransformer) pipeline.Middleware {
							state.APIKey = apiKey
							lastMiddleware = recoverRejectedRemoteCompaction(outbound, current, executor).(*responsesCompactionRecoveryMiddleware)
							return lastMiddleware
						})
					require.NoError(t, err)
					if result.Stream {
						drainRejectedReasoningPipeline(t, result)
					}
					require.Len(t, executor.requests, budget+1)
					assertRejectedCompactionWindowPreserved(t, req.Body, executor.requests[len(executor.requests)-1].Body)
				}
				run(adapter, true)
				// Pass-through drains its transformed stream asynchronously.
				require.Eventually(t, func() bool { return adapter.recoveries.Len() == 1 }, time.Second, time.Millisecond)
				key := adapter.recoveries.Keys()[0]
				expiry, ok := adapter.recoveries.Peek(key)
				require.True(t, ok)
				require.Eventually(t, func() bool {
					stored, loadErr := adapter.systemService.LoadResponsesCompactionRecovery(ctx, key.digest())
					return loadErr == nil && stored.Equal(expiry)
				}, time.Second, time.Millisecond)
				run(adapter, false)
				unchanged, _ := adapter.recoveries.Peek(key)
				require.Equal(t, expiry, unchanged, "cache hits must not extend TTL")
				restarted := newRemoteCompactionAdapter(adapter.requestService, nil, biz.NewSystemService(biz.SystemServiceParams{Ent: client}))
				run(restarted, false)
				for _, mutate := range []func(*responsesCompactionRecoveryKey){
					func(k *responsesCompactionRecoveryKey) { k.scope.provider.channelID++ },
					func(k *responsesCompactionRecoveryKey) { k.scope.provider.url += "/other" },
					func(k *responsesCompactionRecoveryKey) { k.scope.provider.model = "other" },
					func(k *responsesCompactionRecoveryKey) { k.scope.provider.credential[0]++ },
					func(k *responsesCompactionRecoveryKey) { k.scope.owner[0]++ },
					func(k *responsesCompactionRecoveryKey) { k.scope.threadID = "other" },
					func(k *responsesCompactionRecoveryKey) { k.scope.windowID = "other" },
					func(k *responsesCompactionRecoveryKey) { k.cacheKey = "other" },
					func(k *responsesCompactionRecoveryKey) { k.threadID = "other" },
					func(k *responsesCompactionRecoveryKey) { k.apiKeyID++ },
					func(k *responsesCompactionRecoveryKey) { k.projectID++ },
				} {
					changed := key
					mutate(&changed)
					require.False(t, lastMiddleware.hasConfirmedRecovery(ctx, changed))
				}
				metadataExpiry, err := restarted.systemService.LoadResponsesMetadataRejection(ctx, key.digest())
				require.NoError(t, err)
				require.True(t, metadataExpiry.IsZero(), "compaction and metadata namespaces must remain separate")
			})
		}
	}
}

func TestResponsesCompactionRecoveryRequiresSuccessfulCompletion(t *testing.T) {
	for _, event := range []string{
		`{"type":"response.failed","response":{"status":"failed"}}`,
		`{"type":"response.incomplete","response":{"status":"incomplete"}}`,
		`{"type":"response.cancelled"}`,
		`{"type":"error"}`,
		`{"type":"response.created","response":{"status":"in_progress"}}`,
	} {
		t.Run(gjson.Get(event, "type").String(), func(t *testing.T) {
			adapter := newRemoteCompactionAdapter(nil, nil, nil)
			key := responsesCompactionRecoveryKey{cacheKey: "checkpoint"}
			m := &responsesCompactionRecoveryMiddleware{adapter: adapter, adapted: &key}
			stream, err := m.OnOutboundRawStream(t.Context(), streams.SliceStream([]*httpclient.StreamEvent{{Data: []byte(event)}}))
			require.NoError(t, err)
			_, err = streams.All(stream)
			require.NoError(t, err)
			require.Empty(t, adapter.recoveries.Keys())
		})
	}
	adapter := newRemoteCompactionAdapter(nil, nil, nil)
	key := responsesCompactionRecoveryKey{cacheKey: "expired"}
	adapter.recoveries.Add(key, time.Now().Add(-time.Second))
	m := &responsesCompactionRecoveryMiddleware{adapter: adapter}
	require.False(t, m.hasConfirmedRecovery(t.Context(), key))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	m.confirmRecovery(ctx, key)
	require.Empty(t, adapter.recoveries.Keys())
}
