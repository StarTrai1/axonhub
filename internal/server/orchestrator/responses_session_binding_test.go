package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/internal/ent"
	entchannel "github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

const responsesSessionInvalidatedMessage = "session binding invalidated: the model this session is bound to can no longer serve it; restart the task with a new session id"

func invalidatedSessionFailure() error {
	return &httpclient.Error{StatusCode: 409, Body: []byte(`{"error":{"message":"` + responsesSessionInvalidatedMessage + ` [trace_id=synthetic code=30013]"}}`)}
}

func TestResponsesSessionScopeSeparatesRoutesAndStableLogicalClients(t *testing.T) {
	ctx := shared.WithSessionScope(t.Context(), "owner-a")
	channel := &biz.Channel{Channel: &ent.Channel{ID: 3, Type: entchannel.TypeCodex}}
	request := &httpclient.Request{
		URL: "https://relay.example/v1/responses", APIFormat: string(llm.APIFormatOpenAIResponse),
		Headers: map[string][]string{"Authorization": {"Bearer synthetic"}, "Session-Id": {"fabricated-one"}}, Body: []byte(`{"model":"gpt-6.1-sol"}`),
	}
	client := &httpclient.Request{Body: []byte(`{"client_metadata":{"thread_id":"logical-thread"},"input":[]}`)}
	first, ok := responsesSessionBindingScope(ctx, channel, request, client)
	require.True(t, ok)
	request.Headers.Set("Session-Id", "fabricated-two")
	second, ok := responsesSessionBindingScope(ctx, channel, request, client)
	require.True(t, ok)
	require.Equal(t, first, second)
	for _, dimension := range []string{"channel", "endpoint", "credential", "model", "owner"} {
		isolated := *request
		isolated.Headers = request.Headers.Clone()
		other := &biz.Channel{Channel: &ent.Channel{ID: channel.ID, Type: channel.Type}}
		owner := ctx
		switch dimension {
		case "channel":
			other.ID++
		case "endpoint":
			isolated.URL = "https://other.example/v1/responses"
		case "credential":
			isolated.Headers.Set("Authorization", "Bearer other")
		case "model":
			isolated.Body = []byte(`{"model":"gpt-6-astra"}`)
		case "owner":
			owner = shared.WithSessionScope(ctx, "owner-b")
		}
		key, ok := responsesSessionBindingScope(owner, other, &isolated, client)
		require.True(t, ok)
		require.NotEqual(t, first, key, dimension)
	}
}

func bindingTestService(t *testing.T) *biz.SystemService {
	t.Helper()
	client := enttest.NewEntClient(t, "sqlite3", "file:binding?mode=memory&_fk=1")
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	return biz.NewSystemService(biz.SystemServiceParams{Ent: client})
}

func bindingTestMiddleware(service *biz.SystemService) func(*PersistenceState, *PersistentOutboundTransformer) pipeline.Middleware {
	return func(_ *PersistenceState, outbound *PersistentOutboundTransformer) pipeline.Middleware {
		binding := recoverResponsesSessionBinding(outbound, service)
		return &bindingTestDestination{Middleware: binding}
	}
}

type bindingTestDestination struct{ pipeline.Middleware }

func (m *bindingTestDestination) OnOutboundRawRequest(ctx context.Context, request *httpclient.Request) (*httpclient.Request, error) {
	request.URL = "https://relay.example/v1/responses"
	return m.Middleware.OnOutboundRawRequest(ctx, request)
}

func TestResponsesSessionBindingRecoveryPreservesHistory(t *testing.T) {
	for _, checkpoint := range []bool{false, true} {
		for _, inBand := range []bool{false, true} {
			t.Run(fmt.Sprintf("checkpoint=%t/in-band=%t", checkpoint, inBand), func(t *testing.T) {
				service := bindingTestService(t)
				ctx := shared.WithSessionScope(t.Context(), "owner:"+t.Name())
				request := relayAffinityRequest(t)
				if !checkpoint {
					var err error
					request.Body, err = sjson.DeleteBytes(request.Body, "input.8")
					require.NoError(t, err)
				}
				originalBody, originalHeaders := append([]byte(nil), request.Body...), request.Headers.Clone()
				executor := &agentRecoveryStreamExecutor{responsesReasoningPipelineExecutor: responsesReasoningPipelineExecutor{failures: []error{invalidatedSessionFailure()}, events: rejectedReasoningCompactionEvents()}, inBand: inBand}
				state, result, err := runRejectedReasoningPipeline(t, ctx, request, executor, "binding-credential", true, 2, bindingTestMiddleware(service))
				require.NoError(t, err)
				drainRejectedReasoningPipeline(t, result)
				require.Len(t, executor.requests, 2)
				first, retry := executor.requests[0], executor.requests[1]
				alias := retry.Headers.Get("Session-Id")
				require.NotEmpty(t, alias)
				require.NotEqual(t, first.Headers.Get("Session-Id"), alias)
				require.Equal(t, alias, retry.Headers.Get("Thread-Id"))
				require.Equal(t, alias, retry.Headers.Get("Conversation_id"))
				require.Equal(t, gjson.GetBytes(originalBody, "prompt_cache_key").String(), gjson.GetBytes(retry.Body, "prompt_cache_key").String(), "independent cache keys remain stable")
				require.Equal(t, alias+":4", retry.Headers.Get("X-Codex-Window-Id"))
				require.Empty(t, retry.Headers.Get(codexTurnStateHeader))
				for _, field := range []string{"input", "tools", "instructions", "reasoning", "model"} {
					require.Equal(t, gjson.GetBytes(first.Body, field).Raw, gjson.GetBytes(retry.Body, field).Raw, field)
				}
				require.Equal(t, originalBody, request.Body)
				require.Equal(t, originalHeaders, request.Headers)
				require.Equal(t, gjson.GetBytes(request.Body, "client_metadata").Raw, gjson.GetBytes(responsesSessionProviderBody(state), "client_metadata").Raw)
				next := &responsesReasoningPipelineExecutor{events: rejectedReasoningCompactionEvents()}
				_, result, err = runRejectedReasoningPipeline(t, ctx, request, next, "binding-credential", true, 2, bindingTestMiddleware(service))
				require.NoError(t, err)
				drainRejectedReasoningPipeline(t, result)
				require.Len(t, next.requests, 1)
				require.Equal(t, alias, next.requests[0].Headers.Get("Session-Id"))
				for _, isolation := range []string{"owner", "credential", "thread"} {
					separate := *request
					separate.Headers = request.Headers.Clone()
					owner, credential := ctx, "binding-credential"
					switch isolation {
					case "owner":
						owner = shared.WithSessionScope(ctx, "different-owner")
					case "credential":
						credential = "different-credential"
					case "thread":
						separate.Headers.Set("Thread-Id", "different-thread")
					}
					isolated := &responsesReasoningPipelineExecutor{events: rejectedReasoningCompactionEvents()}
					_, result, err = runRejectedReasoningPipeline(t, owner, &separate, isolated, credential, true, 1, bindingTestMiddleware(service))
					require.NoError(t, err)
					drainRejectedReasoningPipeline(t, result)
					require.NotEqual(t, first.Headers.Get("Session-Id"), isolated.requests[0].Headers.Get("Session-Id"), isolation)
					require.NotEqual(t, alias, isolated.requests[0].Headers.Get("Session-Id"), isolation)
				}
			})
		}
	}
}

func TestResponsesSessionBindingRecoveryHonorsBudgetAndStops(t *testing.T) {
	for _, budget := range []int{0, 1, 3} {
		t.Run(fmt.Sprint(budget), func(t *testing.T) {
			service := bindingTestService(t)
			executor := &responsesReasoningPipelineExecutor{failures: []error{invalidatedSessionFailure(), invalidatedSessionFailure(), invalidatedSessionFailure()}}
			ctx := shared.WithSessionScope(t.Context(), t.Name())
			state, result, err := runRejectedReasoningPipeline(t, ctx, relayAffinityRequest(t), executor, "credential", true, budget, bindingTestMiddleware(service))
			require.Error(t, err)
			require.Nil(t, result)
			require.Len(t, executor.requests, min(budget+1, 3))
			if len(executor.requests) == 3 {
				require.Equal(t, executor.requests[1].Headers.Get("Session-Id"), executor.requests[2].Headers.Get("Session-Id"))
				require.NotContains(t, string(executor.requests[2].Body), "preserve-native-reasoning")
				scope, ok := responsesReasoningScope(ctx, state.CurrentCandidate.Channel, executor.requests[1])
				require.True(t, ok)
				_, remembered := rememberedResponsesReasoningRule(scope, executor.requests[1].Body)
				require.False(t, remembered, "a rejected fallback cannot become a learned correction")
			}
		})
	}
}

func TestResponsesSessionBindingRecoversReasoningAfterRenewal(t *testing.T) {
	for _, checkpoint := range []bool{false, true} {
		for _, inBand := range []bool{false, true} {
			t.Run(fmt.Sprintf("checkpoint=%t/in-band=%t", checkpoint, inBand), func(t *testing.T) {
				service := bindingTestService(t)
				ctx := shared.WithSessionScope(t.Context(), t.Name())
				request := relayAffinityRequest(t)
				if checkpoint {
					var err error
					request.Body, err = sjson.SetRawBytes(request.Body, "input.8", []byte(`{"type":"compaction","id":"cmp_retained","encrypted_content":"checkpoint-must-survive"}`))
					require.NoError(t, err)
				}
				original := append([]byte(nil), request.Body...)
				executor := &agentRecoveryStreamExecutor{responsesReasoningPipelineExecutor: responsesReasoningPipelineExecutor{
					failures: []error{invalidatedSessionFailure(), invalidatedSessionFailure()}, events: rejectedReasoningCompactionEvents(),
				}, inBand: inBand}
				state, result, err := runRejectedReasoningPipeline(t, ctx, request, executor, "staged-key", true, 2, bindingTestMiddleware(service))
				require.NoError(t, err)
				drainRejectedReasoningPipeline(t, result)
				require.Len(t, executor.requests, 3)
				first, renewed, recovered := executor.requests[0], executor.requests[1], executor.requests[2]
				require.NotEqual(t, first.Headers.Get("Session-Id"), renewed.Headers.Get("Session-Id"))
				require.Equal(t, renewed.Headers.Get("Session-Id"), recovered.Headers.Get("Session-Id"))
				require.Equal(t, renewed.Headers.Get("X-Codex-Window-Id"), recovered.Headers.Get("X-Codex-Window-Id"))
				require.Equal(t, gjson.GetBytes(first.Body, "input").Raw, gjson.GetBytes(renewed.Body, "input").Raw)
				require.NotContains(t, string(recovered.Body), "preserve-native-reasoning")
				for _, canary := range []string{"visible reasoning summary", "preserve the complete task", "prior explanation", "complete function result", "complete custom input", "complete custom result", "call_function", "call_custom"} {
					require.Contains(t, string(recovered.Body), canary)
				}
				if checkpoint {
					require.Equal(t, gjson.GetBytes(original, "input.8").Raw, gjson.GetBytes(recovered.Body, `input.#(type=="compaction")`).Raw)
				}
				require.Equal(t, original, request.Body)
				scope, ok := responsesReasoningScope(ctx, state.CurrentCandidate.Channel, renewed)
				require.True(t, ok)
				t.Cleanup(func() { responsesReasoningRecoveries.Remove(scope) })
				// A newly accepted target item is never covered by the old recovery.
				nextRequest := *request
				nextRequest.Body, err = sjson.SetRawBytes(request.Body, "input.-1", []byte(`{"type":"reasoning","id":"rs_fresh","encrypted_content":"fresh-target-reasoning","summary":[]}`))
				require.NoError(t, err)
				next := &responsesReasoningPipelineExecutor{events: rejectedReasoningCompactionEvents()}
				_, result, err = runRejectedReasoningPipeline(t, ctx, &nextRequest, next, "staged-key", true, 2, bindingTestMiddleware(service))
				require.NoError(t, err)
				drainRejectedReasoningPipeline(t, result)
				require.Len(t, next.requests, 1)
				require.Equal(t, recovered.Headers.Get("Session-Id"), next.requests[0].Headers.Get("Session-Id"))
				require.NotContains(t, string(next.requests[0].Body), "preserve-native-reasoning")
				require.Contains(t, string(next.requests[0].Body), "fresh-target-reasoning")
			})
		}
	}
}

func TestResponsesSessionBindingWithoutReasoningStopsAfterRenewal(t *testing.T) {
	service := bindingTestService(t)
	request := relayAffinityRequest(t)
	var err error
	request.Body, err = sjson.DeleteBytes(request.Body, "input.2")
	require.NoError(t, err)
	executor := &responsesReasoningPipelineExecutor{failures: []error{invalidatedSessionFailure(), invalidatedSessionFailure()}}
	_, result, err := runRejectedReasoningPipeline(t, shared.WithSessionScope(t.Context(), t.Name()), request, executor, "key", true, 5, bindingTestMiddleware(service))
	require.Error(t, err)
	require.Nil(t, result)
	require.Len(t, executor.requests, 2)
}

func TestResponsesSessionBindingReasoningRecoveryRequiresCompleteHistory(t *testing.T) {
	for _, scenario := range []struct {
		name, path, value string
	}{
		{"previous response", "previous_response_id", `"resp_unresolved"`},
		{"conversation", "conversation", `"conv_unresolved"`},
		{"missing tool call", "input.5.call_id", `"call_missing"`},
		{"opaque reference", "input.8", `{"type":"item_reference","id":"item_unresolved"}`},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			service := bindingTestService(t)
			ctx := shared.WithSessionScope(t.Context(), t.Name())
			request := relayAffinityRequest(t)
			executor := &responsesReasoningPipelineExecutor{events: rejectedReasoningCompactionEvents()}
			var binding *responsesSessionBindingMiddleware
			state, result, err := runRejectedReasoningPipeline(t, ctx, request, executor, "key", true, 2,
				func(_ *PersistenceState, outbound *PersistentOutboundTransformer) pipeline.Middleware {
					binding = recoverResponsesSessionBinding(outbound, service).(*responsesSessionBindingMiddleware)
					return &bindingTestDestination{Middleware: binding}
				})
			require.NoError(t, err)
			drainRejectedReasoningPipeline(t, result)
			require.NotNil(t, binding.attempt)
			binding.renewed[binding.attempt.scope] = true
			state.RawProviderRequest.Body, err = sjson.SetRawBytes(state.RawProviderRequest.Body, scenario.path, []byte(scenario.value))
			require.NoError(t, err)
			before := append([]byte(nil), state.RawProviderRequest.Body...)
			binding.OnOutboundRawError(ctx, invalidatedSessionFailure())
			require.Zero(t, state.responsesRejectedStatusRetryChannel)
			require.Empty(t, state.responsesRejectedStatusRules)
			require.Equal(t, before, state.RawProviderRequest.Body)
		})
	}
}

func TestResponsesSessionBindingErrorRecognition(t *testing.T) {
	for _, scenario := range []struct {
		name                 string
		status               int
		code, message, param string
		want                 bool
	}{
		{"observed", 409, "", responsesSessionInvalidatedMessage + " [trace_id=synthetic code=30013]", "", true},
		{"structured", 409, "30013", responsesSessionInvalidatedMessage, "", true},
		{"wrong status", 400, "30013", responsesSessionInvalidatedMessage, "", false},
		{"message adapter", 409, "", responsesSessionInvalidatedMessage, "", true},
		{"wrong code", 409, "policy_capacity_unavailable", responsesSessionInvalidatedMessage + " [trace_id=synthetic code=30013]", "", false},
		{"unrelated conflict", 409, "30013", "request already running", "", false},
		{"unrelated message", 409, "30013", "model unavailable; start a new session", "", false},
		{"standard expiration", 410, "session_expired", "", "session_id", true},
		{"different numeric code", 409, "99999", "session expired; retry with a fresh session", "", true},
		{"authorization expiration", 401, "session_expired", "", "", false},
		{"different param", 409, "30013", responsesSessionInvalidatedMessage, "input[0]", false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			body, err := json.Marshal(map[string]any{"error": map[string]string{"code": scenario.code, "message": scenario.message, "param": scenario.param}})
			require.NoError(t, err)
			require.Equal(t, scenario.want, isResponsesSessionBindingInvalidation(&httpclient.Error{StatusCode: scenario.status, Body: body}))
			require.Equal(t, scenario.want, isResponsesSessionBindingInvalidation(&llm.ResponseError{StatusCode: scenario.status, Detail: llm.ErrorDetail{Code: scenario.code, Message: scenario.message, Param: scenario.param}}))
		})
	}
}
