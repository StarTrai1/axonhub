package orchestrator

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/requestexecution"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
)

const nativeSourceFixture = `{"model":"gpt-6-astra","client_metadata":{"thread_id":"source-thread"},"input":[{"type":"message","role":"user","content":"complete original task"},{"type":"function_call","name":"inspect","call_id":"call_original","arguments":"{}"},{"type":"function_call_output","call_id":"call_original","output":"complete original result"},{"type":"compaction_trigger"}]}`

const nativeSourceResponse = `{"object":"response","status":"completed","output":[{"type":"compaction","id":"cmp_durable","encrypted_content":"native-ciphertext"}]}`

func TestResponsesRejectedNativeSourceSurvivesLogsAndRestart(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "http", true: "stream"}[stream], func(t *testing.T) {
			ctx, client, apiKey, adapter := rejectedCompactionStorage(t)
			state := &PersistenceState{APIKey: apiKey, RawProviderRequest: &httpclient.Request{
				Body: []byte(nativeSourceFixture), APIFormat: string(llm.APIFormatOpenAIResponse),
				Headers: http.Header{"Authorization": {"Bearer must-not-store"}},
			}}
			middleware := retainNativeCompactionSources(&PersistentOutboundTransformer{state: state}, adapter)
			if stream {
				event := &httpclient.StreamEvent{Data: []byte(`{"type":"response.output_item.done","item":{"type":"compaction","id":"cmp_durable","encrypted_content":"native-ciphertext"}}`)}
				wrapped, err := middleware.OnOutboundRawStream(ctx, streams.SliceStream([]*httpclient.StreamEvent{event}))
				require.NoError(t, err)
				require.True(t, wrapped.Next())
				require.Same(t, event, wrapped.Current())
				require.False(t, wrapped.Next())
				require.NoError(t, wrapped.Err())
				require.NoError(t, wrapped.Close())
			} else {
				response := &httpclient.Response{StatusCode: 200, Body: []byte(nativeSourceResponse)}
				got, err := middleware.OnOutboundRawResponse(ctx, response)
				require.NoError(t, err)
				require.Same(t, response, got)
			}
			ref := &remoteCompactionReference{ID: "cmp_durable", EncryptedContent: "native-ciphertext"}
			sealed, err := adapter.systemService.LoadNativeCompactionSource(ctx, apiKey.ProjectID, apiKey.ID, remoteCompactionCacheKey(ref))
			require.NoError(t, err)
			require.NotEmpty(t, sealed)
			require.NotContains(t, sealed, "complete original")
			require.NotContains(t, sealed, "must-not-store")
			_, err = client.Request.Delete().Exec(ctx)
			require.NoError(t, err)
			restarted := newRemoteCompactionAdapter(nil, nil, biz.NewSystemService(biz.SystemServiceParams{Ent: client}))
			_, source, err := restarted.findStoredSummaryOrSource(ctx, remoteCompactionCacheKey(ref), ref, "resumed-different-thread", state)
			require.NoError(t, err)
			require.JSONEq(t, nativeSourceFixture, string(source.body))
			require.Empty(t, source.headers.Get("Authorization"))
			for _, other := range []*ent.APIKey{{ID: apiKey.ID + 1, ProjectID: apiKey.ProjectID}, {ID: apiKey.ID, ProjectID: apiKey.ProjectID + 1}} {
				missing, err := restarted.loadNativeCompactionSource(ctx, ref, &PersistenceState{APIKey: other})
				require.NoError(t, err)
				require.Nil(t, missing)
			}
			changed := &remoteCompactionReference{ID: ref.ID, EncryptedContent: "different-ciphertext"}
			require.NoError(t, adapter.systemService.SaveNativeCompactionSource(ctx, apiKey.ProjectID, apiKey.ID, remoteCompactionCacheKey(changed), sealed))
			_, err = restarted.loadNativeCompactionSource(ctx, changed, state)
			require.Error(t, err, "ciphertext hash is authenticated, not only used for lookup")
		})
	}
}

func TestResponsesRejectedNativeSourceStandaloneAndFailure(t *testing.T) {
	ctx, _, apiKey, adapter := rejectedCompactionStorage(t)
	body := strings.Replace(nativeSourceFixture, `,{"type":"compaction_trigger"}`, "", 1)
	state := &PersistenceState{APIKey: apiKey, RawProviderRequest: &httpclient.Request{Body: []byte(body), APIFormat: string(llm.APIFormatOpenAIResponseCompact)}}
	middleware := retainNativeCompactionSources(&PersistentOutboundTransformer{state: state}, adapter)
	_, err := middleware.OnOutboundRawResponse(ctx, &httpclient.Response{StatusCode: 200, Body: []byte(nativeSourceResponse)})
	require.NoError(t, err)
	ref := &remoteCompactionReference{ID: "cmp_durable", EncryptedContent: "native-ciphertext"}
	source, err := adapter.loadNativeCompactionSource(ctx, ref, state)
	require.NoError(t, err)
	require.JSONEq(t, nativeSourceFixture, string(source.body))
	_, err = buildLocalCompactionRequest(source.body)
	require.NoError(t, err)
	state.RawProviderRequest.Body = []byte(nativeSourceFixture)
	state.RawProviderRequest.APIFormat = string(llm.APIFormatOpenAIResponse)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	event := &httpclient.StreamEvent{Data: []byte(`{"type":"response.output_item.done","item":{"type":"compaction","id":"cmp_unpublished","encrypted_content":"native-ciphertext"}}`)}
	wrapped, err := middleware.OnOutboundRawStream(canceled, streams.SliceStream([]*httpclient.StreamEvent{event}))
	require.NoError(t, err)
	require.False(t, wrapped.Next(), "never publish a checkpoint whose source could not be retained")
	require.Error(t, wrapped.Err())
}

func TestResponsesRejectedNativeSummarySurvivesSourceRemoval(t *testing.T) {
	ctx, client, apiKey, adapter := rejectedCompactionStorage(t)
	ref := &remoteCompactionReference{ID: "cmp_native_summary", EncryptedContent: "native-summary-ciphertext"}
	state := &PersistenceState{APIKey: apiKey}
	key := remoteCompactionCacheKey(ref)
	require.NoError(t, adapter.retainCompactionSummary(ctx, key, ref, state, "verified bridge summary"))
	restarted := newRemoteCompactionAdapter(nil, nil, biz.NewSystemService(biz.SystemServiceParams{Ent: client}))
	summary, err := restarted.summaryForCompaction(ctx, key, ref, "resumed-thread", "gpt-6-astra", state, nil)
	require.NoError(t, err)
	require.Equal(t, "verified bridge summary", summary)
}

func TestResponsesRejectedNativeSourceDoesNotArchiveOrdinaryContinuation(t *testing.T) {
	ctx, _, apiKey, adapter := rejectedCompactionStorage(t)
	state := &PersistenceState{APIKey: apiKey, RawProviderRequest: &httpclient.Request{
		APIFormat: string(llm.APIFormatOpenAIResponse), Body: []byte(`{"model":"gpt-6-astra","input":[{"type":"compaction","id":"cmp_prior","encrypted_content":"prior-opaque"},{"role":"user","content":"continue"}]}`),
	}}
	middleware := retainNativeCompactionSources(&PersistentOutboundTransformer{state: state}, adapter)
	_, err := middleware.OnOutboundRawResponse(ctx, &httpclient.Response{StatusCode: 200, Body: []byte(nativeSourceResponse)})
	require.NoError(t, err)
	ref := &remoteCompactionReference{ID: "cmp_durable", EncryptedContent: "native-ciphertext"}
	source, err := adapter.loadNativeCompactionSource(ctx, ref, state)
	require.NoError(t, err)
	require.Nil(t, source)
}

func TestResponsesRejectedNativeSourceFailedResponse(t *testing.T) {
	ctx, _, apiKey, adapter := rejectedCompactionStorage(t)
	state := &PersistenceState{APIKey: apiKey, RawProviderRequest: &httpclient.Request{
		APIFormat: string(llm.APIFormatOpenAIResponse), Body: []byte(nativeSourceFixture),
	}}
	middleware := retainNativeCompactionSources(&PersistentOutboundTransformer{state: state}, adapter)
	for _, status := range []string{"failed", "incomplete", "in_progress"} {
		_, err := middleware.OnOutboundRawResponse(ctx, &httpclient.Response{StatusCode: 200, Body: []byte(strings.Replace(nativeSourceResponse, "completed", status, 1))})
		require.NoError(t, err)
	}
	ref := &remoteCompactionReference{ID: "cmp_durable", EncryptedContent: "native-ciphertext"}
	source, err := adapter.loadNativeCompactionSource(ctx, ref, state)
	require.NoError(t, err)
	require.Nil(t, source)
}

func TestResponsesRejectedNativeSourceBeforePassThrough(t *testing.T) {
	ctx, _, apiKey, adapter := rejectedCompactionStorage(t)
	channel := &biz.Channel{Channel: &ent.Channel{ID: 1, Name: "native", Settings: &objects.ChannelSettings{PassThroughBody: lo.ToPtr(true)}}}
	state := &PersistenceState{
		APIKey:             apiKey,
		CurrentCandidate:   &ChannelModelsCandidate{Channel: channel},
		LlmRequest:         &llm.Request{APIFormat: llm.APIFormatOpenAIResponse, RawRequest: &httpclient.Request{APIFormat: string(llm.APIFormatOpenAIResponse)}},
		RawProviderRequest: &httpclient.Request{APIFormat: string(llm.APIFormatOpenAIResponse), Body: []byte(nativeSourceFixture)},
	}
	outbound := &PersistentOutboundTransformer{state: state, wrapped: &mockTransformer{apiFormat: llm.APIFormatOpenAIResponse}}
	ref := &remoteCompactionReference{ID: "cmp_durable", EncryptedContent: "native-ciphertext"}
	event := &httpclient.StreamEvent{Data: json.RawMessage(`{"type":"response.completed","response":{"status":"completed","output":[{"type":"compaction","id":"cmp_durable","encrypted_content":"native-ciphertext"}]}}`)}
	archived, err := retainNativeCompactionSources(outbound, adapter).OnOutboundRawStream(ctx, streams.SliceStream([]*httpclient.StreamEvent{event}))
	require.NoError(t, err)
	fanned, err := captureRawProviderStream(outbound, nil).OnOutboundRawStream(ctx, archived)
	require.NoError(t, err)
	require.NotNil(t, state.RawStreamCh)
	t.Cleanup(func() { _ = fanned.Close() })
	for got := range state.RawStreamCh {
		require.Same(t, event, got)
		source, loadErr := adapter.loadNativeCompactionSource(ctx, ref, state)
		require.NoError(t, loadErr)
		require.NotNil(t, source, "source must be durable as soon as the client can receive the checkpoint")
	}
	require.True(t, fanned.Next())
	require.False(t, fanned.Next())
	require.NoError(t, fanned.Err())
}

func TestResponsesRejectedNativeSourceBackfillsOldLogs(t *testing.T) {
	ctx, client, apiKey, adapter := rejectedCompactionStorage(t)
	seedRejectedCompactionSource(t, ctx, client, apiKey, requestexecution.StatusCompleted, "native-checkpoint-ciphertext")
	ref := &remoteCompactionReference{ID: "cmp_native", EncryptedContent: "native-checkpoint-ciphertext"}
	state := &PersistenceState{APIKey: apiKey}
	_, source, err := adapter.findStoredSummaryOrSource(ctx, remoteCompactionCacheKey(ref), ref, "recovery-thread", state)
	require.NoError(t, err)
	require.NotNil(t, source)
	_, err = client.Request.Delete().Exec(ctx)
	require.NoError(t, err)
	restarted := newRemoteCompactionAdapter(nil, nil, biz.NewSystemService(biz.SystemServiceParams{Ent: client}))
	_, restored, err := restarted.findStoredSummaryOrSource(ctx, remoteCompactionCacheKey(ref), ref, "resumed-thread", state)
	require.NoError(t, err)
	require.NotNil(t, restored)
	require.JSONEq(t, string(source.body), string(restored.body))
}
