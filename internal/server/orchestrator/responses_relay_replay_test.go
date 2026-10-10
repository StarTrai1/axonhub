package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

type relayReplayTestDestination struct{ pipeline.DummyMiddleware }

func (m *relayReplayTestDestination) OnOutboundRawRequest(_ context.Context, request *httpclient.Request) (*httpclient.Request, error) {
	request.URL = "https://relay.invalid/v1/responses"
	return request, nil
}

func TestResponsesRejectedRelayReplayPreservesCiphertextAndHistory(t *testing.T) {
	for _, passThrough := range []bool{false, true} {
		t.Run(map[bool]string{false: "converted", true: "passthrough"}[passThrough], func(t *testing.T) {
			request := rejectedReasoningPipelineRequest(t, llm.APIFormatOpenAIResponse)
			request.Body = []byte(responsesResourceHistoryFixture)
			original := append([]byte(nil), request.Body...)
			executor := &responsesReasoningPipelineExecutor{
				failures: []error{opaqueRelayReplayError()}, events: rejectedReasoningCompactionEvents(),
			}
			ctx := shared.WithSessionScope(t.Context(), "relay-replay-owner")
			state, result, err := runRejectedReasoningPipeline(t, ctx, request, executor, "relay-replay-credential", passThrough, 1,
				func(*PersistenceState, *PersistentOutboundTransformer) pipeline.Middleware {
					return &relayReplayTestDestination{}
				},
			)
			require.NoError(t, err)
			drainRejectedReasoningPipeline(t, result)
			require.Len(t, executor.requests, 2)
			before, after := executor.requests[0], executor.requests[1]
			var expected map[string]any
			require.NoError(t, json.Unmarshal(before.Body, &expected))
			for _, item := range expected["input"].([]any) {
				delete(item.(map[string]any), "id")
			}
			encoded, err := json.Marshal(expected)
			require.NoError(t, err)
			require.JSONEq(t, string(encoded), string(after.Body), "only old item IDs may change")
			require.Equal(t, "preserve-native-reasoning", gjson.GetBytes(after.Body, "input.2.encrypted_content").String())
			require.Equal(t, original, request.Body)
			require.Equal(t, before.Headers.Get("Authorization"), after.Headers.Get("Authorization"))
			scope, ok := responsesReasoningScope(ctx, state.CurrentCandidate.Channel, before)
			require.True(t, ok)
			_, remembered := rememberedResponsesReasoningRule(scope, before.Body)
			require.False(t, remembered, "ID detachment must not be learned as permission to drop ciphertext")
		})
	}
}

func TestResponsesRejectedRelayReplaySharesBudgetAndStops(t *testing.T) {
	for _, retries := range []int{0, 1, 4} {
		request := rejectedReasoningPipelineRequest(t, llm.APIFormatOpenAIResponse)
		request.Body = []byte(responsesResourceHistoryFixture)
		executor := &responsesReasoningPipelineExecutor{failures: []error{opaqueRelayReplayError(), opaqueRelayReplayError()}}
		_, _, err := runRejectedReasoningPipeline(t, t.Context(), request, executor, "relay-replay-bounded", true, retries,
			func(*PersistenceState, *PersistentOutboundTransformer) pipeline.Middleware {
				return &relayReplayTestDestination{}
			},
		)
		require.Error(t, err)
		want := 2
		if retries == 0 {
			want = 1
		}
		require.Len(t, executor.requests, want)
	}
	request := rejectedReasoningPipelineRequest(t, llm.APIFormatOpenAIResponse)
	request.Body = []byte(responsesResourceHistoryFixture)
	executor := &responsesReasoningPipelineExecutor{failures: []error{opaqueRelayReplayError()}}
	_, _, err := runRejectedReasoningPipeline(t, t.Context(), request, executor, "official-unchanged", true, 3)
	require.Error(t, err)
	require.Len(t, executor.requests, 1, "official endpoints do not use opaque relay error recovery")
}

func TestResponsesRejectedRelayReplayAfterAgentRecovery(t *testing.T) {
	for _, passThrough := range []bool{false, true} {
		for _, portable := range []bool{false, true} {
			t.Run(fmt.Sprintf("passthrough=%t/portable=%t", passThrough, portable), func(t *testing.T) {
				client := enttest.NewEntClient(t, "sqlite3", fmt.Sprintf("file:relay-agent-%t-%t?mode=memory&_fk=0", passThrough, portable))
				t.Cleanup(func() { client.Close() })
				ctx := authz.WithTestBypass(ent.NewContext(t.Context(), client))
				service := biz.NewSystemService(biz.SystemServiceParams{Ent: client})
				require.NoError(t, service.SetSecretKey(ctx, "test-agent-message-installation-secret"))
				owner := agentMessageTestState()
				agent := []byte(gjson.Get(responsesAgentMessageFixture, "input.1").Raw)
				if portable {
					aead, err := newResponsesAgentMessageCipher("test-agent-message-installation-secret")
					require.NoError(t, err)
					aad, err := responsesAgentMessageAAD(owner, "")
					require.NoError(t, err)
					sealed, err := sealResponsesAgentMessage(aead, aad, "Exact intermediate message.\n包括全部原文。")
					require.NoError(t, err)
					agent, err = sjson.SetBytes(agent, "content.1.encrypted_content", sealed)
					require.NoError(t, err)
				} else {
					importTestAgentMessage(t, ctx, client, owner)
				}
				rowsBefore, err := client.System.Query().Count(ctx)
				require.NoError(t, err)
				request := rejectedReasoningPipelineRequest(t, llm.APIFormatOpenAIResponse)
				request.Body, err = sjson.SetRawBytes([]byte(responsesResourceHistoryFixture), "input.-1", agent)
				require.NoError(t, err)
				request.Body, err = sjson.SetRawBytes(request.Body, "input.-1", []byte(preservedPlainAgentMessage))
				require.NoError(t, err)
				original := append([]byte(nil), request.Body...)
				for range 2 {
					// A fresh middleware/service models another continuation or a
					// restart; neither case imports another message recovery row.
					restarted := biz.NewSystemService(biz.SystemServiceParams{Ent: client})
					executor := &responsesReasoningPipelineExecutor{failures: []error{opaqueRelayReplayError()}, events: rejectedReasoningCompactionEvents()}
					_, result, err := runRejectedReasoningPipeline(t, ctx, request, executor, t.Name(), passThrough, 1,
						func(*PersistenceState, *PersistentOutboundTransformer) pipeline.Middleware {
							return &relayReplayTestDestination{}
						},
						func(state *PersistenceState, outbound *PersistentOutboundTransformer) pipeline.Middleware {
							state.APIKey = owner.APIKey
							return recoverResponsesAgentMessages(outbound, restarted)
						},
					)
					require.NoError(t, err)
					require.Contains(t, drainRejectedReasoningPipeline(t, result), "response.completed")
					require.Len(t, executor.requests, 2)
					before, after := executor.requests[0], executor.requests[1]
					restored := gjson.GetBytes(before.Body, `input.#(id=="amsg_original")`)
					require.Equal(t, "Exact intermediate message.\n包括全部原文。", restored.Get("content.1.text").String())
					require.Equal(t, restored.Raw, gjson.GetBytes(after.Body, `input.#(id=="amsg_original")`).Raw)
					var expected map[string]any
					require.NoError(t, json.Unmarshal(before.Body, &expected))
					for _, input := range expected["input"].([]any) {
						item := input.(map[string]any)
						if item["type"] != "agent_message" {
							delete(item, "id")
						}
					}
					encoded, err := json.Marshal(expected)
					require.NoError(t, err)
					require.JSONEq(t, string(encoded), string(after.Body), "only optional history IDs may change; agent delivery IDs, content, ciphertext and call_id remain intact")
					require.Equal(t, before.Headers.Get("Authorization"), after.Headers.Get("Authorization"))
					require.Equal(t, before.URL, after.URL)
					require.Equal(t, original, request.Body)
				}
				rowsAfter, err := client.System.Query().Count(ctx)
				require.NoError(t, err)
				require.Equal(t, rowsBefore, rowsAfter, "continuation must not create or reimport message recovery rows")
			})
		}
	}
}

func TestResponsesRejectedRelayReplayGuards(t *testing.T) {
	body := []byte(responsesResourceHistoryFixture)
	for _, change := range []struct {
		path  string
		value any
	}{
		{"previous_response_id", "resp_external"},
		{"conversation", map[string]string{"id": "conv_external"}},
		{"input.8", map[string]string{"type": "compaction", "id": "cmp_native", "encrypted_content": "checkpoint"}},
		{"input.8", map[string]string{"type": "item_reference", "id": "msg_external"}},
		{"input.8", map[string]string{"type": "future_state"}},
		{"input.5.call_id", "unpaired"},
		{"input.4.encrypted_function_args", "opaque"},
		{"input.5.output", []map[string]string{{"type": "encrypted_content", "encrypted_content": "opaque"}}},
		{"input.8", json.RawMessage(preservedEncryptedAgentMessage)},
		{"input.8", map[string]any{"type": "agent_message", "id": "am_incomplete", "author": "/root/worker", "content": []map[string]string{{"type": "input_text", "text": "missing recipient"}}}},
		{"input.8", map[string]any{"type": "agent_message", "id": "am_reference", "author": "/root/worker", "recipient": "/root", "content": []map[string]string{{"type": "input_text", "text": "external reference", "file_id": "file_external"}}}},
	} {
		changed, err := sjson.SetBytes(body, change.path, change.value)
		require.NoError(t, err)
		_, ok := responsesRelayReplayRule(changed, "", "bad response status code 400", "")
		require.False(t, ok, change.path)
	}
	for _, detail := range []struct{ code, message, param string }{
		{"permission_denied", "bad response status code 400", ""},
		{"", "bad response status code 400", "model"},
		{"", "bad response status code 400: invalid model", ""},
	} {
		_, ok := responsesRelayReplayRule(body, detail.code, detail.message, detail.param)
		require.False(t, ok)
	}
	_, ok := responsesRelayReplayRule(body, "", "bad response status code 400 (request id: synthetic-123)", "")
	require.True(t, ok)
}

func opaqueRelayReplayError() error {
	return &httpclient.Error{StatusCode: http.StatusBadRequest, Body: []byte(`{"error":{"type":"invalid_request_error","message":"bad response status code 400 (request id: synthetic-123)"}}`)}
}
