package orchestrator

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	entchannel "github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
)

const responsesAgentMessageFixture = `{"model":"gpt-6-astra","stream":true,"input":[
	{"type":"message","role":"user","content":[{"type":"input_text","text":"retained full task"}]},
	{"type":"agent_message","id":"amsg_original","author":"/root/reviewer","recipient":"/root","content":[{"type":"input_text","text":"original delivery prefix\n"},{"type":"encrypted_content","encrypted_content":"opaque-original-body","extra":"keep-part-metadata"}],"internal_chat_message_metadata_passthrough":{"turn_id":"retain-turn"}},
	{"type":"function_call","id":"fc_original","call_id":"call_original","name":"inspect","arguments":"{}"},
	{"type":"function_call_output","call_id":"call_original","output":"complete tool result"},
	{"type":"agent_message","id":"amsg_unavailable","author":"/root/other","recipient":"/root","content":[{"type":"encrypted_content","encrypted_content":"opaque-unavailable-body"}]}
]}`

func agentMessageTestState() *PersistenceState {
	return &PersistenceState{
		APIKey:           &ent.APIKey{ID: 1, ProjectID: 1},
		CurrentCandidate: &ChannelModelsCandidate{Channel: &biz.Channel{Channel: &ent.Channel{ID: 1, Type: entchannel.TypeCodex}}},
	}
}

func importTestAgentMessage(t *testing.T, ctx context.Context, client *ent.Client, state *PersistenceState) string {
	t.Helper()
	item := gjson.Get(responsesAgentMessageFixture, "input.1")
	digest := responsesAgentMessageRecoveryDigest(item, item.Get("content.1.encrypted_content").String())
	aead, err := newResponsesAgentMessageCipher("test-agent-message-installation-secret")
	require.NoError(t, err)
	aad, err := responsesAgentMessageAAD(state, digest)
	require.NoError(t, err)
	sealed, err := sealResponsesAgentMessage(aead, aad, "Exact intermediate message.\n包括全部原文。")
	require.NoError(t, err)
	key := fmt.Sprintf("agent_message_recovery_v1:%d:%d:%s", state.APIKey.ProjectID, state.APIKey.ID, digest)
	_, err = client.System.Create().SetKey(key).SetValue(sealed).Save(ctx)
	require.NoError(t, err)
	return key
}

func TestResponsesAgentMessageRecoveryPreservesHistory(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:agent-message-recovery?mode=memory&_fk=0")
	t.Cleanup(func() { client.Close() })
	ctx := authz.WithTestBypass(ent.NewContext(t.Context(), client))
	service := biz.NewSystemService(biz.SystemServiceParams{Ent: client})
	require.NoError(t, service.SetSecretKey(ctx, "test-agent-message-installation-secret"))
	state := agentMessageTestState()
	outbound := &PersistentOutboundTransformer{state: state}
	request := &httpclient.Request{APIFormat: string(llm.APIFormatOpenAIResponse), Body: []byte(responsesAgentMessageFixture), JSONBody: []byte(responsesAgentMessageFixture), Headers: http.Header{codexTurnStateHeader: []string{"original-state"}}}
	middleware := recoverResponsesAgentMessages(outbound, service)
	unchanged, err := middleware.OnOutboundRawRequest(ctx, request)
	require.NoError(t, err)
	require.Same(t, request, unchanged, "unrecoverable ciphertext must stay intact")
	importTestAgentMessage(t, ctx, client, state)

	// A prior miss, a service restart, and a different destination must not
	// require another paid source request or discard the imported plaintext.
	state.CurrentCandidate.Channel.ID = 2
	service = biz.NewSystemService(biz.SystemServiceParams{Ent: client})
	restored, err := recoverResponsesAgentMessages(outbound, service).OnOutboundRawRequest(ctx, request)
	require.NoError(t, err)
	require.NotSame(t, request, restored)
	require.Equal(t, responsesAgentMessageFixture, string(request.Body))
	require.Equal(t, "original-state", request.Headers.Get(codexTurnStateHeader))
	require.Empty(t, restored.Headers.Get(codexTurnStateHeader))
	require.Equal(t, restored.Body, restored.JSONBody)
	require.Equal(t, "Exact intermediate message.\n包括全部原文。", gjson.GetBytes(restored.Body, "input.1.content.1.text").String())
	require.False(t, gjson.GetBytes(restored.Body, "input.1.content.1.encrypted_content").Exists())
	require.Equal(t, "keep-part-metadata", gjson.GetBytes(restored.Body, "input.1.content.1.extra").String())
	for _, path := range []string{"input.0", "input.1.id", "input.1.author", "input.1.recipient", "input.1.content.0", "input.1.internal_chat_message_metadata_passthrough", "input.2", "input.3", "input.4"} {
		require.JSONEq(t, gjson.Get(responsesAgentMessageFixture, path).Raw, gjson.GetBytes(restored.Body, path).Raw, path)
	}
	again, err := middleware.OnOutboundRawRequest(ctx, restored)
	require.NoError(t, err)
	require.Same(t, restored, again)

	for _, path := range []string{"input.1.id", "input.1.author", "input.1.recipient", "input.1.content.1.encrypted_content"} {
		changed := *request
		changed.Body, err = sjson.SetBytes(request.Body, path, "different")
		require.NoError(t, err)
		got, err := middleware.OnOutboundRawRequest(ctx, &changed)
		require.NoError(t, err)
		require.Equal(t, changed.Body, got.Body, path)
	}
	for _, owner := range []*ent.APIKey{{ID: 2, ProjectID: 1}, {ID: 1, ProjectID: 2}} {
		state.APIKey = owner
		got, err := middleware.OnOutboundRawRequest(ctx, request)
		require.NoError(t, err)
		require.Same(t, request, got)
	}
}

func TestResponsesAgentMessageRecoveryRejectsTampering(t *testing.T) {
	aead, err := newResponsesAgentMessageCipher("test-agent-message-installation-secret")
	require.NoError(t, err)
	state := agentMessageTestState()
	aad, err := responsesAgentMessageAAD(state, "original-identity")
	require.NoError(t, err)
	sealed, err := sealResponsesAgentMessage(aead, aad, "complete retained message")
	require.NoError(t, err)
	require.NotContains(t, sealed, "retained")
	for _, identity := range []string{"", "changed-identity"} {
		wrongAAD, err := responsesAgentMessageAAD(state, identity)
		require.NoError(t, err)
		_, err = openResponsesAgentMessage(aead, wrongAAD, sealed)
		require.Error(t, err)
	}
	for _, invalid := range []string{responsesAgentMessagePrefix, responsesAgentMessagePrefix + "!", sealed[:len(sealed)-5]} {
		_, err = openResponsesAgentMessage(aead, aad, invalid)
		require.Error(t, err)
	}
	other, err := newResponsesAgentMessageCipher("different-installation")
	require.NoError(t, err)
	_, err = openResponsesAgentMessage(other, aad, sealed)
	require.Error(t, err)
}

func TestResponsesAgentMessageRecoveryPipeline(t *testing.T) {
	for _, raw := range []bool{false, true} {
		t.Run(fmt.Sprintf("raw=%t", raw), func(t *testing.T) {
			client := enttest.NewEntClient(t, "sqlite3", fmt.Sprintf("file:agent-message-pipeline-%t?mode=memory&_fk=0", raw))
			t.Cleanup(func() { client.Close() })
			ctx := authz.WithTestBypass(ent.NewContext(t.Context(), client))
			service := biz.NewSystemService(biz.SystemServiceParams{Ent: client})
			require.NoError(t, service.SetSecretKey(ctx, "test-agent-message-installation-secret"))
			importTestAgentMessage(t, ctx, client, agentMessageTestState())
			request := rejectedReasoningPipelineRequest(t, llm.APIFormatOpenAIResponse)
			request.Body = []byte(responsesAgentMessageFixture)
			executor := &responsesReasoningPipelineExecutor{events: rejectedReasoningCompactionEvents()}
			_, result, err := runRejectedReasoningPipeline(t, ctx, request, executor, t.Name(), raw, 0,
				func(state *PersistenceState, outbound *PersistentOutboundTransformer) pipeline.Middleware {
					state.APIKey = &ent.APIKey{ID: 1, ProjectID: 1}
					return recoverResponsesAgentMessages(outbound, service)
				},
			)
			require.NoError(t, err)
			events := drainRejectedReasoningPipeline(t, result)
			require.Len(t, executor.requests, 1)
			require.Equal(t, "input_text", gjson.GetBytes(executor.requests[0].Body, "input.1.content.1.type").String())
			require.Equal(t, "opaque-unavailable-body", gjson.GetBytes(executor.requests[0].Body, "input.4.content.0.encrypted_content").String())
			require.Equal(t, "new-native-compaction", gjson.Get(events["response.completed"], "response.output.0.encrypted_content").String())
		})
	}
}
