package orchestrator

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/streams"
)

func TestNativeCompactionMigrationRecoveryBoundaries(t *testing.T) {
	for _, mode := range []string{"unsupported", "target rejected", "overload", "incomplete"} {
		t.Run(mode, func(t *testing.T) {
			ctx, _, owner, adapter := rejectedCompactionStorage(t)
			req := rejectedCompactionPipelineRequest(t, llm.APIFormatOpenAIResponse)
			ref, _, _, err := parseRemoteCompactionRequest(req.Body)
			require.NoError(t, err)
			source := []byte(`{"model":"legacy-context-unknown","client_metadata":{"thread_id":"recovery-thread","x-codex-turn-metadata":"{\"request_kind\":\"compaction\",\"compaction\":{\"implementation\":\"responses_compaction_v2\"}}"},"input":[{"role":"user","content":"full source detail"},{"type":"compaction_trigger"}]}`)
			require.NoError(t, adapter.retainNativeCompactionSource(ctx, ref, &PersistenceState{APIKey: owner}, &remoteCompactionSource{body: source}))
			executor := &nativeMigrationBoundaryExecutor{mode: mode}
			_, result, err := runRejectedReasoningPipeline(t, ctx, req, executor, "native-boundary-key", true, 2, func(state *PersistenceState, outbound *PersistentOutboundTransformer) pipeline.Middleware {
				state.APIKey = owner
				// Unknown model context exercises native migration before the legacy fallback.
				state.ChannelModelsCandidates[0].ContextTokens = 0
				state.ChannelModelsCandidates[0].Models[0].ActualModel = "legacy-context-unknown"
				return recoverRejectedRemoteCompaction(outbound, adapter, executor)
			})
			require.Equal(t, 1, executor.nativeCalls)
			if mode == "overload" || mode == "incomplete" {
				require.Error(t, err)
				require.Nil(t, result)
				require.Empty(t, executor.summaries, "capacity and partial-stream failures cannot start a second generation")
				return
			}
			require.NoError(t, err)
			drainRejectedReasoningPipeline(t, result)
			require.Len(t, executor.summaries, 1)
			require.Contains(t, string(executor.summaries[0]), "full source detail")
		})
	}
}

type nativeMigrationBoundaryExecutor struct {
	rejectedCompactionBridgeExecutor

	mode        string
	nativeCalls int
}

func (e *nativeMigrationBoundaryExecutor) DoStream(ctx context.Context, req *httpclient.Request) (streams.Stream[*httpclient.StreamEvent], error) {
	if gjson.GetBytes(req.Body, `input.#(type=="compaction_trigger")`).Exists() {
		e.nativeCalls++
		switch e.mode {
		case "unsupported":
			return nil, &httpclient.Error{StatusCode: 400, Body: []byte(`{"error":{"type":"invalid_request_error","code":"unsupported_compaction","message":"compaction is unavailable"}}`)}
		case "overload":
			return nil, &httpclient.Error{StatusCode: 429, Body: []byte(`{"error":{"type":"rate_limit_error","message":"synthetic capacity"}}`)}
		case "incomplete":
			return streams.SliceStream([]*httpclient.StreamEvent{{Type: "response.incomplete", Data: []byte(`{"type":"response.incomplete","response":{"id":"resp_incomplete","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output":[]}}`)}}), nil
		default:
			return streams.SliceStream(rejectedReasoningCompactionEvents()), nil
		}
	}
	if gjson.GetBytes(req.Body, `input.#(id=="cmp_output")`).Exists() {
		return nil, &httpclient.Error{StatusCode: 400, Body: []byte(`{"error":{"type":"invalid_request_error","message":"The encrypted content for item cmp_output could not be verified. Reason: Encrypted content could not be decrypted or parsed."}}`)}
	}
	if gjson.GetBytes(req.Body, `input.#(id=="cmp_native")`).Exists() {
		return nil, rejectedNativeCheckpointError()
	}
	return e.rejectedCompactionBridgeExecutor.DoStream(ctx, req)
}
