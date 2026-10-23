package orchestrator

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

func TestCheckpointGenericResourceCorrectionPreservesNativeState(t *testing.T) {
	body, err := sjson.SetRawBytes([]byte(responsesCompactionHistoryFixture), "input.-1", []byte(`{"type":"reasoning","id":"rs_stored","encrypted_content":"intact-reasoning","summary":[]}`))
	require.NoError(t, err)
	rule, ok := responsesRejectedResourceRule(body, "", responsesResourceRedactedMessage, "")
	require.True(t, ok)
	require.True(t, rule.detachReasoningID)
	after, changed, err := stripResponsesRejectedStatus(body, []responsesRejectedStatusRule{rule})
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, gjson.GetBytes(body, "input.2").Raw, gjson.GetBytes(after, "input.2").Raw)
	require.Equal(t, "intact-reasoning", gjson.GetBytes(after, "input.7.encrypted_content").String())
	require.False(t, gjson.GetBytes(after, "input.7.id").Exists())
	require.Nil(t, newResponsesReasoningRecovery(responsesReasoningRecoveryScope{}, body, after), "ID-only success must never teach ciphertext deletion")
	require.False(t, responsesResourceHasNativeReasoningIDs(after), "the same ID correction cannot be scheduled twice")
	_, ok = responsesRejectedResourceRule(after, "", responsesResourceRedactedMessage, "")
	require.False(t, ok, "generic rejection must not discard reasoning around an opaque checkpoint")

	indexed, ok := responsesRejectedResourceRule(body, "", responsesResourceRedactedMessage, "input[3].id")
	require.True(t, ok)
	require.False(t, indexed.detachReasoningID, "an indexed message rejection does not reject reasoning IDs")
	for _, change := range []struct {
		path  string
		value any
	}{
		{"input.5.call_id", "orphan"},
		{"input.5.output", []map[string]string{{"type": "encrypted_content", "encrypted_content": "opaque-tool"}}},
		{"input.-1", map[string]string{"type": "compaction", "id": "cmp_extra", "encrypted_content": "extra"}},
		{"previous_response_id", "unresolved"},
	} {
		changed, err := sjson.SetBytes(body, change.path, change.value)
		require.NoError(t, err)
		require.False(t, responsesResourceHasNativeReasoningIDs(changed), change.path)
	}
}

func TestCheckpointDeltaRecoveryUsesAuthenticatedSource(t *testing.T) {
	for _, scenario := range []string{"authenticated", "incomplete", "missing snapshot", "wrong previous", "unresolved effective", "override checkpoint"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := shared.WithSessionScope(t.Context(), "delta-owner")
			outbound := newCodexResponsesPassThroughOutbound()
			outbound.state.APIKey = &ent.APIKey{ID: 205, ProjectID: 206}
			raw := rejectedCompactionPipelineRequest(t, llm.APIFormatOpenAIResponse)
			store := newResponsesSessionStore()
			store.record(ctx, raw.Body, []byte(`{"id":"resp_previous","status":"completed","output":[]}`))
			var err error
			raw.Body, err = sjson.SetBytes(raw.Body, "previous_response_id", "resp_previous")
			require.NoError(t, err)
			raw.Body, err = sjson.SetRawBytes(raw.Body, "input", []byte(`[{"role":"user","content":"delta-canary"}]`))
			require.NoError(t, err)
			outbound.state.RawRequest = raw
			outbound.state.ResponsesContinuation = newResponsesContinuation(store, raw.Body)
			effective, err := prepareResponsesAttemptBody(ctx, outbound, responsesAttemptTarget{})
			require.NoError(t, err)
			provider := cloneRawRequest(raw)
			provider.Body = effective
			provider.URL = "https://example.invalid/v1/responses"
			provider.Headers.Set("Authorization", "Bearer synthetic-delta")
			c := outbound.state.ResponsesContinuation
			switch scenario {
			case "incomplete":
				c.complete = false
			case "missing snapshot":
				c.owner.Snapshot = nil
			case "wrong previous":
				c.owner.ID = "another-response"
			case "unresolved effective":
				c.effective = raw.Body
			case "override checkpoint":
				provider.Body, err = sjson.SetBytes(provider.Body, "input.2.encrypted_content", "substituted")
				require.NoError(t, err)
			}
			middleware := &responsesCompactionRecoveryMiddleware{outbound: outbound}
			_, ref, ok := middleware.recoveryKey(ctx, provider)
			require.Equal(t, scenario == "authenticated", ok)
			if ok {
				require.Equal(t, "cmp_native", ref.ID)
			}
		})
	}
}

func TestCheckpointRecoveryDoesNotInheritUnrelatedPlaintextPolicy(t *testing.T) {
	ctx, client, apiKey, adapter := rejectedCompactionStorage(t)
	for range 2 {
		seedPortableHistoryPair(t, ctx, client, apiKey, portableHistoryBefore, portableHistoryAfter, responsesResourceMismatchMessage)
	}
	ctx = contexts.WithChannelAPIKey(ctx, portableHistoryCredential)
	outbound, request := portableHistoryOutbound(apiKey, adapter.requestService)
	original := rejectedCompactionPipelineRequest(t, llm.APIFormatOpenAIResponse)
	var err error
	original.Body, err = sjson.SetRawBytes(original.Body, "input.-1", []byte(`{"type":"reasoning","id":"rs_suffix","encrypted_content":"accepted-suffix","summary":[]}`))
	require.NoError(t, err)
	outbound.state.RawRequest = original
	request.Body, err = replaceRemoteCompactionWithLocalSummary(original.Body, "verified retained source summary")
	require.NoError(t, err)
	before := append([]byte(nil), request.Body...)
	got, err := applyResponsesHistoryPortability(outbound).OnOutboundRawRequest(ctx, request)
	require.NoError(t, err)
	require.Equal(t, before, got.Body, "replacing a checkpoint does not reject its suffix reasoning")

	// A successful effective window has stronger evidence than unrelated
	// failures on the route, including after a snapshot is loaded on reconnect.
	request.Body = []byte(portableHistoryBefore)
	request.Headers.Set("Thread-Id", "accepted-thread")
	request.Headers.Set("X-Codex-Window-Id", "accepted-window")
	outbound.state.RawRequest.Body = []byte(`{"previous_response_id":"resp_accepted","input":"next"}`)
	outbound.state.ResponsesContinuation = &responsesContinuation{
		previous: "resp_accepted", complete: true,
		owner: &shared.ResponsesRecord{ID: "resp_accepted", Snapshot: &shared.ResponsesSnapshot{Input: []json.RawMessage{[]byte(`{"role":"user","content":"accepted"}`)}}, Origin: shared.ResponsesOrigin{
			ChannelID: outbound.GetCurrentChannel().ID, Endpoint: request.URL, Format: request.APIFormat,
			Model: "gpt-6-astra", Auth: responses.ContinuationAuthIdentity(request),
		}},
	}
	got, err = applyResponsesHistoryPortability(outbound).OnOutboundRawRequest(ctx, request)
	require.NoError(t, err)
	require.Equal(t, portableHistoryBefore, string(got.Body))
}
