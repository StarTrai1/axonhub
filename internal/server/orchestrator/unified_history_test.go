package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
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
	"github.com/looplj/axonhub/llm/transformer/openai/codex"
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

func TestUnifiedHistoryRestoredPromptProtection(t *testing.T) {
	ctx, _, state, adapter := unifiedHistoryFixture(t)
	ref := &remoteCompactionReference{ID: "cmp_protected", EncryptedContent: "native-cipher"}
	source := []byte(`{"model":"gpt-6-astra","input":[{"role":"user","content":"old secret-123 instruction"},{"type":"compaction_trigger"}]}`)
	require.NoError(t, adapter.saveHistorySnapshot(ctx, historyCheckpointAlias(ref), source, nil))
	rule := &ent.PromptProtectionRule{Pattern: "secret-[0-9]+", Settings: &objects.PromptProtectionSettings{Action: objects.PromptProtectionActionMask, Replacement: "[MASKED]", Scopes: []objects.PromptProtectionScope{objects.PromptProtectionScopeUser}}}
	state.PromptProtecter = &payloadPromptProtecter{}
	unchanged, err := protectUnifiedHistory(ctx, source, state)
	require.NoError(t, err)
	require.Contains(t, string(unchanged), "secret-123", "no matched rule leaves verified text unchanged")
	state.PromptProtecter = &legacyPayloadPromptProtecter{rules: []*ent.PromptProtectionRule{rule}}
	_, err = protectUnifiedHistory(ctx, source, state)
	require.Error(t, err, "a legacy protector changing text without raw mask evidence must fail closed")
	state.PromptProtecter = &payloadPromptProtecter{rules: []*ent.PromptProtectionRule{rule}}
	compiled, err := protectUnifiedHistory(ctx, source, state)
	require.NoError(t, err)
	require.NotContains(t, string(compiled), "secret-123")
	require.Contains(t, string(compiled), "[MASKED]")
	original, _, found, err := adapter.loadHistorySnapshot(ctx, historyCheckpointAlias(ref))
	require.NoError(t, err)
	require.True(t, found)
	require.Contains(t, string(original), "secret-123", "protection must not mutate archived evidence")
	rule.Settings.Action = objects.PromptProtectionActionReject
	_, err = protectUnifiedHistory(ctx, source, state)
	require.Error(t, err, "restored history must not bypass a new reject rule")
}

func TestCheckpointSourcesKeepCanonicalWindowsAcrossManyCompactions(t *testing.T) {
	ctx, _, state, adapter := unifiedHistoryFixture(t)
	body := []byte(`{"model":"gpt-6-astra","input":[{"role":"user","content":"initial calibration 17.125"},{"type":"compaction_trigger"}]}`)
	for i := range 40 {
		ref := &remoteCompactionReference{ID: fmt.Sprintf("cmp_generation_%d", i), EncryptedContent: "native-cipher"}
		require.NoError(t, adapter.retainNativeCompactionSource(ctx, ref, state, &remoteCompactionSource{body: body}))
		stored, _, found, err := adapter.loadHistorySnapshot(ctx, historyCheckpointWindowAlias(ref))
		require.NoError(t, err)
		require.True(t, found)
		require.JSONEq(t, string(body), string(stored), "a checkpoint stores exactly its own window")
		body = []byte(fmt.Sprintf(`{"model":"gpt-6-astra","input":[{"type":"compaction","id":%q,"encrypted_content":%q},{"role":"user","content":"correction %d"},{"type":"compaction_trigger"}]}`, ref.ID, ref.EncryptedContent, i))
		require.NoError(t, adapter.archiveHistoryResponse(ctx, body, []byte(fmt.Sprintf(`{"id":"resp_generation_%d","status":"completed","output":[]}`, i))))
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
	require.JSONEq(t, string(original), string(stored))
	require.NotContains(t, string(stored), "lossy provider view")
	retained, err := adapter.loadNativeCompactionSource(ctx, ref, state)
	require.NoError(t, err)
	require.Contains(t, string(retained.body), "exact original calibration 17.125", "the source archive remains independently accessible")
}
