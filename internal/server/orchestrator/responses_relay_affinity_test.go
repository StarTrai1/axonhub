package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
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
				func(*PersistenceState, *PersistentOutboundTransformer) pipeline.Middleware { return &relayReplayTestDestination{} },
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
				func(*PersistenceState, *PersistentOutboundTransformer) pipeline.Middleware { return &relayReplayTestDestination{} },
				relayAffinityMiddleware(t, nil),
			)
			require.NoError(t, err)
			drainRejectedReasoningPipeline(t, result)
			require.Len(t, next.requests, 1)
			require.Equal(t, affinity, next.requests[0].Headers.Get("Thread-Id"))
		})
	}
}

func TestResponsesRejectedRelayAffinitySharesBudget(t *testing.T) {
	for _, retries := range []int{0, 1, 5} {
		t.Run(fmt.Sprint(retries), func(t *testing.T) {
			executor := &responsesReasoningPipelineExecutor{failures: []error{relayAffinityOverload(), relayAffinityOverload()}}
			var middleware *responsesRelayAffinityMiddleware
			_, _, err := runRejectedReasoningPipeline(t, t.Context(), relayAffinityRequest(t), executor, t.Name(), true, retries,
				func(*PersistenceState, *PersistentOutboundTransformer) pipeline.Middleware { return &relayReplayTestDestination{} },
				relayAffinityMiddleware(t, &middleware),
			)
			require.Error(t, err)
			want := 2
			if retries == 0 {
				want = 1
			}
			require.Len(t, executor.requests, want)
			for key := range middleware.pending {
				_, cached := responsesRelayAffinities.Get(key)
				require.False(t, cached, "an unsuccessful retry cannot establish a recovered affinity")
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		if r.Header.Get("Thread-Id") == originalThread {
			failure := relayAffinityOverload()
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-New-Api-Routed-Channel-Id", failure.Headers.Get("X-New-Api-Routed-Channel-Id"))
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write(failure.Body)
			return
		}
		if r.Header.Get("Thread-Id") == "" || gjson.GetBytes(body, "prompt_cache_key").String() != r.Header.Get("Thread-Id") {
			t.Error("inconsistent recovered upstream affinity")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if gjson.GetBytes(body, "input").Raw != gjson.GetBytes(request.Body, "input").Raw {
			t.Error("history changed while recovering affinity")
			w.WriteHeader(http.StatusBadRequest)
			return
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
	require.Equal(t, int32(2), attempts.Load())
}

func TestResponsesRejectedRelayAffinityScopeIsolation(t *testing.T) {
	request := relayAffinityRequest(t)
	executor := &responsesReasoningPipelineExecutor{failures: []error{relayAffinityOverload()}, events: rejectedReasoningCompactionEvents()}
	var middleware *responsesRelayAffinityMiddleware
	state, result, err := runRejectedReasoningPipeline(t, t.Context(), request, executor, "scope-credential", false, 1,
		func(*PersistenceState, *PersistentOutboundTransformer) pipeline.Middleware { return &relayReplayTestDestination{} },
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
