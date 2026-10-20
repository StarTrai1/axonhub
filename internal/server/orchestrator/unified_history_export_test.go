package orchestrator

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

type unifiedExportExecutor struct {
	unifiedConcurrentExecutor

	requests []*httpclient.Request
}

func (e *unifiedExportExecutor) DoStream(ctx context.Context, request *httpclient.Request) (streams.Stream[*httpclient.StreamEvent], error) {
	e.requests = append(e.requests, request)
	return e.unifiedConcurrentExecutor.DoStream(ctx, request)
}

func TestUnifiedHistoryCheckpointExportUsesProvenReaderOnce(t *testing.T) {
	ctx, _, state, adapter := unifiedHistoryFixture(t)
	body := []byte(`{"model":"gpt-6-astra","input":[{"role":"user","content":"keep original exact 17.125"},{"type":"compaction","id":"cmp_export","encrypted_content":"reader-private-state"},{"role":"user","content":"new correction 17.250"}]}`)
	executor := &unifiedExportExecutor{}
	_, ok, err := adapter.compileUnifiedContext(ctx, body, state, executor)
	require.NoError(t, err)
	require.False(t, ok)
	require.Empty(t, executor.requests, "unknown readers must not be probed with user history")
	// A synthetic completed request supplies the exact finalized provider identity.
	source := &remoteCompactionSource{body: []byte(`{"model":"gpt-6-astra","input":[{"type":"compaction","id":"cmp_export","encrypted_content":"reader-private-state"},{"type":"compaction_trigger"}]}`), headers: make(http.Header)}
	_, err = adapter.generateLocalSummary(ctx, "synthetic-provenance", source, state, executor)
	require.NoError(t, err)
	request := executor.requests[0]
	origin := nativeHistoryReader{Provider: historyProviderIdentity{ChannelID: state.CurrentCandidate.Channel.ID, Endpoint: request.URL, Auth: historyReaderCredential(request, false), Model: "gpt-6-astra", Format: request.APIFormat}}
	require.NoError(t, adapter.rememberNativeHistoryOrigins(ctx, body, []byte(`{"output":[]}`), origin))
	executor.requests = nil
	compiled, ok, err := adapter.compileUnifiedContext(ctx, body, state, executor)
	require.NoError(t, err)
	require.True(t, ok)
	require.Len(t, executor.requests, 1)
	require.NotContains(t, string(executor.requests[0].Body), "new correction 17.250", "export must exclude the live suffix")
	require.Contains(t, string(executor.requests[0].Body), "reader-private-state")
	require.Contains(t, string(compiled), "original exact 17.125")
	require.Contains(t, string(compiled), "new correction 17.250")
	require.False(t, gjson.GetBytes(compiled, `input.#(type=="compaction")`).Exists())
	restarted := newRemoteCompactionAdapter(nil, nil, adapter.systemService)
	again, ok, err := restarted.compileUnifiedContext(ctx, body, state, executor)
	require.NoError(t, err)
	require.True(t, ok)
	require.JSONEq(t, string(compiled), string(again))
	require.Len(t, executor.requests, 1, "restart cannot export the same checkpoint again")
}

func TestUnifiedHistoryCheckpointExportRejectsChangedReader(t *testing.T) {
	ctx, _, state, adapter := unifiedHistoryFixture(t)
	body := []byte(`{"model":"gpt-6-astra","input":[{"type":"compaction","id":"cmp_export_wrong","encrypted_content":"private-state"},{"role":"user","content":"continue"}]}`)
	origin := nativeHistoryReader{Provider: historyProviderIdentity{ChannelID: state.CurrentCandidate.Channel.ID, Endpoint: "https://foreign.invalid/responses", Auth: "changed-account", Model: "gpt-6-astra"}}
	require.NoError(t, adapter.rememberNativeHistoryOrigins(ctx, body, []byte(`{"output":[]}`), origin))
	executor := &unifiedExportExecutor{}
	_, ok, err := adapter.compileUnifiedContext(ctx, body, state, executor)
	require.False(t, ok)
	require.ErrorContains(t, err, "no upstream request was sent")
	require.Empty(t, executor.requests)
	state.CurrentCandidate = nil
	state.ChannelModelsCandidates = nil
	_, ok, err = adapter.exportUnifiedCheckpoint(ctx, body, state, executor)
	require.NoError(t, err)
	require.False(t, ok, "provenance does not authorize a removed candidate")
}

func TestExpensiveGatewayTimeoutStopsSameChannelReplay(t *testing.T) {
	now := time.Now()
	_, _, state, _ := unifiedHistoryFixture(t)
	outbound := &PersistentOutboundTransformer{state: state}
	state.Perf = &biz.PerformanceRecord{StartTime: now.Add(-31 * time.Second)}
	err := &httpclient.Error{StatusCode: http.StatusGatewayTimeout}
	require.False(t, outbound.CanRetry(err))
	require.False(t, canRetryLocalCompactionStream(outbound, err))
	state.Perf.StartTime = now
	require.True(t, outbound.CanRetry(err), "fast transient gateway failures retain the existing bounded retry policy")
	require.True(t, canRetryLocalCompactionStream(outbound, err))
	require.True(t, outbound.CanRetry(&httpclient.Error{StatusCode: http.StatusBadGateway}))
}

func TestUnifiedHistoryReaderIdentityExcludesRequestHeadersNotAccounts(t *testing.T) {
	_, _, state, _ := unifiedHistoryFixture(t)
	request := &httpclient.Request{URL: "https://example.invalid/responses", Body: []byte(`{"model":"gpt-6-astra"}`), Headers: http.Header{"Authorization": {"Bearer first"}, "Chatgpt-Account-Id": {"account-a"}, "X-Request-ID": {"old"}}}
	reader := nativeHistoryReader{Provider: historyProviderIdentity{ChannelID: state.CurrentCandidate.Channel.ID, Endpoint: request.URL, Model: "gpt-6-astra", Auth: historyReaderCredential(request, false)}}
	request.Headers.Set("X-Request-ID", "new")
	require.True(t, historyExportMatches(reader, state.CurrentCandidate, request))
	request.Headers.Set("Authorization", "Bearer second")
	require.False(t, historyExportMatches(reader, state.CurrentCandidate, request))
	state.CurrentCandidate.Channel.Credentials = objects.ChannelCredentials{OAuth: &objects.OAuthCredentials{AccessToken: "second"}}
	reader.OAuth = true
	reader.Provider.Auth = historyReaderCredential(request, true)
	request.Headers.Set("Authorization", "Bearer refreshed-same-account")
	require.True(t, historyExportMatches(reader, state.CurrentCandidate, request))
	request.Headers.Set("Chatgpt-Account-Id", "account-b")
	require.False(t, historyExportMatches(reader, state.CurrentCandidate, request))
}

func TestUnifiedHistoryNativeReadingIndependentOfGenerationPolicy(t *testing.T) {
	ctx, _, state, adapter := unifiedHistoryFixture(t)
	ctx = shared.WithSessionScope(ctx, "native-reader-owner")
	state.CurrentCandidate.Channel.Policies.RemoteCompaction = objects.RemoteCompactionPolicyLocalBridge
	body := []byte(`{"model":"gpt-6-astra","client_metadata":{"thread_id":"reader-thread"},"input":[{"type":"compaction","id":"cmp_readable","encrypted_content":"same-account-state"},{"role":"user","content":"continue exact 17.125"}]}`)
	state.RawRequest = &httpclient.Request{Body: body, APIFormat: string(llm.APIFormatOpenAIResponse)}
	state.RawProviderRequest = &httpclient.Request{Body: body, URL: "https://example.invalid/responses", APIFormat: string(llm.APIFormatOpenAIResponse), Headers: http.Header{"Authorization": {"Bearer same-account"}}}
	reader := nativeHistoryReaderFor(state)
	require.NoError(t, adapter.rememberNativeHistoryOrigins(ctx, body, []byte(`{"output":[]}`), reader))
	executor := &unifiedExportExecutor{}
	req, err := responses.NewInboundTransformer().TransformRequest(ctx, state.RawRequest)
	require.NoError(t, err)
	req.RawRequest = state.RawRequest
	inbound := &remoteCompactionMiddleware{inbound: &PersistentInboundTransformer{state: state}, adapter: adapter, executor: executor}
	passed, err := inbound.OnInboundLlmRequest(ctx, req)
	require.NoError(t, err)
	require.Same(t, req, passed)
	m := &responsesCompactionRecoveryMiddleware{adapter: adapter, executor: executor, outbound: &PersistentOutboundTransformer{state: state}}
	key, _, ok := m.recoveryKey(ctx, state.RawProviderRequest)
	require.True(t, ok, "exercise the actual recovery branch")
	outgoing, err := m.OnOutboundRawRequest(ctx, state.RawProviderRequest)
	require.NoError(t, err)
	require.Equal(t, body, outgoing.Body)
	require.Empty(t, executor.requests, "changing compression generation policy must not re-summarize readable state")
	m.rejected = map[responsesCompactionRecoveryKey]struct{}{key: {}}
	_, err = m.OnOutboundRawRequest(ctx, state.RawProviderRequest)
	require.Error(t, err, "explicit subsequent rejection overrides historical read evidence")
	require.Empty(t, executor.requests, "a rejected current checkpoint must not be exported back to the same reader")
}
