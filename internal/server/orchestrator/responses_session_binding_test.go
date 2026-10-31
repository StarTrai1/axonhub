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
	request := &httpclient.Request{URL: "https://relay.example/v1/responses", APIFormat: string(llm.APIFormatOpenAIResponse),
		Headers: map[string][]string{"Authorization": {"Bearer synthetic"}, "Session-Id": {"fabricated-one"}}, Body: []byte(`{"model":"gpt-6.1-sol"}`)}
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
			_, result, err := runRejectedReasoningPipeline(t, shared.WithSessionScope(t.Context(), "owner"), relayAffinityRequest(t), executor, "credential", true, budget, bindingTestMiddleware(service))
			require.Error(t, err)
			require.Nil(t, result)
			require.Len(t, executor.requests, min(budget+1, 2))
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
