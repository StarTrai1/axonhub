package orchestrator

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/request"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

func TestResponsesSessionRestoresAcceptedWireWithoutExpandingArchive(t *testing.T) {
	for _, scenario := range []string{"restore", "new connection", "missing", "foreign", "failed", "disabled"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, client, state, adapter := unifiedHistoryFixture(t)
			wire := `{"model":"gpt-6-astra","input":[{"role":"user","content":"preserve original instruction"},{"type":"compaction","id":"cmp_accepted","encrypted_content":"unchanged-accepted-cipher"}]}`
			owner := state.APIKey
			if scenario == "foreign" {
				owner = &ent.APIKey{ID: owner.ID + 1, ProjectID: owner.ProjectID}
			}
			row := seedCompactionPredecessor(t, ctx, client, owner, "resp_accepted", wire, `[{"type":"function_call","call_id":"call_exact","name":"inspect","arguments":"{}"}]`)
			if scenario == "missing" {
				require.NoError(t, client.Request.DeleteOne(row).Exec(ctx))
			}
			if scenario == "failed" {
				require.NoError(t, row.Update().SetStatus(request.StatusFailed).Exec(ctx))
			}
			// The raw archive intentionally differs: recovery must retain the
			// accepted native checkpoint rather than expanding all old originals.
			require.NoError(t, adapter.saveHistorySnapshot(ctx, historyResponseAlias("resp_accepted"), []byte(`{"model":"gpt-6-astra","input":[{"role":"user","content":"archived-only-original"}]}`), []byte(`{"id":"resp_accepted","status":"completed","output":[]}`)))
			if scenario == "disabled" {
				adapter.systemService.UnifiedHistory.Enabled = false
			}
			raw := &httpclient.Request{Body: []byte(`{"model":"gpt-6-astra","previous_response_id":"resp_accepted","input":[{"type":"function_call_output","call_id":"call_exact","output":"exact tool result"}]}`)}
			llmRequest, err := responses.NewInboundTransformer().TransformRequest(ctx, raw)
			require.NoError(t, err)
			state.RawRequest, state.LlmRequest = raw, llmRequest
			state.SystemService, state.RequestService = adapter.systemService, adapter.requestService
			state.RawProviderRequest = &httpclient.Request{APIFormat: string(llm.APIFormatOpenAIResponseWebSocket)}
			state.ResponsesContinuation = newResponsesContinuation(nil, raw.Body)
			if scenario == "new connection" { state.RawProviderRequest = nil; state.CurrentCandidate.APIFormat = string(llm.APIFormatOpenAIResponseWebSocket) }
            p := &PersistentOutboundTransformer{state: state}
			restored, err := p.RestoreResponsesContinuation(ctx, llmRequest)
			require.NoError(t, err)
			if scenario != "restore" && scenario != "new connection" {
				require.Nil(t, restored)
				return
			}
			require.NotNil(t, restored)
			body := restored.RawRequest.Body
			require.Len(t, gjson.GetBytes(body, "input").Array(), 4)
			require.Equal(t, "unchanged-accepted-cipher", gjson.GetBytes(body, "input.1.encrypted_content").String())
			require.Equal(t, "exact tool result", gjson.GetBytes(body, "input.3.output").String())
			require.NotContains(t, string(body), "archived-only-original")
			require.False(t, gjson.GetBytes(body, "previous_response_id").Exists())
			require.Contains(t, string(raw.Body), "previous_response_id", "the failed original request remains unchanged")
		})
	}
}
