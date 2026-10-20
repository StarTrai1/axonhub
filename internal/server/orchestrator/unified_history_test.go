package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/ent/system"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/oauth"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/transformer/openai/codex"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

func unifiedHistoryFixture(t *testing.T) (context.Context, *ent.Client, *PersistenceState, *remoteCompactionAdapter) {
	t.Helper()
	ctx, client, owner, adapter := rejectedCompactionStorage(t)
	provider, err := codex.NewOutboundTransformer(codex.Params{BaseURL: "https://example.invalid/v1", TokenProvider: oauth.NewStaticTokenProvider(&oauth.OAuthCredentials{AccessToken: "synthetic-history-key"})})
	require.NoError(t, err)
	candidate := &ChannelModelsCandidate{Channel: &biz.Channel{Channel: &ent.Channel{ID: 93009, Type: channel.TypeCodex, Settings: &objects.ChannelSettings{PassThroughBody: lo.ToPtr(true)}}, Outbound: provider}, Models: []biz.ChannelModelEntry{{RequestModel: "gpt-6-astra", ActualModel: "gpt-6-astra", Source: "direct"}}, APIFormat: string(llm.APIFormatOpenAIResponse), ContextTokens: 372000}
	return ctx, client, &PersistenceState{APIKey: owner, ChannelModelsCandidates: []*ChannelModelsCandidate{candidate}, CurrentCandidate: candidate}, adapter
}

func TestUnifiedHistoryDurabilityDeduplicationAndIsolation(t *testing.T) {
	ctx, client, state, adapter := unifiedHistoryFixture(t)
	body := []byte(`{"model":"gpt-6-astra","client_metadata":{"thread_id":"history-thread"},"input":[{"role":"user","content":"never alter calibration 17.125"},{"role":"assistant","content":"same exact repeated content"},{"role":"assistant","content":"same exact repeated content"}]}`)
	response := []byte(`{"id":"resp_history_source","status":"completed","output":[{"type":"function_call","call_id":"history_call","name":"inspect","arguments":"{\"value\":17.125}"}]}`)
	require.NoError(t, adapter.archiveHistoryResponse(ctx, body, response))
	blocksBefore, err := client.System.Query().Where(system.KeyContains(":block:")).Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 3, blocksBefore, "identical items share storage but not positions")
	got, output, found, err := adapter.loadHistorySnapshot(ctx, historyResponseAlias("resp_history_source"))
	require.NoError(t, err)
	require.True(t, found)
	require.JSONEq(t, string(body), string(got))
	require.JSONEq(t, string(response), string(output))
	delta := []byte(`{"model":"gpt-6-astra","previous_response_id":"resp_history_source","client_metadata":{"thread_id":"history-thread"},"input":[{"type":"function_call_output","call_id":"history_call","output":"verified 17.125"}]}`)
	resolved, err := adapter.resolveCompactionSourceContinuation(ctx, &remoteCompactionSource{body: delta}, state)
	require.NoError(t, err)
	require.Len(t, gjson.GetBytes(resolved.body, "input").Array(), 5)
	require.NoError(t, adapter.archiveHistoryResponse(ctx, resolved.body, []byte(`{"id":"resp_history_next","status":"completed","output":[]}`)))
	blocksAfter, err := client.System.Query().Where(system.KeyContains(":block:")).Count(ctx)
	require.NoError(t, err)
	require.Equal(t, blocksBefore+1, blocksAfter, "a continuation writes only the new materialized item")
	_, err = client.Request.Delete().Exec(ctx)
	require.NoError(t, err)
	adapter = newRemoteCompactionAdapter(nil, nil, adapter.systemService)
	got, _, found, err = adapter.loadHistorySnapshot(ctx, historyResponseAlias("resp_history_next"))
	require.NoError(t, err)
	require.True(t, found)
	require.JSONEq(t, string(resolved.body), string(got))
	foreign := contexts.WithAPIKey(contexts.CloneRequestContext(ctx), &ent.APIKey{ID: state.APIKey.ID + 1, ProjectID: state.APIKey.ProjectID})
	_, _, found, err = adapter.loadHistorySnapshot(foreign, historyResponseAlias("resp_history_next"))
	require.NoError(t, err)
	require.False(t, found)
	other, err := sjson.SetBytes(body, "input.0.content", "different sibling branch")
	require.NoError(t, err)
	require.ErrorContains(t, adapter.archiveHistoryResponse(ctx, other, response), "different immutable version")
	got, _, _, err = adapter.loadHistorySnapshot(ctx, historyResponseAlias("resp_history_source"))
	require.NoError(t, err)
	require.JSONEq(t, string(body), string(got))
	rows, err := client.System.Query().Where(system.KeyHasPrefix("conversation_history_v1:")).All(ctx)
	require.NoError(t, err)
	for _, row := range rows {
		require.NotContains(t, row.Value, "17.125")
		require.NotContains(t, row.Value, "calibration")
	}
	block, err := client.System.Query().Where(system.KeyHasSuffix(":block:" + historyDigest([]byte(`{"role":"user","content":"never alter calibration 17.125"}`)))).Only(ctx)
	require.NoError(t, err)
	require.NoError(t, block.Update().SetValue("tampered-ciphertext").Exec(ctx))
	_, _, _, err = adapter.loadHistorySnapshot(ctx, historyResponseAlias("resp_history_source"))
	require.Error(t, err, "corrupt source must not silently fall back to unrelated history")
}

func TestUnifiedHistoryNativeAndLocalCheckpointUseOriginals(t *testing.T) {
	for _, local := range []bool{false, true} {
		t.Run(fmt.Sprint(local), func(t *testing.T) {
			ctx, _, state, adapter := unifiedHistoryFixture(t)
			ref := &remoteCompactionReference{ID: "cmp_original", EncryptedContent: "native-cipher"}
			if local {
				var err error
				ref, err = newLocalCompactionReference()
				require.NoError(t, err)
				g := &localCompactionGeneration{ref: ref}
				g.summaryCipher, err = adapter.localCompactionCipher(ctx)
				require.NoError(t, err)
				g.associatedData, err = localCompactionAssociatedData(ref, state)
				require.NoError(t, err)
				require.NoError(t, g.sealSummary("lossy summary omitting the calibration"))
			}
			source := []byte(`{"model":"gpt-6-astra","reasoning":{"effort":"xhigh"},"input":[{"role":"user","content":"calibration must stay exactly 17.125"},{"type":"function_call","call_id":"call_original","name":"inspect","arguments":"{\"x\":17.125}"},{"type":"function_call_output","call_id":"call_original","output":"original unabridged result"},{"type":"compaction_trigger"}]}`)
			require.NoError(t, adapter.saveHistorySnapshot(ctx, historyCheckpointAlias(ref), source, nil))
			body := []byte(fmt.Sprintf(`{"model":"gpt-6-astra","reasoning":{"effort":"xhigh"},"input":[{"type":"compaction","id":%q,"encrypted_content":%q},{"role":"user","content":"correction: use 17.250 for the new branch"}]}`, ref.ID, ref.EncryptedContent))
			executor := &compactionEvidenceExecutor{}
			compiled, ok, err := adapter.compileUnifiedContext(ctx, body, state, executor)
			require.NoError(t, err)
			require.True(t, ok)
			require.Contains(t, string(compiled), "17.125")
			require.Contains(t, string(compiled), "17.250")
			require.Contains(t, string(compiled), "original unabridged result")
			require.NotContains(t, string(compiled), "lossy summary")
			require.Empty(t, executor.summaries)
			require.Equal(t, "xhigh", gjson.GetBytes(compiled, "reasoning.effort").String())
			_, complete := responsesResourceHistorySupportsRecovery(compiled)
			require.True(t, complete)
		})
	}
}

func TestUnifiedHistorySegmentSummariesKeepRawInstructionsAndReuse(t *testing.T) {
	ctx, _, state, adapter := unifiedHistoryFixture(t)
	var input []json.RawMessage
	for i := range 8 {
		user, _ := json.Marshal(map[string]any{"role": "user", "content": fmt.Sprintf("constraint_%d: preserve calibration %d.125", i, i)})
		assistant, _ := json.Marshal(map[string]any{"role": "assistant", "content": fmt.Sprintf("original evidence_%d ", i) + historyTestEntropy(45000, uint64(i))})
		input = append(input, user, assistant)
	}
	input = append(input, json.RawMessage(`{"role":"user","content":"latest correction: calibration 17.250; continue"}`))
	body, err := json.Marshal(map[string]any{"model": "gpt-6-astra", "reasoning": map[string]string{"effort": "xhigh"}, "input": input})
	require.NoError(t, err)
	executor := &unifiedConcurrentExecutor{}
	first, ok, err := adapter.compileUnifiedContext(ctx, body, state, executor)
	require.NoError(t, err)
	require.True(t, ok)
	require.Len(t, executor.summaries, 3)
	for i := range 8 {
		require.Contains(t, string(first), fmt.Sprintf("constraint_%d", i))
	}
	require.Contains(t, string(first), "latest correction: calibration 17.250")
	for _, summary := range executor.summaries {
		require.NotContains(t, string(summary), "verified retained source summary")
		require.Contains(t, string(summary), "original evidence_")
	}
	adapter = newRemoteCompactionAdapter(adapter.requestService, nil, adapter.systemService)
	changed, err := sjson.SetBytes(body, "client_metadata.turn_id", "a-new-turn")
	require.NoError(t, err)
	second, ok, err := adapter.compileUnifiedContext(ctx, changed, state, executor)
	require.NoError(t, err)
	require.True(t, ok)
	require.Len(t, executor.summaries, 3, "turn identifiers must not force a second summary of the same raw segment")
	require.JSONEq(t, gjson.GetBytes(first, "input").Raw, gjson.GetBytes(second, "input").Raw, "the reusable prefix is stable")
	input = append(input, json.RawMessage(`{"role":"assistant","content":"latest exact evidence"}`), json.RawMessage(`{"role":"user","content":"new unrelated turn"}`))
	grown, err := json.Marshal(map[string]any{"model": "gpt-6-astra", "reasoning": map[string]string{"effort": "xhigh"}, "input": input})
	require.NoError(t, err)
	next, ok, err := adapter.compileUnifiedContext(ctx, grown, state, executor)
	require.NoError(t, err)
	require.True(t, ok)
	require.Len(t, executor.summaries, 3, "a growing partial segment must not trigger a new overlapping summary each turn")
	require.Contains(t, string(next), "latest exact evidence")
	require.Contains(t, string(next), "new unrelated turn")
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, _, err = adapter.compileUnifiedContext(canceled, body, state, executor)
	require.ErrorIs(t, err, context.Canceled)
}

func TestUnifiedHistoryGenerationAvoidsAdditionalProviderCall(t *testing.T) {
	ctx, _, owner, adapter := rejectedCompactionStorage(t)
	req := rejectedCompactionPipelineRequest(t, llm.APIFormatOpenAIResponse)
	req.Body = []byte(`{"model":"gpt-6-astra","stream":true,"client_metadata":{"thread_id":"recovery-thread"},"input":[{"role":"user","content":"retain this exact original constraint"},{"type":"compaction_trigger"}]}`)
	executor := &compactionEvidenceExecutor{}
	_, result, err := runRejectedReasoningPipeline(t, ctx, req, executor, "unified-generation-key", true, 1, func(state *PersistenceState, outbound *PersistentOutboundTransformer) pipeline.Middleware {
		state.APIKey = owner
		state.ChannelModelsCandidates[0].ContextTokens = 372000
		state.ChannelModelsCandidates[0].Channel.Policies.RemoteCompaction = objects.RemoteCompactionPolicyLocalBridge
		inbound, _ := NewPersistentTransformers(state, responses.NewInboundTransformer())
		return adaptRemoteCompactionForUnsupportedChannels(inbound, adapter, executor)
	})
	require.NoError(t, err)
	events := drainRejectedReasoningPipeline(t, result)
	require.Contains(t, events["response.completed"], "compaction")
	require.Zero(t, executor.nativeCalls)
	require.Empty(t, executor.summaries)
	require.Empty(t, executor.continuations)
	var ref *remoteCompactionReference
	for _, item := range gjson.Get(events["response.completed"], "response.output").Array() {
		if item.Get("type").String() == "compaction" {
			ref = &remoteCompactionReference{ID: item.Get("id").String(), EncryptedContent: item.Get("encrypted_content").String()}
		}
	}
	require.NotNil(t, ref)
	body, _, found, err := adapter.loadHistorySnapshot(ctx, historyCheckpointAlias(ref))
	require.NoError(t, err)
	require.True(t, found)
	require.Contains(t, string(body), "retain this exact original constraint")
	responseID := gjson.Get(events["response.completed"], "response.id").String()
	stored, completed, found, err := adapter.loadHistorySnapshot(ctx, historyResponseAlias(responseID))
	require.NoError(t, err)
	require.True(t, found)
	require.False(t, gjson.GetBytes(stored, `input.#(type=="compaction_trigger")`).Exists(), "completed control frames must not reappear in the next delta")
	require.Equal(t, ref.ID, gjson.GetBytes(completed, "output.0.id").String())
	require.Equal(t, "compaction", gjson.GetBytes(completed, "output.0.type").String(), "internal assistant summary must never become the published checkpoint output")
}

func TestUnifiedHistoryPortableReasoningAndToolBoundaries(t *testing.T) {
	agent := json.RawMessage(`{"type":"agent_message","id":"amsg_original","author":"/root/reviewer","recipient":"/root","content":[{"type":"input_text","text":"exact recovered reviewer evidence"}]}`)
	preserved, _, portable := unifiedPortableItems([]json.RawMessage{agent})
	require.True(t, portable)
	require.JSONEq(t, string(agent), string(preserved[0]))
	_, _, portable = unifiedPortableItems([]json.RawMessage{json.RawMessage(`{"type":"agent_message","author":"/root/reviewer","recipient":"/root","content":[{"type":"encrypted_content","encrypted_content":"opaque"}]}`)})
	require.False(t, portable, "opaque agent payloads must not be replaced by their visible prefix")

	items := []json.RawMessage{json.RawMessage(`{"type":"reasoning","encrypted_content":"private","summary":[{"text":"visible rationale"}]}`), json.RawMessage(`{"type":"function_call","call_id":"call","name":"tool","arguments":"{\"x\":1}"}`)}
	got, opaque, ok := unifiedPortableItems(items)
	require.True(t, ok)
	require.Equal(t, 1, opaque)
	require.Contains(t, string(got[0]), "visible rationale")
	require.JSONEq(t, string(items[1]), string(got[1]))
	_, _, ok = unifiedPortableItems([]json.RawMessage{json.RawMessage(`{"type":"function_call","encrypted_function_args":["cipher"],"arguments":""}`)})
	require.False(t, ok)
	_, _, ok = unifiedPortableItems([]json.RawMessage{json.RawMessage(`{"role":"user","content":[{"type":"input_image","image_url":"https://example.invalid/image"}]}`)})
	require.True(t, ok, "complete images remain in the original context")
	_, _, ok = unifiedPortableItems([]json.RawMessage{json.RawMessage(`{"role":"user","content":[{"type":"input_image","file_id":"private-file-reference"}]}`)})
	require.False(t, ok, "a provider-private file reference is not portable")
}

var _ pipeline.Executor = (*unifiedCompactionExecutor)(nil)

func TestUnifiedHistoryRestoredPromptProtection(t *testing.T) {
	ctx, _, state, adapter := unifiedHistoryFixture(t)
	ref := &remoteCompactionReference{ID: "cmp_protected", EncryptedContent: "native-cipher"}
	source := []byte(`{"model":"gpt-6-astra","input":[{"role":"user","content":"old secret-123 instruction"},{"type":"compaction_trigger"}]}`)
	require.NoError(t, adapter.saveHistorySnapshot(ctx, historyCheckpointAlias(ref), source, nil))
	body := []byte(`{"model":"gpt-6-astra","input":[{"type":"compaction","id":"cmp_protected","encrypted_content":"native-cipher"},{"role":"user","content":"continue"}]}`)
	rule := &ent.PromptProtectionRule{Pattern: "secret-[0-9]+", Settings: &objects.PromptProtectionSettings{Action: objects.PromptProtectionActionMask, Replacement: "[MASKED]", Scopes: []objects.PromptProtectionScope{objects.PromptProtectionScopeUser}}}
	state.PromptProtecter = &payloadPromptProtecter{}
	unchanged, ok, err := adapter.compileUnifiedContext(ctx, body, state, &compactionEvidenceExecutor{})
	require.NoError(t, err)
	require.True(t, ok)
	require.Contains(t, string(unchanged), "secret-123", "no matched rule leaves verified text unchanged")
	state.PromptProtecter = &legacyPayloadPromptProtecter{rules: []*ent.PromptProtectionRule{rule}}
	_, _, err = adapter.compileUnifiedContext(ctx, body, state, &compactionEvidenceExecutor{})
	require.Error(t, err, "a legacy protector changing text without raw mask evidence must fail closed")
	state.PromptProtecter = &payloadPromptProtecter{rules: []*ent.PromptProtectionRule{rule}}
	compiled, ok, err := adapter.compileUnifiedContext(ctx, body, state, &compactionEvidenceExecutor{})
	require.NoError(t, err)
	require.True(t, ok)
	require.NotContains(t, string(compiled), "secret-123")
	require.Contains(t, string(compiled), "[MASKED]")
	original, _, found, err := adapter.loadHistorySnapshot(ctx, historyCheckpointAlias(ref))
	require.NoError(t, err)
	require.True(t, found)
	require.Contains(t, string(original), "secret-123", "protection must not mutate archived evidence")
	rule.Settings.Action = objects.PromptProtectionActionReject
	_, _, err = adapter.compileUnifiedContext(ctx, body, state, &compactionEvidenceExecutor{})
	require.Error(t, err, "restored history must not bypass a new reject rule")
}

func TestUnifiedHistoryRepeatedCheckpointsFlattenOriginalSources(t *testing.T) {
	ctx, _, state, adapter := unifiedHistoryFixture(t)
	body := []byte(`{"model":"gpt-6-astra","input":[{"role":"user","content":"initial calibration 17.125"},{"type":"compaction_trigger"}]}`)
	var ref *remoteCompactionReference
	for i := range 20 {
		ref = &remoteCompactionReference{ID: fmt.Sprintf("cmp_generation_%d", i), EncryptedContent: "native-cipher"}
		require.NoError(t, adapter.retainNativeCompactionSource(ctx, ref, state, &remoteCompactionSource{body: body}))
		original, _, found, err := adapter.loadHistorySnapshot(ctx, historyCheckpointAlias(ref))
		require.NoError(t, err)
		require.True(t, found)
		require.Contains(t, string(original), "initial calibration 17.125")
		require.False(t, gjson.GetBytes(original, `input.#(type=="compaction")`).Exists(), "new checkpoints must not accumulate ancestry depth")
		body = []byte(fmt.Sprintf(`{"model":"gpt-6-astra","input":[{"type":"compaction","id":%q,"encrypted_content":%q},{"role":"user","content":"correction %d"},{"type":"compaction_trigger"}]}`, ref.ID, ref.EncryptedContent, i))
	}
	compiled, ok, err := adapter.compileUnifiedContext(ctx, body, state, &compactionEvidenceExecutor{})
	require.NoError(t, err)
	require.True(t, ok)
	for i := range 20 {
		require.Contains(t, string(compiled), fmt.Sprintf("correction %d", i))
	}
}

func TestUnifiedHistoryArchivesOriginalInsteadOfProviderSummaryView(t *testing.T) {
	ctx, _, state, adapter := unifiedHistoryFixture(t)
	ref := &remoteCompactionReference{ID: "cmp_origin", EncryptedContent: "native-cipher"}
	source := []byte(`{"model":"gpt-6-astra","input":[{"role":"user","content":"exact original calibration 17.125"},{"type":"compaction_trigger"}]}`)
	require.NoError(t, adapter.retainNativeCompactionSource(ctx, ref, state, &remoteCompactionSource{body: source}))
	original := []byte(`{"model":"gpt-6-astra","input":[{"type":"compaction","id":"cmp_origin","encrypted_content":"native-cipher"},{"role":"user","content":"continue"}]}`)
	ctx = withUnifiedHistoryOrigin(ctx, original)
	provider := []byte(`{"model":"gpt-6-astra","input":[{"role":"assistant","content":"lossy provider view"}]}`)
	require.NoError(t, adapter.archiveHistoryResponse(ctx, provider, []byte(`{"id":"resp_origin","status":"completed","output":[]}`)))
	stored, _, found, err := adapter.loadHistorySnapshot(ctx, historyResponseAlias("resp_origin"))
	require.NoError(t, err)
	require.True(t, found)
	require.Contains(t, string(stored), "exact original calibration 17.125")
	require.NotContains(t, string(stored), "lossy provider view")
	state.unifiedHistorySource = &remoteCompactionSource{body: source}
	ref = &remoteCompactionReference{ID: "cmp_provider_view", EncryptedContent: "new-native-cipher"}
	// The provider may have a transport-local predecessor unknown to the archive.
	require.NoError(t, adapter.retainNativeCompactionSource(ctx, ref, state, &remoteCompactionSource{body: []byte(`{"model":"gpt-6-astra","previous_response_id":"provider-only-id","input":[]}`)}))
	stored, _, found, err = adapter.loadHistorySnapshot(ctx, historyCheckpointAlias(ref))
	require.NoError(t, err)
	require.True(t, found)
	require.Contains(t, string(stored), "exact original calibration 17.125")
}

func TestUnifiedHistoryGroupsKeepInterruptedToolPairs(t *testing.T) {
	items := []json.RawMessage{
		json.RawMessage(`{"role":"user","content":"inspect original source"}`),
		json.RawMessage(`{"type":"function_call","name":"inspect","call_id":"call_original","arguments":"{}"}`),
		json.RawMessage(`{"role":"user","content":"interruption: preserve calibration 17.125"}`),
		json.RawMessage(`{"type":"function_call_output","call_id":"call_original","output":"exact original result"}`),
		json.RawMessage(`{"role":"user","content":"continue"}`),
	}
	groups := unifiedHistoryGroups(items)
	require.Len(t, groups, 2)
	require.Len(t, groups[0], 4, "an interruption cannot separate the original call from its result")
	require.Len(t, groups[1], 1)
}
