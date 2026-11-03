package orchestrator

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/streams"
)

// The current checkpoint source is complete. Its still-readable native ancestor
// must not force a lookup of expired history or undo several earlier compactions.
func TestCheckpointCanonicalRecoveryKeepsAcceptedAncestor(t *testing.T) {
	for _, archive := range []string{"missing predecessor", "oversized archive"} {
		for _, format := range []llm.APIFormat{llm.APIFormatOpenAIResponse, llm.APIFormatOpenAIResponseWebSocket} {
			t.Run(archive+"/"+string(format), func(t *testing.T) {
				ctx, _, owner, adapter := rejectedCompactionStorage(t)
				req := rejectedCompactionPipelineRequest(t, format)
				latest, _, _, err := parseRemoteCompactionRequest(req.Body)
				require.NoError(t, err)
				ancestor := &remoteCompactionReference{ID: "cmp_ancestor", EncryptedContent: "accepted-native-ancestor"}
				state := &PersistenceState{APIKey: owner}
				current := []byte(`{"model":"gpt-6-astra","client_metadata":{"thread_id":"recovery-thread"},"input":[{"role":"user","content":"retained detail"},{"type":"compaction","id":"cmp_ancestor","encrypted_content":"accepted-native-ancestor"},{"type":"custom_tool_call","call_id":"recent-call","name":"inspect","input":"exact input 17.125"},{"type":"custom_tool_call_output","call_id":"recent-call","output":"exact result 17.250"},{"type":"compaction_trigger"}]}`)
				require.NoError(t, adapter.retainNativeCompactionSource(ctx, latest, state, &remoteCompactionSource{body: current}))
				if archive == "missing predecessor" {
					delta := []byte(`{"model":"gpt-6-astra","previous_response_id":"resp_expired","input":[{"role":"user","content":"archival delta"},{"type":"compaction_trigger"}]}`)
					require.NoError(t, adapter.retainUnresolvedNativeSource(ctx, ancestor, state, &remoteCompactionSource{body: delta}))
				} else {
					old := []byte(`{"model":"gpt-6-astra","input":[{"role":"user","content":"retained detail"},{"role":"assistant","content":"old archive"},{"type":"compaction_trigger"}]}`)
					old, err = sjson.SetBytes(old, "input.1.content", strings.Repeat("previously compacted original history ", 8000))
					require.NoError(t, err)
					require.NoError(t, adapter.retainNativeCompactionSource(ctx, ancestor, state, &remoteCompactionSource{body: old}))
				}
				executor := &canonicalCheckpointExecutor{maxBytes: 64 << 10}
				configure := func(state *PersistenceState, outbound *PersistentOutboundTransformer) pipeline.Middleware {
					state.APIKey = owner
					return recoverRejectedRemoteCompaction(outbound, adapter, executor)
				}
				for range 2 {
					_, result, err := runRejectedReasoningPipeline(t, ctx, req, executor, "canonical-recovery-key", true, 2, configure)
					require.NoError(t, err)
					drainRejectedReasoningPipeline(t, result)
					adapter = newRemoteCompactionAdapter(adapter.requestService, nil, adapter.systemService)
				}
				require.Len(t, executor.summaries, 1, "persist the completed summary and reuse it after restart")
				summary := executor.summaries[0]
				require.Equal(t, "accepted-native-ancestor", gjson.GetBytes(summary, `input.#(type=="compaction").encrypted_content`).String())
				require.Contains(t, string(summary), "exact input 17.125")
				require.Contains(t, string(summary), "exact result 17.250")
				require.NotContains(t, string(summary), "previously compacted original history")
				require.Len(t, executor.continuations, 3)
				assertRejectedCompactionWindowPreserved(t, executor.continuations[0], executor.continuations[1])
				assertRejectedCompactionWindowPreserved(t, executor.continuations[0], executor.continuations[2])
				stored, err := adapter.loadNativeCompactionSource(ctx, latest, state)
				require.NoError(t, err)
				require.JSONEq(t, string(current), string(stored.body))
			})
		}
	}
}

func TestCheckpointCanonicalRecoveryDoesNotInventMissingHistory(t *testing.T) {
	ctx, _, state, adapter := unifiedHistoryFixture(t)
	latest := &remoteCompactionReference{ID: "cmp_latest", EncryptedContent: "latest-cipher"}
	current := []byte(`{"model":"gpt-6-astra","client_metadata":{"thread_id":"recovery-thread"},"input":[{"type":"compaction","id":"cmp_ancestor","encrypted_content":"native-ancestor"},{"role":"user","content":"new source detail"},{"type":"compaction_trigger"}]}`)
	require.NoError(t, adapter.retainNativeCompactionSource(ctx, latest, state, &remoteCompactionSource{body: current}))
	executor := &lazyCompactionExecutor{reject: true}
	summary, err := adapter.summaryForCompaction(ctx, remoteCompactionCacheKey(latest), latest, "recovery-thread", "gpt-6-astra", state, executor)
	require.ErrorContains(t, err, "no retained source")
	require.Empty(t, summary)
	require.Len(t, executor.bodies, 1, "try the canonical window, then stop when its rejected source is unavailable")
	require.Empty(t, executor.summaries)
	sealed, err := adapter.systemService.LoadLocalCompactionCheckpoint(ctx, state.APIKey.ProjectID, state.APIKey.ID, remoteCompactionCacheKey(latest))
	require.NoError(t, err)
	require.Empty(t, sealed)
}

func TestCheckpointCanonicalRecoveryPreservesOtherFailures(t *testing.T) {
	for _, scenario := range []string{"unrelated error", "partial summary", "cycle"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, _, state, adapter := unifiedHistoryFixture(t)
			latest := &remoteCompactionReference{ID: "cmp_latest", EncryptedContent: "latest-cipher"}
			current := []byte(`{"model":"gpt-6-astra","client_metadata":{"thread_id":"recovery-thread"},"input":[{"type":"compaction","id":"cmp_ancestor","encrypted_content":"native-ancestor"},{"role":"user","content":"complete current detail"},{"type":"compaction_trigger"}]}`)
			if scenario == "cycle" {
				current = []byte(strings.ReplaceAll(strings.ReplaceAll(string(current), "cmp_ancestor", latest.ID), "native-ancestor", latest.EncryptedContent))
			}
			require.NoError(t, adapter.retainNativeCompactionSource(ctx, latest, state, &remoteCompactionSource{body: current}))
			executor := &canonicalCheckpointFailureExecutor{partial: scenario == "partial summary"}
			summary, err := adapter.summaryForCompaction(ctx, remoteCompactionCacheKey(latest), latest, "recovery-thread", "gpt-6-astra", state, executor)
			require.Error(t, err)
			require.Empty(t, summary)
			if scenario == "cycle" {
				require.ErrorContains(t, err, "cyclic")
				require.Zero(t, executor.calls)
			} else {
				require.Equal(t, 1, executor.calls)
				if scenario == "partial summary" {
					require.ErrorIs(t, err, llm.ErrStreamIncomplete)
				} else {
					require.ErrorContains(t, err, "Invalid tool schema")
				}
			}
			sealed, err := adapter.systemService.LoadLocalCompactionCheckpoint(ctx, state.APIKey.ProjectID, state.APIKey.ID, remoteCompactionCacheKey(latest))
			require.NoError(t, err)
			require.Empty(t, sealed, "failed or partial generation must never become an authoritative checkpoint")
		})
	}
}

type canonicalCheckpointFailureExecutor struct {
	rejectedCompactionBridgeExecutor

	partial bool
	calls   int
}

func (e *canonicalCheckpointFailureExecutor) DoStream(context.Context, *httpclient.Request) (streams.Stream[*httpclient.StreamEvent], error) {
	e.calls++
	if e.partial {
		return streams.SliceStream([]*httpclient.StreamEvent{{Type: "response.output_text.delta", Data: []byte(`{"type":"response.output_text.delta","delta":"incomplete summary"}`)}}), nil
	}
	return nil, &httpclient.Error{StatusCode: 400, Body: []byte(`{"error":{"type":"invalid_request_error","message":"Invalid tool schema"}}`)}
}

type canonicalCheckpointExecutor struct {
	rejectedCompactionBridgeExecutor

	maxBytes int
}

func (e *canonicalCheckpointExecutor) DoStream(ctx context.Context, req *httpclient.Request) (streams.Stream[*httpclient.StreamEvent], error) {
	if len(req.Body) > e.maxBytes {
		return nil, &httpclient.Error{StatusCode: http.StatusBadRequest, Body: []byte(`{"error":{"type":"invalid_request_error","code":"context_length_exceeded","param":"input","message":"Your input exceeds the context window of this model. Please adjust your input and try again."}}`)}
	}
	return e.rejectedCompactionBridgeExecutor.DoStream(ctx, req)
}

func TestCheckpointCanonicalRecoveryExpandsOnlyRejectedAncestor(t *testing.T) {
	for _, reject := range []bool{false, true} {
		t.Run(fmt.Sprintf("reject=%t", reject), func(t *testing.T) {
			ctx, _, state, adapter := unifiedHistoryFixture(t)
			latest := &remoteCompactionReference{ID: "cmp_latest", EncryptedContent: "latest-cipher"}
			ancestor := &remoteCompactionReference{ID: "cmp_ancestor", EncryptedContent: "native-ancestor"}
			root := &remoteCompactionReference{ID: "cmp_root", EncryptedContent: "readable-root"}
			first := []byte(`{"model":"gpt-6-astra","client_metadata":{"thread_id":"recovery-thread"},"input":[{"role":"user","content":"retained detail"},{"type":"compaction","id":"cmp_root","encrypted_content":"readable-root"},{"role":"assistant","content":"exact intermediate detail"},{"type":"compaction_trigger"}]}`)
			current := []byte(`{"model":"gpt-6-astra","client_metadata":{"thread_id":"recovery-thread"},"input":[{"role":"user","content":"retained detail"},{"type":"compaction","id":"cmp_ancestor","encrypted_content":"native-ancestor"},{"role":"user","content":"new source detail"},{"type":"compaction_trigger"}]}`)
			require.NoError(t, adapter.retainNativeCompactionSource(ctx, latest, state, &remoteCompactionSource{body: current}))
			require.NoError(t, adapter.retainNativeCompactionSource(ctx, ancestor, state, &remoteCompactionSource{body: first}))
			delta := []byte(`{"model":"gpt-6-astra","previous_response_id":"resp_missing","input":[{"type":"compaction_trigger"}]}`)
			require.NoError(t, adapter.retainUnresolvedNativeSource(ctx, root, state, &remoteCompactionSource{body: delta}))
			executor := &lazyCompactionExecutor{reject: reject}
			summary, err := adapter.summaryForCompaction(ctx, remoteCompactionCacheKey(latest), latest, "recovery-thread", "gpt-6-astra", state, executor)
			require.NoError(t, err)
			require.Equal(t, "verified retained source summary", summary)
			if reject {
				require.Len(t, executor.bodies, 2)
				require.Contains(t, string(executor.bodies[1]), "exact intermediate detail")
				require.Contains(t, string(executor.bodies[1]), "readable-root")
			} else {
				require.Len(t, executor.bodies, 1)
			}
			require.Len(t, executor.summaries, 1, "do not generate an intermediate ancestor summary")
		})
	}
}
