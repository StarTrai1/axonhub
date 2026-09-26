package orchestrator

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

func responsesReasoningRecoveryFixture(t *testing.T) (context.Context, *PersistentOutboundTransformer, *responsesRejectedStatusCompatibilityMiddleware, responsesReasoningRecoveryScope) {
	t.Helper()
	outbound := newCodexResponsesPassThroughOutbound()
	request := outbound.state.RawProviderRequest
	request.URL = "https://recovery.example/v1/responses"
	request.Headers.Set("Authorization", "Bearer synthetic-upstream-credential")
	request.Headers.Set("Thread-Id", "thread-"+t.Name())
	request.Headers.Set("X-Codex-Window-Id", "window-1")
	request.Body = []byte(responsesRejectedReasoningFixture)
	ctx := shared.WithSessionScope(t.Context(), "api_key:1:project:1")
	scope, ok := responsesReasoningScope(ctx, outbound.GetCurrentChannel(), request)
	require.True(t, ok)
	t.Cleanup(func() { responsesReasoningRecoveries.Remove(scope) })
	middleware := &responsesRejectedStatusCompatibilityMiddleware{outbound: outbound}
	return ctx, outbound, middleware, scope
}

func retryRejectedReasoning(t *testing.T, ctx context.Context, outbound *PersistentOutboundTransformer, middleware *responsesRejectedStatusCompatibilityMiddleware) {
	t.Helper()
	failure := &llm.ResponseError{StatusCode: http.StatusBadRequest, Detail: llm.ErrorDetail{Code: "invalid_encrypted_content"}}
	middleware.OnOutboundRawError(ctx, failure)
	require.True(t, outbound.CanRetry(failure))
	require.NoError(t, outbound.PrepareForRetry(ctx))
	_, err := middleware.OnOutboundRawRequest(ctx, outbound.state.RawProviderRequest)
	require.NoError(t, err)
	require.NotNil(t, middleware.recoveredReasoning)
}

func TestResponsesRejectedReasoningRemembersOnlySuccessfullyRecoveredItems(t *testing.T) {
	ctx, outbound, middleware, scope := responsesReasoningRecoveryFixture(t)
	original := *outbound.state.RawProviderRequest
	retryRejectedReasoning(t, ctx, outbound, middleware)
	_, found := rememberedResponsesReasoningRule(scope, original.Body)
	require.False(t, found, "rejection alone must not authorize later rewrites")
	_, err := middleware.OnOutboundRawResponse(ctx, &httpclient.Response{StatusCode: 200, Body: []byte(`{"status":"completed","output":[]}`)})
	require.NoError(t, err)

	// The next turn replays old rejected items alongside new valid reasoning.
	next, err := sjson.SetRawBytes(original.Body, "input.-1", []byte(`{"type":"reasoning","id":"rs_new","encrypted_content":"new-valid-reasoning","summary":[]}`))
	require.NoError(t, err)
	next, err = sjson.SetRawBytes(next, "input.-1", []byte(`{"type":"compaction_trigger"}`))
	require.NoError(t, err)
	fresh := newCodexResponsesPassThroughOutbound()
	fresh.state.CurrentCandidate = outbound.state.CurrentCandidate
	freshRequest := original
	freshRequest.Body = next
	fresh.state.RawProviderRequest = &freshRequest
	result, err := applyResponsesRejectedStatusCompatibility(fresh).OnOutboundRawRequest(ctx, &freshRequest)
	require.NoError(t, err)
	require.Empty(t, gjson.GetBytes(result.Body, `input.#(id=="rs_visible")`).Raw)
	require.Equal(t, "new-valid-reasoning", gjson.GetBytes(result.Body, `input.#(id=="rs_new").encrypted_content`).String())
	require.Equal(t, "compaction_trigger", gjson.GetBytes(result.Body, "input").Array()[len(gjson.GetBytes(result.Body, "input").Array())-1].Get("type").String())
	require.Equal(t, "visible summary\n\nvisible rationale", gjson.GetBytes(result.Body, "input.1.content.0.text").String())
	require.Equal(t, "keep function output", gjson.GetBytes(result.Body, "input.3.output").String())
	require.NotEmpty(t, gjson.GetBytes(next, "input.1.encrypted_content").String())
	require.False(t, hasResponsesRejectedStatusCompatibilityRetry(fresh.state, 1))

	changedID, err := sjson.SetBytes(original.Body, "input.1.id", "rs_different")
	require.NoError(t, err)
	rule, found := rememberedResponsesReasoningRule(scope, changedID)
	require.True(t, found, "the other known item may still match")
	resultBody, changed, err := stripResponsesRejectedStatus(changedID, []responsesRejectedStatusRule{rule})
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "rejected-with-summary", gjson.GetBytes(resultBody, `input.#(id=="rs_different").encrypted_content`).String())
}

func TestResponsesReasoningRecoveryDetachesOnlyRebuiltGroup(t *testing.T) {
	body := []byte(`{"input":[
		{"type":"message","id":"msg_user","role":"user","content":"context"},
		{"type":"compaction","id":"cmp_native","encrypted_content":"checkpoint"},
		{"type":"reasoning","id":"rs_bad","encrypted_content":"rejected","summary":[]},
		{"type":"message","id":"msg_bad","role":"assistant","phase":"commentary","content":[{"type":"output_text","text":"keep answer"}]},
		{"type":"custom_tool_call","id":"ctc_bad","call_id":"call_keep","name":"exec","input":"command"},
		{"type":"custom_tool_call_output","id":"out_bad","call_id":"call_keep","output":"keep result"},
		{"type":"reasoning","id":"rs_fresh","encrypted_content":"valid","summary":[]},
		{"type":"message","id":"msg_fresh","role":"assistant","content":"fresh answer"},
		{"type":"message","id":"msg_next","role":"user","content":"continue"}
	]}`)
	for _, cached := range []bool{false, true} {
		t.Run(fmt.Sprintf("cached=%t", cached), func(t *testing.T) {
			rule := responsesRejectedStatusRule{index: 2, field: "encrypted_content", dropItem: true, preserveCompaction: true}
			if cached {
				// A confirmed recovery matches only the old hash on the next turn.
				rule.index = -1
				rule.itemType = "reasoning"
				rule.reasoningHashes = map[[sha256.Size]byte]struct{}{responsesReasoningItemHash(gjson.GetBytes(body, "input.2")): {}}
			}
			got, changed, err := stripResponsesRejectedStatus(body, []responsesRejectedStatusRule{rule})
			require.NoError(t, err)
			require.True(t, changed)
			for _, index := range []int{2, 3, 4} {
				require.False(t, gjson.GetBytes(got, fmt.Sprintf("input.%d.id", index)).Exists())
			}
			require.Equal(t, "keep answer", gjson.GetBytes(got, "input.2.content.0.text").String())
			require.Equal(t, "commentary", gjson.GetBytes(got, "input.2.phase").String())
			require.Equal(t, "call_keep", gjson.GetBytes(got, "input.3.call_id").String())
			require.Equal(t, "call_keep", gjson.GetBytes(got, "input.4.call_id").String())
			require.Equal(t, "keep result", gjson.GetBytes(got, "input.4.output").String())
			for _, pair := range [][2]int{{0, 0}, {1, 1}, {6, 5}, {7, 6}, {8, 7}} {
				require.JSONEq(t, gjson.GetBytes(body, fmt.Sprintf("input.%d", pair[0])).Raw, gjson.GetBytes(got, fmt.Sprintf("input.%d", pair[1])).Raw)
			}
			require.Equal(t, "msg_bad", gjson.GetBytes(body, "input.3.id").String(), "caller input remains unchanged")
		})
	}
}

func TestResponsesRejectedReasoningCacheScopeAndExpiry(t *testing.T) {
	ctx, outbound, middleware, scope := responsesReasoningRecoveryFixture(t)
	original := *outbound.state.RawProviderRequest
	retryRejectedReasoning(t, ctx, outbound, middleware)
	middleware.recoveredReasoning.confirm()
	first, found := responsesReasoningRecoveries.Peek(scope)
	require.True(t, found)

	for _, tt := range []struct {
		name   string
		mutate func(*responsesReasoningRecoveryScope)
	}{
		{"owner", func(s *responsesReasoningRecoveryScope) { s.owner = sha256.Sum256([]byte("another-owner")) }},
		{"thread", func(s *responsesReasoningRecoveryScope) { s.threadID += "-child" }},
		{"window", func(s *responsesReasoningRecoveryScope) { s.windowID = "window-2" }},
		{"channel", func(s *responsesReasoningRecoveryScope) { s.provider.channelID++ }},
		{"endpoint", func(s *responsesReasoningRecoveryScope) { s.provider.url += "/compact" }},
		{"model", func(s *responsesReasoningRecoveryScope) { s.provider.model = "different-model" }},
		{"credential", func(s *responsesReasoningRecoveryScope) { s.provider.credential = sha256.Sum256([]byte("different-credential")) }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			changed := scope
			tt.mutate(&changed)
			_, matched := rememberedResponsesReasoningRule(changed, original.Body)
			require.False(t, matched)
		})
	}
	middleware.recoveredReasoning.confirm()
	second, _ := responsesReasoningRecoveries.Peek(scope)
	require.Equal(t, first.expiresAt, second.expiresAt, "cache hits must not extend absolute expiry")
	second.expiresAt = time.Now().Add(-time.Second)
	responsesReasoningRecoveries.Add(scope, second)
	_, found = rememberedResponsesReasoningRule(scope, original.Body)
	require.False(t, found)
	_, found = responsesReasoningRecoveries.Peek(scope)
	require.False(t, found)
}

func TestResponsesRejectedReasoningCachedRecoveryStillRequiresExplicitHistory(t *testing.T) {
	ctx, outbound, middleware, scope := responsesReasoningRecoveryFixture(t)
	original := *outbound.state.RawProviderRequest
	retryRejectedReasoning(t, ctx, outbound, middleware)
	middleware.recoveredReasoning.confirm()
	for _, change := range []struct {
		path  string
		value any
	}{
		{"previous_response_id", "resp_unresolved"},
		{"input.9", map[string]any{"type": "compaction", "id": "cmp_axonhub_unknown", "encrypted_content": "opaque"}},
		{"input.3.call_id", "missing_call"},
		{"input.2.encrypted_function_args", "opaque"},
		{"input.9", map[string]any{"type": "future_opaque_state"}},
	} {
		body, err := sjson.SetBytes(original.Body, change.path, change.value)
		require.NoError(t, err)
		_, found := rememberedResponsesReasoningRule(scope, body)
		require.False(t, found)
	}
}

func TestResponsesRejectedReasoningConfirmsOnlySuccessfulTerminal(t *testing.T) {
	for _, tt := range []struct {
		name   string
		events []string
		want   bool
	}{
		{"complete", []string{`{"type":"response.completed","response":{"status":"completed","output":[]}}`}, true},
		{"created then EOF", []string{`{"type":"response.created","response":{"status":"in_progress"}}`}, false},
		{"failed", []string{`{"type":"response.failed","response":{"status":"failed","error":{"code":"invalid_encrypted_content"}}}`}, false},
		{"incomplete", []string{`{"type":"response.incomplete","response":{"status":"incomplete"}}`}, false},
		{"error then fake complete", []string{`{"type":"error","error":{"code":"invalid_encrypted_content"}}`, `{"type":"response.completed","response":{"status":"completed"}}`}, false},
		{"completion with embedded error", []string{`{"type":"response.completed","response":{"status":"completed","error":{"code":"failed"}}}`}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, outbound, middleware, scope := responsesReasoningRecoveryFixture(t)
			original := *outbound.state.RawProviderRequest
			retryRejectedReasoning(t, ctx, outbound, middleware)
			events := make([]*httpclient.StreamEvent, 0, len(tt.events))
			for _, body := range tt.events {
				events = append(events, &httpclient.StreamEvent{Data: []byte(body)})
			}
			stream, err := middleware.OnOutboundRawStream(ctx, streams.SliceStream(events))
			require.NoError(t, err)
			var forwarded []string
			for stream.Next() {
				event := stream.Current()
				require.NotNil(t, event)
				require.Same(t, event, stream.Current(), "observing an event must not consume another source item")
				forwarded = append(forwarded, string(event.Data))
			}
			require.NoError(t, stream.Err())
			require.NoError(t, stream.Close())
			require.Equal(t, tt.events, forwarded)
			_, found := rememberedResponsesReasoningRule(scope, original.Body)
			require.Equal(t, tt.want, found)
		})
	}
}

func TestResponsesRejectedReasoningCacheUsesFinalCredentialAndRequiresIdentity(t *testing.T) {
	ctx, outbound, _, scope := responsesReasoningRecoveryFixture(t)
	original := *outbound.state.RawProviderRequest
	changed := original
	changed.Headers = original.Headers.Clone()
	changed.Headers.Set("Authorization", "Bearer changed-upstream")
	other, ok := responsesReasoningScope(ctx, outbound.GetCurrentChannel(), &changed)
	require.True(t, ok)
	require.NotEqual(t, scope, other)
	for _, header := range []string{"Authorization", "X-Codex-Window-Id"} {
		changed.Headers = original.Headers.Clone()
		changed.Headers.Del(header)
		_, ok = responsesReasoningScope(ctx, outbound.GetCurrentChannel(), &changed)
		require.False(t, ok, header)
	}
	changed.Headers = original.Headers.Clone()
	changed.Headers.Del("Thread-Id")
	other, ok = responsesReasoningScope(ctx, outbound.GetCurrentChannel(), &changed)
	require.True(t, ok, "the shared metadata reader can derive a thread from its window")
	require.NotEqual(t, scope, other, "a different derived thread cannot reuse this recovery")
	_, ok = responsesReasoningScope(context.Background(), outbound.GetCurrentChannel(), &original)
	require.False(t, ok)
}

func TestResponsesRejectedReasoningRejectsUnsuccessfulResponses(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{"complete", 200, `{"status":"completed","output":[]}`, true},
		{"standalone compaction", 200, `{"object":"response.compaction","output":[{"type":"compaction","encrypted_content":"new"}]}`, true},
		{"incomplete", 200, `{"status":"incomplete","output":[]}`, false},
		{"server error", 502, `{"status":"completed","output":[]}`, false},
		{"embedded error", 200, `{"status":"completed","error":{"code":"failed"}}`, false},
		{"empty compact output", 200, `{"object":"response.compaction","output":[]}`, false},
		{"truncated JSON", 200, `{"status":"completed",`, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ctx, outbound, middleware, scope := responsesReasoningRecoveryFixture(t)
			original := *outbound.state.RawProviderRequest
			retryRejectedReasoning(t, ctx, outbound, middleware)
			_, err := middleware.OnOutboundRawResponse(ctx, &httpclient.Response{StatusCode: scenario.status, Body: []byte(scenario.body)})
			require.NoError(t, err)
			_, found := rememberedResponsesReasoningRule(scope, original.Body)
			require.Equal(t, scenario.want, found)
		})
	}
}

func TestResponsesRejectedReasoningCacheConcurrentConfirmAndLookup(t *testing.T) {
	ctx, outbound, middleware, scope := responsesReasoningRecoveryFixture(t)
	original := *outbound.state.RawProviderRequest
	retryRejectedReasoning(t, ctx, outbound, middleware)
	recovery := middleware.recoveredReasoning
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			defer func() {
				if value := recover(); value != nil {
					t.Errorf("concurrent cache access panicked: %v", value)
				}
			}()
			for range 10 {
				recovery.confirm()
				_, _ = rememberedResponsesReasoningRule(scope, original.Body)
			}
		})
	}
	wg.Wait()
	entry, found := responsesReasoningRecoveries.Peek(scope)
	require.True(t, found)
	require.Len(t, entry.hashes, 2)
	bounded := &responsesReasoningRecovery{scope: scope, hashes: make(map[[sha256.Size]byte]struct{})}
	for i := range responsesReasoningRecoveryMaxHashes + 10 {
		bounded.hashes[sha256.Sum256([]byte(fmt.Sprint(i)))] = struct{}{}
	}
	bounded.confirm()
	entry, _ = responsesReasoningRecoveries.Peek(scope)
	require.Len(t, entry.hashes, responsesReasoningRecoveryMaxHashes)
}
