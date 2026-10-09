package orchestrator

import (
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
)

const portableAgentToolFixture = `{"model":"gpt-6-astra","stream":true,"input":[{"type":"additional_tools","role":"developer","tools":[{"type":"namespace","name":"agents","description":"agents","tools":[{"type":"function","name":"send_message","parameters":{"type":"object","properties":{"message":{"type":"string","encrypted":true},"target":{"type":"string"}}}}]}]},{"role":"user","content":"test"}]}`

func portableAgentEvents(t *testing.T) []*httpclient.StreamEvent {
	t.Helper()
	arguments := `{"target":"/root","message":"Exact portable agent message.\nSecond line."}`
	call := map[string]any{"type": "function_call", "id": "fc_portable", "call_id": "call_portable", "name": "send_message", "namespace": "agents", "arguments": arguments}
	added := map[string]any{"type": "function_call", "id": "fc_portable", "call_id": "call_portable", "name": "send_message", "namespace": "agents", "arguments": ""}
	values := []map[string]any{
		{"type": "response.created", "response": map[string]any{"id": "resp_portable", "object": "response", "status": "in_progress", "model": "gpt-6-astra", "output": []any{}}},
		{"type": "response.output_item.added", "output_index": 0, "item": added},
		{"type": "response.function_call_arguments.delta", "item_id": "fc_portable", "output_index": 0, "delta": arguments[:30]},
		{"type": "response.function_call_arguments.delta", "item_id": "fc_portable", "output_index": 0, "delta": arguments[30:]},
		{"type": "response.function_call_arguments.done", "item_id": "fc_portable", "output_index": 0, "arguments": arguments},
		{"type": "response.output_item.done", "output_index": 0, "item": call},
		{"type": "response.completed", "response": map[string]any{"id": "resp_portable", "object": "response", "status": "completed", "model": "gpt-6-astra", "output": []any{call}, "usage": map[string]int{"input_tokens": 10, "output_tokens": 10, "total_tokens": 20}}},
	}
	var events []*httpclient.StreamEvent
	for i, value := range values {
		value["sequence_number"] = i
		body, err := json.Marshal(value)
		require.NoError(t, err)
		events = append(events, &httpclient.StreamEvent{Type: value["type"].(string), Data: body})
	}
	return events
}

func TestResponsesAgentTransportAcrossChannels(t *testing.T) {
	for _, raw := range []bool{false, true} {
		t.Run(fmt.Sprintf("raw=%t", raw), func(t *testing.T) {
			client := enttest.NewEntClient(t, "sqlite3", fmt.Sprintf("file:portable-agent-%t?mode=memory&_fk=0", raw))
			t.Cleanup(func() { client.Close() })
			ctx := authz.WithTestBypass(ent.NewContext(t.Context(), client))
			service := biz.NewSystemService(biz.SystemServiceParams{Ent: client})
			require.NoError(t, service.SetSecretKey(ctx, "test-agent-message-installation-secret"))
			request := rejectedReasoningPipelineRequest(t, llm.APIFormatOpenAIResponse)
			request.Body = []byte(portableAgentToolFixture)
			executor := &responsesReasoningPipelineExecutor{events: portableAgentEvents(t)}
			_, result, err := runRejectedReasoningPipeline(t, ctx, request, executor, t.Name(), raw, 0,
				func(state *PersistenceState, outbound *PersistentOutboundTransformer) pipeline.Middleware {
					state.APIKey = &ent.APIKey{ID: 1, ProjectID: 1}
					return portableResponsesAgentTransport(outbound, service)
				},
			)
			require.NoError(t, err)
			require.Len(t, executor.requests, 1)
			require.False(t, gjson.GetBytes(executor.requests[0].Body, "input.0.tools.0.tools.0.parameters.properties.message.encrypted").Bool())
			events := make(map[string]string)
			for result.EventStream.Next() {
				event := result.EventStream.Current()
				require.NotContains(t, string(event.Data), "Exact portable agent message", "plaintext must not leak through argument deltas or completion snapshots")
				events[gjson.GetBytes(event.Data, "type").String()] = string(event.Data)
			}
			require.NoError(t, result.EventStream.Err())
			require.NoError(t, result.EventStream.Close())
			call := gjson.Get(events["response.output_item.done"], "item")
			arguments := call.Get("arguments").String()
			sealed := gjson.Get(arguments, "message").String()
			require.Contains(t, sealed, responsesAgentMessagePrefix)
			require.Equal(t, arguments, gjson.Get(events["response.function_call_arguments.done"], "arguments").String())
			require.Equal(t, arguments, gjson.Get(events["response.completed"], "response.output.0.arguments").String())

			// Model the receiving agent and the sender's next history, including
			// client delivery prefixes and tool output linkage. A new service and
			// destination must reopen the same exact content without stored rows.
			receiverBody, err := json.Marshal(map[string]any{"model": "gpt-6-astra", "input": []any{
				map[string]any{"type": "agent_message", "id": "amsg_receiver", "author": "/root/reviewer", "recipient": "/root", "content": []any{
					map[string]string{"type": "input_text", "text": "original delivery prefix\n"},
					map[string]string{"type": "encrypted_content", "encrypted_content": sealed},
				}},
				json.RawMessage(call.Raw),
				map[string]string{"type": "function_call_output", "call_id": call.Get("call_id").String(), "output": "queued"},
			}})
			require.NoError(t, err)
			state := agentMessageTestState()
			state.CurrentCandidate.Channel.ID = 99
			restarted := biz.NewSystemService(biz.SystemServiceParams{Ent: client})
			receiver := &httpclient.Request{APIFormat: string(llm.APIFormatOpenAIResponse), Body: receiverBody, Headers: make(http.Header)}
			restored, err := recoverResponsesAgentMessages(&PersistentOutboundTransformer{state: state}, restarted).OnOutboundRawRequest(ctx, receiver)
			require.NoError(t, err)
			require.Equal(t, "Exact portable agent message.\nSecond line.", gjson.GetBytes(restored.Body, "input.0.content.1.text").String())
			require.Equal(t, "original delivery prefix\n", gjson.GetBytes(restored.Body, "input.0.content.0.text").String())
			require.Equal(t, "Exact portable agent message.\nSecond line.", gjson.Get(gjson.GetBytes(restored.Body, "input.1.arguments").String(), "message").String())
			require.Equal(t, call.Get("call_id").String(), gjson.GetBytes(restored.Body, "input.2.call_id").String())
			require.False(t, gjson.GetBytes(restored.Body, "input.1.encrypted_function_args").Exists())
			count, err := client.System.Query().Count(ctx)
			require.NoError(t, err)
			require.Equal(t, 2, count, "only the installation secret and transport policy are stored; new messages carry their own encrypted content")
			state.APIKey = &ent.APIKey{ID: 2, ProjectID: 1}
			_, err = recoverResponsesAgentMessages(&PersistentOutboundTransformer{state: state}, restarted).OnOutboundRawRequest(ctx, receiver)
			require.Error(t, err)
		})
	}
}

func TestResponsesAgentTransportPolicySurvivesNativeCheckpoint(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:portable-agent-checkpoint?mode=memory&_fk=0")
	t.Cleanup(func() { client.Close() })
	ctx := authz.WithTestBypass(ent.NewContext(t.Context(), client))
	service := biz.NewSystemService(biz.SystemServiceParams{Ent: client})
	require.NoError(t, service.SetSecretKey(ctx, "test-agent-message-installation-secret"))
	state := agentMessageTestState()
	request := &httpclient.Request{URL: "https://relay.example/responses/compact", APIFormat: string(llm.APIFormatOpenAIResponseCompact), Body: []byte(portableAgentToolFixture), Headers: make(http.Header)}
	request.Headers.Set("Authorization", "Bearer test-owner")
	request.Headers.Set("Thread-Id", "same-upstream-thread")
	request.Headers.Set("Session-Id", "same-upstream-session")
	first := portableResponsesAgentTransport(&PersistentOutboundTransformer{state: state}, service).(*responsesAgentTransport)
	_, err := first.OnOutboundRawRequest(ctx, request)
	require.NoError(t, err)
	_, err = first.OnOutboundRawResponse(ctx, &httpclient.Response{StatusCode: 200, Body: []byte(`{"object":"response.compaction","output":[{"type":"compaction","id":"cmp_native","encrypted_content":"native-checkpoint"}]}`)})
	require.NoError(t, err)
	second := portableResponsesAgentTransport(&PersistentOutboundTransformer{state: state}, biz.NewSystemService(biz.SystemServiceParams{Ent: client})).(*responsesAgentTransport)
	request.URL = "https://relay.example/responses"
	request.APIFormat = string(llm.APIFormatOpenAIResponse)
	request.Body = []byte(`{"model":"gpt-6-astra","input":[{"type":"compaction","id":"cmp_native","encrypted_content":"native-checkpoint"},{"role":"user","content":"continue"}]}`)
	got, err := second.OnOutboundRawRequest(ctx, request)
	require.NoError(t, err)
	require.True(t, second.tools["agents.send_message"])
	require.Equal(t, request.Body, got.Body)
	require.Equal(t, request.Headers, got.Headers, "restoring tool transport must keep the same upstream session")
	for _, kind := range []string{"credential", "owner", "destination"} {
		changed := *request
		changed.Headers = request.Headers.Clone()
		changedState := *state
		switch kind {
		case "credential":
			changed.Headers.Set("Authorization", "Bearer another-owner")
		case "owner":
			changedState.APIKey = &ent.APIKey{ID: 2, ProjectID: 1}
		case "destination":
			changed.URL = "https://other.example/responses"
		}
		isolated := portableResponsesAgentTransport(&PersistentOutboundTransformer{state: &changedState}, service).(*responsesAgentTransport)
		_, err := isolated.OnOutboundRawRequest(ctx, &changed)
		require.NoError(t, err)
		require.Empty(t, isolated.tools, kind)
	}
}

func TestResponsesAgentTransportScopeAndRetry(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:portable-agent-scope?mode=memory&_fk=0")
	t.Cleanup(func() { client.Close() })
	ctx := authz.WithTestBypass(ent.NewContext(t.Context(), client))
	service := biz.NewSystemService(biz.SystemServiceParams{Ent: client})
	require.NoError(t, service.SetSecretKey(ctx, "test-agent-message-installation-secret"))
	state := agentMessageTestState()
	middleware := portableResponsesAgentTransport(&PersistentOutboundTransformer{state: state}, service).(*responsesAgentTransport)
	request := &httpclient.Request{APIFormat: string(llm.APIFormatOpenAIResponse), Body: []byte(portableAgentToolFixture)}
	changed, err := middleware.OnOutboundRawRequest(ctx, request)
	require.NoError(t, err)
	require.True(t, middleware.tools["agents.send_message"])
	_, err = middleware.OnOutboundRawRequest(ctx, changed)
	require.NoError(t, err)
	require.True(t, middleware.tools["agents.send_message"], "same-channel retries must still seal their output")
	for _, path := range []string{"input.0.tools.0.name", "input.0.tools.0.tools.0.name"} {
		other := *request
		other.Body, err = sjson.SetBytes(other.Body, path, "ordinary_tool")
		require.NoError(t, err)
		got, err := middleware.OnOutboundRawRequest(ctx, &other)
		require.NoError(t, err)
		require.Same(t, &other, got)
		require.Empty(t, middleware.tools)
	}
}
