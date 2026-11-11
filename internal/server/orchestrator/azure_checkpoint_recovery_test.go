package orchestrator

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/streams"
)

func TestAzureCheckpointContinuationStagesResourceRecovery(t *testing.T) {
	for _, format := range []llm.APIFormat{llm.APIFormatOpenAIResponse, llm.APIFormatOpenAIResponseWebSocket} {
		t.Run(string(format), func(t *testing.T) {
			ctx, client, owner, adapter := rejectedCompactionStorage(t)
			seedRejectedCompactionSource(t, ctx, client, owner, "completed", "native-checkpoint-ciphertext")
			req := rejectedCompactionPipelineRequest(t, format)
			var err error
			req.Body, err = sjson.SetRawBytes(req.Body, "input.-1", []byte(`{"type":"reasoning","id":"rs_suffix","encrypted_content":"suffix-cipher","summary":[]}`))
			require.NoError(t, err)
			executor := &rejectedCompactionBridgeExecutor{}
			_, result, err := runRejectedReasoningPipeline(t, ctx, req, executor, "synthetic-relay-key", true, 3,
				func(state *PersistenceState, outbound *PersistentOutboundTransformer) pipeline.Middleware {
					state.APIKey = owner
					state.CurrentCandidate = state.ChannelModelsCandidates[0]
					state.CurrentCandidate.Channel.Credentials = objects.ChannelCredentials{APIKey: "synthetic-relay-key"}
					return pipeline.OnRawRequest("relay-target", func(_ context.Context, request *httpclient.Request) (*httpclient.Request, error) {
						request.URL = "https://relay.example/v1/responses"
						return request, nil
					})
				},
				func(_ *PersistenceState, outbound *PersistentOutboundTransformer) pipeline.Middleware {
					return recoverRejectedRemoteCompaction(outbound, adapter, executor)
				},
				func(_ *PersistenceState, outbound *PersistentOutboundTransformer) pipeline.Middleware {
					return applyAzureResponsesHistoryCompatibility(outbound)
				})
			require.NoError(t, err)
			drainRejectedReasoningPipeline(t, result)
			require.Len(t, executor.continuations, 3)
			require.Len(t, executor.summaries, 1)
			second := executor.continuations[1]
			require.True(t, gjson.GetBytes(second, `input.#(type=="compaction")`).Exists())
			require.Equal(t, "suffix-cipher", gjson.GetBytes(second, `input.#(type=="reasoning").encrypted_content`).String())
			require.False(t, gjson.GetBytes(second, `input.#(type=="reasoning").id`).Exists())
			require.Contains(t, string(executor.continuations[2]), "verified retained source summary")
			require.False(t, gjson.GetBytes(executor.continuations[2], `input.#(type=="compaction")`).Exists())
		})
	}
}

func TestAzureCheckpointSummaryRestoresOnlyRejectedAncestor(t *testing.T) {
	for _, rejectAncestor := range []bool{false, true} {
		t.Run(fmt.Sprint(rejectAncestor), func(t *testing.T) {
			ctx, _, state, adapter := unifiedHistoryFixture(t)
			state.CurrentCandidate.Channel.Credentials = objects.ChannelCredentials{APIKey: "synthetic-relay-key"}
			state.Settings = &RequestSettings{RetryPolicy: biz.RetryPolicy{Enabled: true, MaxSingleChannelRetries: 3}}
			latest := &remoteCompactionReference{ID: "cmp_latest", EncryptedContent: "latest-cipher"}
			ancestor := &remoteCompactionReference{ID: "cmp_ancestor", EncryptedContent: "ancestor-cipher"}
			current := []byte(`{"model":"gpt-6-astra","client_metadata":{"thread_id":"recovery-thread"},"input":[{"role":"user","content":"retained detail"},{"type":"compaction","id":"cmp_ancestor","encrypted_content":"ancestor-cipher"},{"type":"reasoning","id":"rs_old","encrypted_content":"reasoning-cipher","summary":[]},{"type":"custom_tool_call","id":"ct_old","call_id":"call_one","name":"inspect","input":"17.125"},{"type":"custom_tool_call_output","id":"out_old","call_id":"call_one","output":"17.250"},{"type":"compaction_trigger"}]}`)
			parent := []byte(`{"model":"gpt-6-astra","client_metadata":{"thread_id":"recovery-thread"},"input":[{"role":"user","content":"retained detail"},{"role":"assistant","content":"exact ancestor source"},{"type":"compaction_trigger"}]}`)
			require.NoError(t, adapter.retainNativeCompactionSource(ctx, latest, state, &remoteCompactionSource{body: current}))
			if rejectAncestor {
				require.NoError(t, adapter.retainNativeCompactionSource(ctx, ancestor, state, &remoteCompactionSource{body: parent}))
			}
			executor := &azureCheckpointSummaryExecutor{rejectAncestor: rejectAncestor}
			summary, err := adapter.summaryForCompaction(ctx, remoteCompactionCacheKey(latest), latest, "recovery-thread", "gpt-6-astra", state, executor)
			require.NoError(t, err)
			require.Equal(t, "verified retained source summary", summary)
			require.Len(t, executor.summaries, 1, "only the current checkpoint gets a summary")
			require.False(t, gjson.GetBytes(executor.bodies[1], `input.#(type=="reasoning").id`).Exists())
			require.True(t, gjson.GetBytes(executor.bodies[1], `input.#(type=="compaction")`).Exists())
			if rejectAncestor {
				require.Len(t, executor.bodies, 3)
				require.Contains(t, string(executor.summaries[0]), "exact ancestor source")
			} else {
				require.Len(t, executor.bodies, 2)
				require.Contains(t, string(executor.summaries[0]), "ancestor-cipher")
			}
			require.Equal(t, "17.125", gjson.GetBytes(executor.summaries[0], `input.#(type=="custom_tool_call").input`).String())
			require.Equal(t, "17.250", gjson.GetBytes(executor.summaries[0], `input.#(type=="custom_tool_call_output").output`).String())
			stored, err := adapter.loadNativeCompactionSource(ctx, latest, state)
			require.NoError(t, err)
			require.JSONEq(t, string(current), string(stored.body))
		})
	}
}

type azureCheckpointSummaryExecutor struct {
	rejectedCompactionBridgeExecutor

	bodies         [][]byte
	rejectAncestor bool
}

func (e *azureCheckpointSummaryExecutor) DoStream(ctx context.Context, req *httpclient.Request) (streams.Stream[*httpclient.StreamEvent], error) {
	e.bodies = append(e.bodies, append([]byte(nil), req.Body...))
	if len(e.bodies) == 1 || e.rejectAncestor && gjson.GetBytes(req.Body, `input.#(type=="compaction")`).Exists() {
		return nil, resourceMismatchHTTPError()
	}
	return e.rejectedCompactionBridgeExecutor.DoStream(ctx, req)
}
