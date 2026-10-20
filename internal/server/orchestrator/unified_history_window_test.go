package orchestrator

import (
	"encoding/json"
"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

func TestUnifiedHistoryNativeWindowSurvivesUnavailableAncestor(t *testing.T) {
	ctx, client, state, adapter := unifiedHistoryFixture(t)
	old := &remoteCompactionReference{ID: "cmp_old_native", EncryptedContent: "source-readable-private-root"}
	seedLegacyDeltaCompactionSource(t, ctx, adapter, old, state)
	body := []byte(`{"model":"gpt-6-astra","input":[{"role":"user","content":"keep exact 17.125"},{"type":"compaction","id":"cmp_old_native","encrypted_content":"source-readable-private-root"},{"role":"user","content":"continue safely"},{"type":"compaction_trigger"}]}`)
	state.RawProviderRequest = &httpclient.Request{Body: body, APIFormat: string(llm.APIFormatOpenAIResponseWebSocket)}
	event := &httpclient.StreamEvent{Data: []byte(`{"type":"response.output_item.done","item":{"type":"compaction","id":"cmp_new_native","encrypted_content":"new-valid-native-root"}}`)}
	wrapped, err := retainNativeCompactionSources(&PersistentOutboundTransformer{state: state}, adapter).OnOutboundRawStream(ctx, streams.SliceStream([]*httpclient.StreamEvent{event}))
	require.NoError(t, err)
	require.True(t, wrapped.Next(), "a missing old plaintext ancestor must not discard the new native checkpoint")
	require.Same(t, event, wrapped.Current())
	require.NoError(t, wrapped.Err())
	require.NoError(t, wrapped.Close())
	ref := &remoteCompactionReference{ID: "cmp_new_native", EncryptedContent: "new-valid-native-root"}
	source, err := adapter.loadNativeCompactionSource(ctx, ref, state)
	require.NoError(t, err)
	require.JSONEq(t, string(body), string(source.body))
	// Preserve the opaque dependency; never relabel it as complete plaintext.
	_, complete, err := adapter.expandUnifiedHistory(ctx, source.body, state, make(map[string]bool))
	require.False(t, complete)
	require.True(t, missingHistoryPredecessor(err))

	attempt := &shared.ResponsesAttempt{}
	origin := shared.ResponsesOrigin{ChannelID: state.CurrentCandidate.Channel.ID, Endpoint:"https://example.invalid/v1/responses", Model:"gpt-6-astra", Auth:"synthetic-account-hash", Format:string(llm.APIFormatOpenAIResponseWebSocket)}
 attempt.SetOrigin(origin)
	ctx = shared.WithResponsesAttempt(ctx, attempt)
	envelope, input, err := decodeResponsesInput(body)
	require.NoError(t, err)
	require.NoError(t, setResponseEnvelopeInput(envelope, input[:len(input)-1]))
	accepted, err := json.Marshal(envelope)
	require.NoError(t, err)
	response := []byte(`{"id":"resp_accepted_native","status":"completed","output":[{"role":"assistant","content":"exact completed reply"}]}`)
	require.NoError(t, adapter.archiveHistoryResponse(ctx, accepted, response))
	_, err = client.Request.Delete().Exec(ctx)
	require.NoError(t, err)
	restarted := newRemoteCompactionAdapter(nil, nil, adapter.systemService)
 store := newResponsesSessionStore(func(context.Context, string) ([]byte, []byte, bool, error) {
 t.Fatal("a restart must prefer the accepted native window over expanded raw archives")
 return nil, nil, false, nil
 })
 store.loadWindow = restarted.loadHistoryWindow
 record := store.lookupOrLoad(shared.WithSessionScope(ctx, "native-window-owner"), "resp_accepted_native")
 require.NotNil(t, record)
 require.Equal(t, origin, record.origin)
 require.Contains(t, string(record.input[1]), "source-readable-private-root")
	delta := &remoteCompactionSource{body: []byte(`{"model":"gpt-6-astra","previous_response_id":"resp_accepted_native","input":[{"role":"user","content":"new exact suffix"}]}`)}
	restored, err := restarted.resolveRetainedContinuation(ctx, delta, state, true)
	require.NoError(t, err)
	require.Contains(t, string(restored.body), "source-readable-private-root")
	require.Contains(t, string(restored.body), "exact completed reply")
	require.Contains(t, string(restored.body), "new exact suffix")
	require.False(t, gjson.GetBytes(restored.body, "previous_response_id").Exists())
	foreign := *state
	foreign.CurrentCandidate = nil
	_, err = restarted.resolveRetainedContinuation(ctx, delta, &foreign, true)
	require.Error(t, err, "native windows are channel scoped")
}

func TestUnifiedHistoryNativeCheckpointKeepsExplicitMissingPredecessor(t *testing.T) {
 ctx, _, state, adapter := unifiedHistoryFixture(t)
 ref := &remoteCompactionReference{ID:"cmp_opaque_missing_delta", EncryptedContent:"valid-upstream-checkpoint"}
 source := &remoteCompactionSource{body:[]byte(`{"model":"gpt-6-astra","previous_response_id":"resp_unknown_connection","input":[{"role":"user","content":"exact delta 17.125"},{"type":"compaction_trigger"}]}`)}
 require.NoError(t, adapter.retainNativeCompactionSource(ctx, ref, state, source))
 _, _, found, err := adapter.loadHistorySnapshot(ctx, historyCheckpointAlias(ref))
 require.NoError(t, err)
 require.False(t, found, "a delta with a gap cannot become a complete archival source")
 restarted := newRemoteCompactionAdapter(nil, nil, adapter.systemService)
 _, err = restarted.loadNativeCompactionSource(ctx, ref, state)
 require.True(t, missingHistoryPredecessor(err), "cross-channel plaintext recovery must still report the coverage gap")
 require.NoError(t, adapter.saveHistorySnapshot(ctx, historyResponseAlias("resp_unknown_connection"), []byte(`{"model":"gpt-6-astra","input":[{"role":"user","content":"original parent"}]}`), []byte(`{"id":"resp_unknown_connection","status":"completed","output":[]}`)))
 recovered, err := restarted.loadNativeCompactionSource(ctx, ref, state)
 require.NoError(t, err)
 require.Contains(t, string(recovered.body), "original parent")
 require.Contains(t, string(recovered.body), "exact delta 17.125")
 require.False(t, gjson.GetBytes(recovered.body, "previous_response_id").Exists())
}
