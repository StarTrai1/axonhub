package orchestrator

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/streams"
)

func TestCheckpointRecoveryResumesAfterCanceledAncestor(t *testing.T) {
	for _, format := range []llm.APIFormat{llm.APIFormatOpenAIResponse, llm.APIFormatOpenAIResponseWebSocket} {
		t.Run(string(format), func(t *testing.T) {
			ctx, _, owner, adapter := rejectedCompactionStorage(t)
			request := rejectedCompactionPipelineRequest(t, format)
			latest, _, _, err := parseRemoteCompactionRequest(request.Body)
			require.NoError(t, err)
			ancestor := &remoteCompactionReference{ID: "cmp_ancestor", EncryptedContent: "ancestor-cipher"}
			source := []byte(`{"model":"gpt-6-astra","client_metadata":{"thread_id":"recovery-thread"},"input":[{"role":"user","content":"retained detail"},{"type":"compaction","id":"cmp_ancestor","encrypted_content":"ancestor-cipher"},{"type":"reasoning","id":"rs_old","encrypted_content":"reasoning-cipher","summary":[]},{"type":"custom_tool_call","id":"ct_old","call_id":"call_one","name":"inspect","input":"17.125"},{"type":"custom_tool_call_output","id":"out_old","call_id":"call_one","output":"17.250"},{"type":"agent_message","id":"amsg_one","author":"/root/reviewer","recipient":"/root","content":[{"type":"input_text","text":"complete reviewer evidence"}]},{"type":"compaction_trigger"}]}`)
			parent := []byte(`{"model":"gpt-6-astra","client_metadata":{"thread_id":"recovery-thread"},"input":[{"role":"user","content":"retained detail"},{"type":"compaction","id":"cmp_untested","encrypted_content":"untested-cipher"},{"role":"assistant","content":"exact ancestor source"},{"type":"compaction_trigger"}]}`)
			storageState := &PersistenceState{APIKey: owner}
			require.NoError(t, adapter.retainNativeCompactionSource(ctx, latest, storageState, &remoteCompactionSource{body: source}))
			require.NoError(t, adapter.retainNativeCompactionSource(ctx, ancestor, storageState, &remoteCompactionSource{body: parent}))
			// Deliberately retain no source for cmp_untested: cancellation must not
			// authorize expanding an ancestor the provider has not rejected.
			executor := &checkpointResumeExecutor{cancelNext: true}
			configure := func(state *PersistenceState, outbound *PersistentOutboundTransformer) pipeline.Middleware {
				state.APIKey = owner
				state.CurrentCandidate = state.ChannelModelsCandidates[0]
				state.CurrentCandidate.Channel.Credentials = objects.ChannelCredentials{APIKey: "checkpoint-resume-key"}
				state.Settings = &RequestSettings{RetryPolicy: biz.RetryPolicy{Enabled: true, MaxSingleChannelRetries: 3}}
				return recoverRejectedRemoteCompaction(outbound, adapter, executor)
			}
			_, _, err = runRejectedReasoningPipeline(t, ctx, request, executor, "checkpoint-resume-key", true, 3, configure)
			require.ErrorIs(t, err, context.Canceled)
			require.Len(t, executor.ancestorBodies, 2)
			require.Equal(t, 1, executor.nextCalls)
			require.Empty(t, executor.summaries)
			require.Equal(t, "reasoning-cipher", gjson.GetBytes(executor.ancestorBodies[1], `input.#(type=="reasoning").encrypted_content`).String())
			require.False(t, gjson.GetBytes(executor.ancestorBodies[1], `input.#(type=="reasoning").id`).Exists())

			// A new adapter simulates restart; a reconnect may use a new window.
			adapter = newRemoteCompactionAdapter(adapter.requestService, nil, adapter.systemService)
			executor = &checkpointResumeExecutor{}
			request.Headers.Set("X-Codex-Window-Id", "recovery-thread:2")
			_, result, err := runRejectedReasoningPipeline(t, ctx, request, executor, "checkpoint-resume-key", true, 3, configure)
			require.NoError(t, err)
			drainRejectedReasoningPipeline(t, result)
			require.Empty(t, executor.ancestorBodies, "resume must not repeat the two already rejected ancestor requests")
			require.Equal(t, 1, executor.nextCalls)
			require.Len(t, executor.summaries, 1)
			summarySource := executor.summaries[0]
			require.Contains(t, string(summarySource), "untested-cipher")
			require.Contains(t, string(summarySource), "exact ancestor source")
			require.Equal(t, "17.125", gjson.GetBytes(summarySource, `input.#(type=="custom_tool_call").input`).String())
			require.Equal(t, "17.250", gjson.GetBytes(summarySource, `input.#(type=="custom_tool_call_output").output`).String())
			require.JSONEq(t, gjson.GetBytes(source, `input.#(type=="agent_message")`).Raw, gjson.GetBytes(summarySource, `input.#(type=="agent_message")`).Raw)
			stored, err := adapter.loadNativeCompactionSource(ctx, latest, storageState)
			require.NoError(t, err)
			require.JSONEq(t, string(source), string(stored.body))
		})
	}
}

type checkpointResumeExecutor struct {
	rejectedCompactionBridgeExecutor

	ancestorBodies [][]byte
	nextCalls      int
	cancelNext     bool
}

func (e *checkpointResumeExecutor) DoStream(ctx context.Context, req *httpclient.Request) (streams.Stream[*httpclient.StreamEvent], error) {
	switch gjson.GetBytes(req.Body, `input.#(type=="compaction").id`).String() {
	case "cmp_native":
		return nil, rejectedNativeCheckpointError()
	case "cmp_ancestor":
		e.ancestorBodies = append(e.ancestorBodies, append([]byte(nil), req.Body...))
		return nil, resourceMismatchHTTPError()
	case "cmp_untested":
		e.nextCalls++
		if e.cancelNext {
			return nil, context.Canceled
		}
	}
	return e.rejectedCompactionBridgeExecutor.DoStream(ctx, req)
}
