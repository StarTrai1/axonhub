package orchestrator

import (
	"encoding/json"
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
	attempt.SetOrigin(shared.ResponsesOrigin{ChannelID: state.CurrentCandidate.Channel.ID})
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
