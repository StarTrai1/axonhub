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

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/streams"
)

func TestNativeCompactionMigrationReusesAncestorEvidence(t *testing.T) {
	for _, large := range []bool{false, true} {
		for _, format := range []llm.APIFormat{llm.APIFormatOpenAIResponse, llm.APIFormatOpenAIResponseWebSocket} {
			t.Run(fmt.Sprintf("large=%t/%s", large, format), func(t *testing.T) {
				ctx, _, owner, adapter := rejectedCompactionStorage(t)
				req := rejectedCompactionPipelineRequest(t, format)
				ref, _, _, err := parseRemoteCompactionRequest(req.Body)
				require.NoError(t, err)
				first := []byte(`{"model":"legacy-context-unknown","client_metadata":{"thread_id":"recovery-thread"},"input":[{"type":"message","role":"user","status":"completed","content":"retained detail"},{"role":"user","content":"unabridged first source"},{"type":"compaction_trigger"}]}`)
				if large {
					first, err = sjson.SetBytes(first, "input.1.content", "unabridged first source"+strings.Repeat(" full history", 6000))
					require.NoError(t, err)
				}
				latest := []byte(`{"model":"legacy-context-unknown","reasoning":{"effort":"xhigh","context":"all_turns"},"client_metadata":{"thread_id":"recovery-thread","x-codex-turn-metadata":"{\"request_kind\":\"compaction\",\"compaction\":{\"implementation\":\"responses_compaction_v2\"}}"},"input":[{"type":"message","role":"user","status":"completed","content":"retained detail"},{"type":"compaction","id":"cmp_ancestor","encrypted_content":"ancestor-cipher"},{"role":"user","content":"new source detail"},{"type":"compaction_trigger"}]}`)
				if large {
					// Match #61053/#61054/#61056: the rejected field belongs to
					// reasoning, whose ID, summary and ciphertext must survive.
					first, err = sjson.DeleteBytes(first, "input.0.status")
					require.NoError(t, err)
					latest, err = sjson.DeleteBytes(latest, "input.0.status")
					require.NoError(t, err)
					latest = []byte(strings.ReplaceAll(string(latest), `{"type":"compaction_trigger"}`, `{"type":"reasoning","id":"rs_source","status":"completed","summary":[],"encrypted_content":"reasoning-cipher"},{"type":"compaction_trigger"}`))
				}
				ancestor := &remoteCompactionReference{ID: "cmp_ancestor", EncryptedContent: "ancestor-cipher"}
				state := &PersistenceState{APIKey: owner}
				require.NoError(t, seedLegacyNativeCompactionSource(ctx, adapter, ancestor, state, &remoteCompactionSource{body: first}))
				require.NoError(t, seedLegacyNativeCompactionSource(ctx, adapter, ref, state, &remoteCompactionSource{body: latest}))
				executor := &compactionEvidenceExecutor{}
				configure := func(state *PersistenceState, outbound *PersistentOutboundTransformer) pipeline.Middleware {
					state.APIKey = owner
					// Exercise the retained compatibility path for a model without
					// a known context contract; normal models now use unified recovery.
					state.ChannelModelsCandidates[0].ContextTokens = 0
					state.ChannelModelsCandidates[0].Models[0].ActualModel = "legacy-context-unknown"
					state.RetryPolicyProvider = &mockRetryPolicyProvider{policy: &biz.RetryPolicy{Enabled: true, MaxSingleChannelRetries: 2}}
					return recoverRejectedRemoteCompaction(outbound, adapter, executor)
				}
				_, result, err := runRejectedReasoningPipeline(t, ctx, req, executor, "ancestor-evidence-key", true, 2, configure)
				require.NoError(t, err)
				drainRejectedReasoningPipeline(t, result)
				require.Zero(t, executor.statusRejections)
				require.Zero(t, executor.ancestorRejections)
				require.Zero(t, executor.nativeCalls)
				wantSummaries := 1
				require.Len(t, executor.summaries, wantSummaries)
				last := executor.summaries[0]
				require.Contains(t, string(last), "unabridged first source")
				require.Contains(t, string(last), "new source detail")
				require.Equal(t, "xhigh", gjson.GetBytes(last, "reasoning.effort").String())
				require.NotContains(t, string(last), "verified retained source summary")
				require.NotContains(t, string(last), "ancestor-cipher")
				require.NotContains(t, string(last), "reasoning-cipher")
				// A new adapter and request still reuse the completed sealed summary.
				adapter = newRemoteCompactionAdapter(adapter.requestService, nil, adapter.systemService)
				_, result, err = runRejectedReasoningPipeline(t, ctx, req, executor, "ancestor-evidence-key", true, 2, configure)
				require.NoError(t, err)
				drainRejectedReasoningPipeline(t, result)
				require.Len(t, executor.summaries, wantSummaries)
				require.Zero(t, executor.ancestorRejections)
			})
		}
	}
}

type compactionEvidenceExecutor struct {
	rejectedCompactionBridgeExecutor

	nativeCalls        int
	statusRejections   int
	ancestorRejections int
}

func (e *compactionEvidenceExecutor) DoStream(ctx context.Context, req *httpclient.Request) (streams.Stream[*httpclient.StreamEvent], error) {
	if gjson.GetBytes(req.Body, `input.#(id=="cmp_native")`).Exists() {
		return nil, rejectedNativeCheckpointError()
	}
	if gjson.GetBytes(req.Body, `input.#(type=="compaction_trigger")`).Exists() {
		e.nativeCalls++
	}
	for i, item := range gjson.GetBytes(req.Body, "input").Array() {
		if item.Get("status").Exists() {
			e.statusRejections++
			return nil, &httpclient.Error{StatusCode: 400, Body: fmt.Appendf(nil, `{"error":{"code":"unknown_parameter","message":"Unknown parameter","param":"input[%d].status"}}`, i)}
		}
	}
	if gjson.GetBytes(req.Body, `input.#(id=="cmp_ancestor")`).Exists() {
		e.ancestorRejections++
		return nil, &httpclient.Error{StatusCode: 400, Body: []byte(`{"error":{"message":"The encrypted content for item cmp_ancestor could not be verified. Reason: Encrypted content could not be decrypted or parsed."}}`)}
	}
	return e.rejectedCompactionBridgeExecutor.DoStream(ctx, req)
}

func TestRemoteCompactionEvidenceDestinationIsolation(t *testing.T) {
	for _, changed := range []string{"same", "channel", "endpoint", "model", "credential", "untyped", "other item type"} {
		t.Run(changed, func(t *testing.T) {
			evidence := &compactionRecoveryEvidence{}
			request := &httpclient.Request{URL: "https://example.invalid/v1/responses", APIFormat: string(llm.APIFormatOpenAIResponse), Headers: http.Header{"Authorization": {"Bearer synthetic"}}, Body: []byte(`{"model":"gpt-6-astra","input":[{"type":"message","role":"user","status":"completed","content":"complete"},{"type":"reasoning","id":"rs_preserved","encrypted_content":"preserved-cipher"}]}`)}
			channelID := 40
			candidate := &ChannelModelsCandidate{Channel: &biz.Channel{Channel: &ent.Channel{ID: channelID, Type: channel.TypeCodex}}}
			state := &PersistenceState{CurrentCandidate: candidate, ChannelModelsCandidates: []*ChannelModelsCandidate{candidate}, RawProviderRequest: request, compactionRecovery: evidence}
			outbound := &PersistentOutboundTransformer{state: state}
			middleware := applyResponsesRejectedStatusCompatibility(outbound)
			if changed == "untyped" {
				request.Body = []byte(`{"model":"gpt-6-astra","input":[{"role":"user","status":"completed","content":"complete"}]}`)
			}
			middleware.OnOutboundRawError(t.Context(), &httpclient.Error{StatusCode: 400, Body: []byte(`{"error":{"code":"unknown_parameter","param":"input[0].status","message":"Unknown parameter"}}`)})
			ref := &remoteCompactionReference{ID: "cmp_one", EncryptedContent: "cipher-one"}
			evidence.rejectSource(responsesMetadataKey(channelID, request), remoteCompactionCacheKey(ref))
			state.responsesRejectedStatusRules = nil // A separate generation has no per-attempt rules.
			switch changed {
			case "channel":
				candidate.Channel.ID++
			case "endpoint":
				request.URL += "/other"
			case "model":
				request.Body, _ = sjson.SetBytes(request.Body, "model", "different-model")
			case "credential":
				request.Headers.Set("Authorization", "Bearer other")
			case "other item type":
				request.Body, _ = sjson.SetBytes(request.Body, "input.0.type", "agent_message")
			}
			original := append([]byte(nil), request.Body...)
			got, err := middleware.OnOutboundRawRequest(t.Context(), request)
			require.NoError(t, err)
			if changed == "same" {
				require.False(t, gjson.GetBytes(got.Body, "input.0.status").Exists())
				require.Equal(t, "preserved-cipher", gjson.GetBytes(got.Body, "input.1.encrypted_content").String())
			} else {
				require.JSONEq(t, string(original), string(got.Body))
			}
			wantRejection := changed == "same" || changed == "untyped" || changed == "other item type"
			require.Equal(t, wantRejection, evidence.sourceRejected(state, ref))
			require.False(t, evidence.sourceRejected(state, &remoteCompactionReference{ID: ref.ID, EncryptedContent: "different-cipher"}))
		})
	}
}

func TestCheckpointMigrationRejectsCyclicSources(t *testing.T) {
	ctx, _, owner, adapter := rejectedCompactionStorage(t)
	ref := &remoteCompactionReference{ID: "cmp_cycle", EncryptedContent: "cycle-cipher"}
	body := []byte(`{"model":"gpt-6-astra","input":[{"type":"compaction","id":"cmp_cycle","encrypted_content":"cycle-cipher"},{"type":"compaction_trigger"}]}`)
	state := &PersistenceState{APIKey: owner}
	require.NoError(t, seedLegacyNativeCompactionSource(ctx, adapter, ref, state, &remoteCompactionSource{body: body}))
	_, err := adapter.restoreCheckpointWindow(ctx, &remoteCompactionSource{body: body}, state)
	var responseErr *llm.ResponseError
	require.ErrorAs(t, err, &responseErr)
	require.Equal(t, "compaction_source_invalid", responseErr.Detail.Code)
}
