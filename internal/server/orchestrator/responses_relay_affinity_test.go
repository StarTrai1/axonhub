package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

func relayAffinityFailure(status int, message, route string) *httpclient.Error {
	body, _ := json.Marshal(map[string]any{"error": map[string]string{"type": "server_error", "message": message}})
	return &httpclient.Error{StatusCode: status, Body: body, Headers: http.Header{"X-New-Api-Routed-Channel-Id": {route}}}
}

func relayAffinityOverload() *httpclient.Error {
	return relayAffinityFailure(http.StatusInternalServerError,
		"当前模型 gpt-6-astra 负载已经达到上限，请稍后重试 (request id: synthetic-overload)", "(11,11),(22,22)")
}

func relayAffinityRequest(t *testing.T) *httpclient.Request {
	t.Helper()
	request := rejectedReasoningPipelineRequest(t, llm.APIFormatOpenAIResponse)
	request.Body = []byte(responsesResourceHistoryFixture)
	thread := request.Headers.Get("Thread-Id")
	metadata := map[string]string{
		"thread_id": thread, "session_id": "shared-session", "turn_id": "original-turn", "root_turn_id": "original-root",
		"x-codex-window-id": thread + ":4", "x-codex-installation-id": "original-installation",
	}
	turn, err := json.Marshal(map[string]any{"thread_id": thread, "session_id": "shared-session", "window_id": thread + ":4", "turn_id": "original-turn", "root_turn_id": "original-root", "window_number": 4})
	require.NoError(t, err)
	metadata["x-codex-turn-metadata"] = string(turn)
	request.Body, err = sjson.SetBytes(request.Body, "client_metadata", metadata)
	require.NoError(t, err)
	request.Headers.Set("X-Codex-Turn-Metadata", string(turn))
	request.Headers.Set("X-Codex-Window-Id", thread+":4")
	request.Headers.Set("Conversation_id", thread)
	request.Headers.Set("X-Codex-Turn-State", "opaque-old-turn")
	return request
}

func relayAffinityMiddleware(t *testing.T, target **responsesRelayAffinityMiddleware) func(*PersistenceState, *PersistentOutboundTransformer) pipeline.Middleware {
	t.Helper()
	return func(state *PersistenceState, outbound *PersistentOutboundTransformer) pipeline.Middleware {
		state.APIKey = &ent.APIKey{ID: 1, ProjectID: 1}
		middleware := recoverResponsesRelayAffinity(outbound).(*responsesRelayAffinityMiddleware)
		if target != nil {
			*target = middleware
		}
		t.Cleanup(func() {
			for key := range middleware.pending {
				responsesRelayAffinities.Remove(key)
			}
		})
		return middleware
	}
}

func TestResponsesRejectedRelayAffinityPreservesHistory(t *testing.T) {
	for _, raw := range []bool{false, true} {
		t.Run(fmt.Sprintf("raw=%t", raw), func(t *testing.T) {
			request := relayAffinityRequest(t)
			original := append([]byte(nil), request.Body...)
			originalHeaders := request.Headers.Clone()
			executor := &responsesReasoningPipelineExecutor{failures: []error{relayAffinityOverload()}, events: rejectedReasoningCompactionEvents()}
			var middleware *responsesRelayAffinityMiddleware
			state, result, err := runRejectedReasoningPipeline(t, t.Context(), request, executor, t.Name(), raw, 2,
				func(*PersistenceState, *PersistentOutboundTransformer) pipeline.Middleware {
					return &relayReplayTestDestination{}
				},
				relayAffinityMiddleware(t, &middleware),
			)
			require.NoError(t, err)
			drainRejectedReasoningPipeline(t, result)
			require.Len(t, executor.requests, 2)
			before, after := executor.requests[0], executor.requests[1]
			require.Equal(t, gjson.GetBytes(before.Body, "input").Raw, gjson.GetBytes(after.Body, "input").Raw, "all IDs, ciphertext, messages, tool results and controls remain byte-identical")
			for _, path := range []string{"model", "store", "reasoning", "include", "client_metadata.turn_id", "client_metadata.root_turn_id", "client_metadata.x-codex-installation-id"} {
				require.Equal(t, gjson.GetBytes(before.Body, path).Raw, gjson.GetBytes(after.Body, path).Raw, path)
			}
			affinity := after.Headers.Get("Thread-Id")
			require.NotEmpty(t, affinity)
			require.NotEqual(t, before.Headers.Get("Thread-Id"), affinity)
			require.Equal(t, affinity, after.Headers.Get("Session-Id"))
			require.Equal(t, affinity, after.Headers.Get("Conversation_id"))
			require.Equal(t, affinity, gjson.GetBytes(after.Body, "prompt_cache_key").String())
			require.Equal(t, affinity, gjson.GetBytes(after.Body, "client_metadata.thread_id").String())
			require.Equal(t, affinity+":4", after.Headers.Get("X-Codex-Window-Id"))
			require.Equal(t, "original-root", gjson.Get(after.Headers.Get("X-Codex-Turn-Metadata"), "root_turn_id").String())
			require.Equal(t, before.URL, after.URL)
			require.Equal(t, before.Headers.Get("Authorization"), after.Headers.Get("Authorization"))
			require.Empty(t, after.Headers.Get(codexTurnStateHeader))
			require.Equal(t, original, request.Body)
			require.Equal(t, originalHeaders, request.Headers)
			require.True(t, state.responsesRelayAffinityApplied)
			for key := range middleware.pending {
				require.Eventually(t, func() bool {
					cached, found := responsesRelayAffinities.Get(key)
					return found && cached.id == affinity
				}, time.Second, time.Millisecond)
			}

			// A later request retains the downstream thread and immediately uses
			// only the successfully recovered upstream alias.
			next := &responsesReasoningPipelineExecutor{events: rejectedReasoningCompactionEvents()}
			_, result, err = runRejectedReasoningPipeline(t, t.Context(), request, next, t.Name(), raw, 2,
				func(*PersistenceState, *PersistentOutboundTransformer) pipeline.Middleware {
					return &relayReplayTestDestination{}
				},
				relayAffinityMiddleware(t, nil),
			)
			require.NoError(t, err)
			drainRejectedReasoningPipeline(t, result)
			require.Len(t, next.requests, 1)
			require.Equal(t, affinity, next.requests[0].Headers.Get("Thread-Id"))

			// A confirmed alias can later become overloaded without sticky-pair
			// diagnostics. It is still a known migration, scoped to this owner.
			later := &responsesReasoningPipelineExecutor{
				failures: []error{relayAffinityFailure(http.StatusInternalServerError,
					"We're currently experiencing high demand, which may cause temporary errors", "33,22")},
				events:   rejectedReasoningCompactionEvents(),
			}
			_, result, err = runRejectedReasoningPipeline(t, t.Context(), request, later, t.Name(), raw, 1,
				func(*PersistenceState, *PersistentOutboundTransformer) pipeline.Middleware {
					return &relayReplayTestDestination{}
				},
				relayAffinityMiddleware(t, nil),
			)
			require.NoError(t, err)
			drainRejectedReasoningPipeline(t, result)
			require.Len(t, later.requests, 2)
			require.Equal(t, affinity, later.requests[0].Headers.Get("Thread-Id"))
			require.NotEqual(t, affinity, later.requests[1].Headers.Get("Thread-Id"))
		})
	}
}

func TestResponsesRejectedRelayAffinitySharesBudget(t *testing.T) {
	for _, retries := range []int{0, 1, 5} {
		t.Run(fmt.Sprint(retries), func(t *testing.T) {
			executor := &responsesReasoningPipelineExecutor{failures: []error{relayAffinityOverload(), relayAffinityOverload(), relayAffinityOverload(), relayAffinityOverload()}}
			var middleware *responsesRelayAffinityMiddleware
			_, _, err := runRejectedReasoningPipeline(t, t.Context(), relayAffinityRequest(t), executor, t.Name(), true, retries,
				func(*PersistenceState, *PersistentOutboundTransformer) pipeline.Middleware {
					return &relayReplayTestDestination{}
				},
				relayAffinityMiddleware(t, &middleware),
			)
			require.Error(t, err)
			want := responsesRelayAffinityMaxMigrations + 1
			if retries < responsesRelayAffinityMaxMigrations {
				want = retries + 1
			}
			require.Len(t, executor.requests, want)
			for key := range middleware.pending {
				_, cached := responsesRelayAffinities.Get(key)
				require.False(t, cached, "an unsuccessful retry cannot establish a recovered affinity")
			}
		})
	}
}

func TestResponsesRejectedRelayAffinityBoundsPlainRouteOverload(t *testing.T) {
	for _, raw := range []bool{false, true} {
		t.Run(fmt.Sprintf("raw=%t", raw), func(t *testing.T) {
			const demand = "We’re currently experiencing high demand, which may cause temporary errors"
			first := relayAffinityFailure(http.StatusInternalServerError, demand, "(70605,70605),(411041,411041)")
			next := relayAffinityFailure(http.StatusInternalServerError, demand, "108877,411041")
			executor := &responsesReasoningPipelineExecutor{failures: []error{first, next, next, next}, events: rejectedReasoningCompactionEvents()}
			var middleware *responsesRelayAffinityMiddleware
			_, _, err := runRejectedReasoningPipeline(t, t.Context(), relayAffinityRequest(t), executor, t.Name(), raw, 5,
				func(*PersistenceState, *PersistentOutboundTransformer) pipeline.Middleware {
					return &relayReplayTestDestination{}
				},
				relayAffinityMiddleware(t, &middleware),
			)
			require.Error(t, err)
			require.Len(t, executor.requests, responsesRelayAffinityMaxMigrations+1)
			seen := make(map[string]bool)
			for _, request := range executor.requests {
				identity := request.Headers.Get("Thread-Id")
				require.False(t, seen[identity], "a failed migration must not repeat merely because the relay changed its route diagnostic format")
				seen[identity] = true
				require.Equal(t, gjson.GetBytes(executor.requests[0].Body, "input").Raw, gjson.GetBytes(request.Body, "input").Raw)
			}
			for key := range middleware.pending {
				_, cached := responsesRelayAffinities.Get(key)
				require.False(t, cached)
			}
		})
	}
}

func TestResponsesRejectedRelayAffinityOverloadGuards(t *testing.T) {
	const demand = "We’re currently experiencing high demand, which may cause temporary errors"
	for _, tc := range []struct {
		status  int
		message string
		route   string
		want    bool
	}{
		{500, demand, "(11,11),(22,22)", true},
		{503, demand + ".", "(22,22)", true},
		{429, demand, "(22,22)", false},
		{400, demand, "(22,22)", false},
		{500, "insufficient_quota", "(22,22)", false},
		{500, demand, "", false},
		{500, demand, "11,22", false},
		{500, demand, "(11,22)", false},
		{500, demand, "prefix (22,22)", false},
		{500, demand + " Also invalid model.", "(22,22)", false},
		{500, "当前模型 another-model 负载已经达到上限，请稍后重试", "(22,22)", false},
	} {
		require.Equal(t, tc.want, responsesRelayStickyOverload(relayAffinityFailure(tc.status, tc.message, tc.route), "gpt-6-astra"), "%+v", tc)
	}
	require.False(t, responsesRelayStickyOverload(errors.New(demand), "gpt-6-astra"))
	quota := relayAffinityOverload()
	var err error
	quota.Body, err = sjson.SetBytes(quota.Body, "error.code", "usage_limit_reached")
	require.NoError(t, err)
	require.False(t, responsesRelayStickyOverload(quota, "gpt-6-astra"))
	cooldown := relayAffinityOverload()
	cooldown.Headers.Set("Retry-After", "120")
	require.False(t, responsesRelayStickyOverload(cooldown, "gpt-6-astra"))
}

func TestResponsesRejectedRelayAffinityHistoryGuards(t *testing.T) {
	body := []byte(responsesResourceHistoryFixture)
	require.True(t, responsesRelayAffinityHistory(body))
	for _, change := range []struct {
		path  string
		value any
	}{
		{"previous_response_id", "resp_external"},
		{"conversation", map[string]string{"id": "conv_external"}},
		{"input.8", map[string]string{"type": "compaction", "id": "cmp_native", "encrypted_content": "opaque-checkpoint"}},
		{"input.8", map[string]string{"type": "item_reference", "id": "msg_external"}},
		{"input.5.call_id", "orphan"},
		{"input.5.output", []map[string]string{{"type": "encrypted_content", "encrypted_content": "opaque-result"}}},
		{"input", []map[string]string{{"role": "user", "content": "new conversation"}}},
	} {
		changed, err := sjson.SetBytes(body, change.path, change.value)
		require.NoError(t, err)
		require.False(t, responsesRelayAffinityHistory(changed), change.path)
	}
	withAgent, err := sjson.SetRawBytes(body, "input.8", []byte(preservedEncryptedAgentMessage))
	require.NoError(t, err)
	require.True(t, responsesRelayAffinityHistory(withAgent), "an affinity change does not rewrite the opaque agent message")
}

func TestResponsesRejectedRelayAffinityKeepsClientSessionWindow(t *testing.T) {
	request := relayAffinityRequest(t)
	upstream, err := applyResponsesRelayAffinity(request, "recovered-upstream")
	require.NoError(t, err)
	state := &PersistenceState{RawRequest: request, RawProviderRequest: upstream, responsesRelayAffinityApplied: true}
	store := newResponsesSessionStore()
	ctx := shared.WithSessionScope(t.Context(), "affinity-client-window")
	ctx = shared.WithSessionID(ctx, "original-client")
	store.record(ctx, responsesSessionProviderBody(state), []byte(`{"id":"resp_recovered","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"complete answer"}]}]}`))
	next, err := sjson.SetBytes(request.Body, "previous_response_id", "resp_recovered")
	require.NoError(t, err)
	next, err = sjson.SetRawBytes(next, "input", []byte(`[{"type":"message","role":"user","content":"continue"}]`))
	require.NoError(t, err)
	prepared, session := store.prepare(ctx, next)
	require.Equal(t, "original-client", session)
	require.False(t, gjson.GetBytes(prepared, "previous_response_id").Exists())
	require.Equal(t, int64(11), gjson.GetBytes(prepared, "input.#").Int(), "same-window continuation must retain prior input and output")
	require.Contains(t, string(prepared), "complete answer")
	require.Equal(t, gjson.GetBytes(request.Body, "client_metadata").Raw, gjson.GetBytes(responsesSessionProviderBody(state), "client_metadata").Raw)
}

func TestResponsesRejectedRelayAffinityHTTP(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	request := relayAffinityRequest(t)
	originalThread := request.Headers.Get("Thread-Id")
	var attempts atomic.Int32
	var firstSelection atomic.Value
	var recoveredSelection atomic.Value
	var replayedInput []map[string]any
	require.NoError(t, json.Unmarshal([]byte(gjson.GetBytes(request.Body, "input").Raw), &replayedInput))
	for _, item := range replayedInput {
		delete(item, "id")
	}
	writeFailure := func(w http.ResponseWriter, failure *httpclient.Error) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-New-Api-Routed-Channel-Id", failure.Headers.Get("X-New-Api-Routed-Channel-Id"))
		w.WriteHeader(failure.StatusCode)
		_, _ = w.Write(failure.Body)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := attempts.Add(1)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		if r.Header.Get("Thread-Id") == originalThread {
			writeFailure(w, relayAffinityOverload())
			return
		}
		if r.Header.Get("Thread-Id") == "" || gjson.GetBytes(body, "prompt_cache_key").String() != r.Header.Get("Thread-Id") {
			t.Error("inconsistent recovered upstream affinity")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if attempt < 4 {
			if gjson.GetBytes(body, "input").Raw != gjson.GetBytes(request.Body, "input").Raw {
				t.Error("history changed before an explicit replay rejection")
			}
			if attempt == 2 {
				firstSelection.Store(r.Header.Get("Thread-Id"))
				writeFailure(w, relayAffinityFailure(http.StatusInternalServerError,
					"We're currently experiencing high demand, which may cause temporary errors", "33,22"))
				return
			}
			if firstSelection.Load() == r.Header.Get("Thread-Id") {
				t.Error("repeated the failed relay selection")
			}
			recoveredSelection.Store(r.Header.Get("Thread-Id"))
			writeFailure(w, relayAffinityFailure(http.StatusBadRequest, "bad response status code 400 (request id: synthetic-replay)", "44,55"))
			return
		}
		var actualInput []map[string]any
		if err := json.Unmarshal([]byte(gjson.GetBytes(body, "input").Raw), &actualInput); err != nil || !reflect.DeepEqual(replayedInput, actualInput) {
			t.Error("replay must only detach optional item IDs and retain every ciphertext, message, tool result and call_id")
		}
		if recoveredSelection.Load() != r.Header.Get("Thread-Id") {
			t.Error("history replay changed the recovered upstream session")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, event := range rejectedReasoningCompactionEvents() {
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, event.Data)
		}
	}))
	t.Cleanup(server.Close)
	executor := httpclient.NewHttpClientWithProxy(&httpclient.ProxyConfig{Type: httpclient.ProxyTypeDisabled})
	_, result, err := runRejectedReasoningPipeline(t, ctx, request, executor, "synthetic-http-affinity", true, 4,
		func(*PersistenceState, *PersistentOutboundTransformer) pipeline.Middleware {
			return pipeline.OnRawRequest("test-relay-destination", func(_ context.Context, request *httpclient.Request) (*httpclient.Request, error) {
				request.URL = server.URL + "/v1/responses"
				return request, nil
			})
		},
		relayAffinityMiddleware(t, nil),
	)
	require.NoError(t, err)
	require.Contains(t, drainRejectedReasoningPipeline(t, result), "response.completed")
	require.Equal(t, int32(4), attempts.Load(), "sticky overload, fresh selection overload, explicit 400, then lossless replay")
}

func TestResponsesRejectedRelayAffinityScopeIsolation(t *testing.T) {
	request := relayAffinityRequest(t)
	executor := &responsesReasoningPipelineExecutor{failures: []error{relayAffinityOverload()}, events: rejectedReasoningCompactionEvents()}
	var middleware *responsesRelayAffinityMiddleware
	state, result, err := runRejectedReasoningPipeline(t, t.Context(), request, executor, "scope-credential", false, 1,
		func(*PersistenceState, *PersistentOutboundTransformer) pipeline.Middleware {
			return &relayReplayTestDestination{}
		},
		relayAffinityMiddleware(t, &middleware),
	)
	require.NoError(t, err)
	drainRejectedReasoningPipeline(t, result)
	baseline := executor.requests[0]
	key, ok := middleware.scope(baseline)
	require.True(t, ok)
	for _, change := range []struct{ name, value string }{
		{"url", "https://another-relay.invalid/v1/responses"},
		{"credential", "Bearer another-credential"},
		{"model", "another-model"},
	} {
		changed := *baseline
		changed.Headers = baseline.Headers.Clone()
		switch change.name {
		case "url":
			changed.URL = change.value
		case "credential":
			changed.Auth = nil
			changed.Headers.Set("Authorization", change.value)
		case "model":
			changed.Body, err = sjson.SetBytes(changed.Body, "model", change.value)
			require.NoError(t, err)
		}
		other, valid := middleware.scope(&changed)
		require.True(t, valid)
		require.NotEqual(t, key, other)
		_, cached := responsesRelayAffinities.Get(other)
		require.False(t, cached)
	}
	for _, destination := range []string{"https://chatgpt.com/backend-api/codex/responses", "https://api.openai.com/v1/responses", "https://sub.chatgpt.com/responses"} {
		changed := *baseline
		changed.URL = destination
		_, valid := middleware.scope(&changed)
		require.False(t, valid, destination)
	}
	state.APIKey = &ent.APIKey{ID: 2, ProjectID: 1}
	other, valid := middleware.scope(baseline)
	require.True(t, valid)
	require.NotEqual(t, key, other)
	state.APIKey = &ent.APIKey{ID: 1, ProjectID: 2}
	other, valid = middleware.scope(baseline)
	require.True(t, valid)
	require.NotEqual(t, key, other)
	state.APIKey = nil
	_, valid = middleware.scope(baseline)
	require.False(t, valid)
}

func TestResponsesRejectedRelayAffinityPreservesUnknownMetadata(t *testing.T) {
	request := relayAffinityRequest(t)
	const turn = `{"thread_id":"old","window_id":"old:4","extension":{"large_id":9007199254740993},"turn_id":"keep-turn"}`
	request.Headers.Set("X-Codex-Turn-Metadata", turn)
	var err error
	request.Body, err = sjson.SetBytes(request.Body, "client_metadata.x-codex-turn-metadata", turn)
	require.NoError(t, err)
	updated, err := applyResponsesRelayAffinity(request, "new-affinity")
	require.NoError(t, err)
	for _, metadata := range []string{updated.Headers.Get("X-Codex-Turn-Metadata"), gjson.GetBytes(updated.Body, "client_metadata.x-codex-turn-metadata").String()} {
		require.Equal(t, "9007199254740993", gjson.Get(metadata, "extension.large_id").Raw)
		require.Equal(t, "keep-turn", gjson.Get(metadata, "turn_id").String())
	}
	request.Headers.Set("X-Codex-Turn-Metadata", "null")
	_, err = applyResponsesRelayAffinity(request, "new-affinity")
	require.Error(t, err, "null metadata must not panic or silently retain the old affinity")
}

func TestResponsesRejectedRelayAffinityLocalCompaction(t *testing.T) {
	for _, exhausted := range []bool{false, true} {
		t.Run(fmt.Sprint(exhausted), func(t *testing.T) {
			ctx := shared.WithSessionScope(t.Context(), t.Name())
			outbound := newCodexResponsesPassThroughOutbound()
			outbound.wrapped = new(responses.OutboundTransformer)
			outbound.state.CurrentCandidate.Channel.ID = 95071
			outbound.state.APIKey = &ent.APIKey{ID: 41, ProjectID: 42}
			outbound.state.RetryPolicyProvider = &mockRetryPolicyProvider{policy: &biz.RetryPolicy{Enabled: true, MaxSingleChannelRetries: 4}}
			request := relayAffinityRequest(t)
			request.URL = "https://relay.invalid/v1/responses"
			request.Headers.Set("Authorization", "Bearer synthetic-bridge-credential")
			outbound.state.RawRequest = request
			middleware := recoverResponsesRelayAffinity(outbound).(*responsesRelayAffinityMiddleware)
			t.Cleanup(func() {
				for key := range middleware.pending {
					responsesRelayAffinities.Remove(key)
				}
			})
			executor := &responsesReasoningPipelineExecutor{failures: []error{relayAffinityOverload()}, events: rejectedReasoningCompactionEvents()}
			if exhausted {
				executor.failures = append(executor.failures, relayAffinityOverload(), relayAffinityOverload(), relayAffinityOverload())
			} else {
				executor.failures = append(executor.failures, agentRecoveryError("The encrypted content for item rs_source could not be verified. Reason: Encrypted content could not be decrypted or parsed."))
			}
			adapter := newRemoteCompactionAdapter(nil, nil, nil)
			stream, _, err := adapter.startLocalCompactionStream(ctx, outbound, request, executor,
				middleware, applyResponsesRejectedStatusCompatibility(outbound))
			wantCalls := 3
			if exhausted {
				wantCalls = responsesRelayAffinityMaxMigrations + 1
			}
			require.Len(t, executor.requests, wantCalls)
			require.Equal(t, gjson.GetBytes(executor.requests[0].Body, "input").Raw, gjson.GetBytes(executor.requests[1].Body, "input").Raw)
			require.NotEqual(t, executor.requests[0].Headers.Get("Thread-Id"), executor.requests[1].Headers.Get("Thread-Id"))
			if exhausted {
				require.Error(t, err)
				require.Nil(t, stream)
				return
			}
			require.NoError(t, err)
			require.Equal(t, executor.requests[1].Headers.Get("Thread-Id"), executor.requests[2].Headers.Get("Thread-Id"))
			require.Empty(t, encryptedResponsesReasoningHashes(executor.requests[2].Body))
			for stream.Next() {
				_ = stream.Current()
			}
			require.NoError(t, stream.Err())
			require.NoError(t, stream.Close())
			for key := range middleware.pending {
				_, confirmed := responsesRelayAffinities.Get(key)
				require.True(t, confirmed)
			}
		})
	}
}

func TestResponsesRejectedRelayAffinityThenReasoning(t *testing.T) {
	for _, raw := range []bool{false, true} {
		t.Run(fmt.Sprint(raw), func(t *testing.T) {
			ctx := shared.WithSessionScope(t.Context(), t.Name())
			request := relayAffinityRequest(t)
			executor := &responsesReasoningPipelineExecutor{
				failures: []error{relayAffinityOverload(), agentRecoveryError("The encrypted content for item rs_source could not be verified. Reason: Encrypted content could not be decrypted or parsed.")},
				events:   rejectedReasoningCompactionEvents(),
			}
			state, result, err := runRejectedReasoningPipeline(t, ctx, request, executor, "affinity-reasoning-combination", raw, 3,
				func(*PersistenceState, *PersistentOutboundTransformer) pipeline.Middleware {
					return &relayReplayTestDestination{}
				},
				relayAffinityMiddleware(t, nil),
			)
			require.NoError(t, err)
			drainRejectedReasoningPipeline(t, result)
			require.Len(t, executor.requests, 3)
			affinity := executor.requests[1].Headers.Get("Thread-Id")
			require.Equal(t, affinity, executor.requests[2].Headers.Get("Thread-Id"))
			require.Equal(t, gjson.GetBytes(executor.requests[0].Body, "input").Raw, gjson.GetBytes(executor.requests[1].Body, "input").Raw)
			require.Empty(t, encryptedResponsesReasoningHashes(executor.requests[2].Body))
			scope, ok := responsesReasoningScope(ctx, state.CurrentCandidate.Channel, executor.requests[1])
			require.True(t, ok)
			t.Cleanup(func() { responsesReasoningRecoveries.Remove(scope) })
			require.Eventually(t, func() bool {
				_, found := rememberedResponsesReasoningRule(scope, executor.requests[1].Body)
				return found
			}, time.Second, time.Millisecond)

			// A checkpoint created on the recovered session must keep that alias.
			// Full-history admission only gates a new migration, not this reuse.
			request.Body, err = sjson.SetRawBytes(request.Body, "input.8", []byte(`{"type":"compaction","id":"cmp_recovered_session","encrypted_content":"keep-native-checkpoint"}`))
			require.NoError(t, err)
			adapter := newRemoteCompactionAdapter(nil, nil, nil)
			ref, _, _, err := parseRemoteCompactionRequest(request.Body)
			require.NoError(t, err)
			owner := &PersistenceState{APIKey: &ent.APIKey{ID: 1, ProjectID: 1}}
			adapter.summaries.SetDefault(remoteCompactionOwnerCacheKey(owner, remoteCompactionCacheKey(ref)), "summary retained on the recovered session")
			next := &responsesReasoningPipelineExecutor{
				failures: []error{agentRecoveryError("The encrypted content for item cmp_recovered_session could not be verified. Reason: Encrypted content could not be decrypted or parsed.")},
				events:   rejectedReasoningCompactionEvents(),
			}
			_, result, err = runRejectedReasoningPipeline(t, ctx, request, next, "affinity-reasoning-combination", raw, 1,
				func(*PersistenceState, *PersistentOutboundTransformer) pipeline.Middleware {
					return &relayReplayTestDestination{}
				},
				func(_ *PersistenceState, outbound *PersistentOutboundTransformer) pipeline.Middleware {
					return prepareResponsesRelayAffinity(outbound)
				},
				func(_ *PersistenceState, outbound *PersistentOutboundTransformer) pipeline.Middleware {
					return recoverRejectedRemoteCompaction(outbound, adapter, next)
				},
				relayAffinityMiddleware(t, nil),
			)
			require.NoError(t, err)
			drainRejectedReasoningPipeline(t, result)
			require.Len(t, next.requests, 2)
			require.Equal(t, affinity, next.requests[0].Headers.Get("Thread-Id"))
			require.Equal(t, "keep-native-checkpoint", gjson.GetBytes(next.requests[0].Body, `input.#(type=="compaction").encrypted_content`).String())
			require.Equal(t, affinity, next.requests[1].Headers.Get("Thread-Id"))
			require.Contains(t, string(next.requests[1].Body), "summary retained on the recovered session")
			require.False(t, gjson.GetBytes(next.requests[1].Body, `input.#(type=="compaction")`).Exists())
		})
	}
}
