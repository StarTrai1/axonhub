package orchestrator

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/internal/ent"
	entchannel "github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/oauth"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/streams"
	"github.com/looplj/axonhub/llm/transformer"
	"github.com/looplj/axonhub/llm/transformer/openai/codex"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

func TestResponsesRejectedReasoningPipelineCompactionRecovery(t *testing.T) {
	for _, scenario := range []struct {
		name        string
		errorBody   string
		passThrough bool
	}{
		{"native error with pass-through", `{"error":{"code":"invalid_encrypted_content","param":"input[2].encrypted_content"}}`, true},
		{"agent relay with pass-through", `{"error":{"type":"invalid_request_error","message":"OpenAI Responses bad request: ` + rejectedReasoningMessage + ` [trace_id=synthetic-trace]"}}`, true},
		{"native error with transformed stream", `{"error":{"code":"invalid_encrypted_content","param":"input[2].encrypted_content"}}`, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ctx := shared.WithSessionScope(t.Context(), "api_key:1:project:1")
			request := rejectedReasoningPipelineRequest(t, llm.APIFormatOpenAIResponse)
			original := append([]byte(nil), request.Body...)
			executor := &responsesReasoningPipelineExecutor{
				failures: []error{&httpclient.Error{StatusCode: 400, Body: []byte(scenario.errorBody)}},
				events:   rejectedReasoningCompactionEvents(),
			}
			state, result, err := runRejectedReasoningPipeline(t, ctx, request, executor, "target-credential", scenario.passThrough, 1)
			require.NoError(t, err)
			events := drainRejectedReasoningPipeline(t, result)
			require.Len(t, executor.requests, 2, "the rejection must recover on the same channel exactly once")
			require.JSONEq(t, gjson.GetBytes(original, "input").Raw, gjson.GetBytes(executor.requests[0].Body, "input").Raw)
			retry := executor.requests[1].Body
			require.Empty(t, gjson.GetBytes(retry, `input.#(encrypted_content)#`).Array())
			require.Equal(t, "visible summary\n\nvisible rationale", gjson.GetBytes(retry, "input.2.content.0.text").String())
			for _, path := range []string{
				"model", "reasoning", "prompt_cache_key", "input.0", "input.1", "input.3", "input.4", "input.5", "input.6",
				`input.#(type=="compaction_trigger")`,
			} {
				require.JSONEq(t, gjson.GetBytes(executor.requests[0].Body, path).Raw, gjson.GetBytes(retry, path).Raw, path)
			}
			require.Equal(t, original, request.Body)
			require.Contains(t, events, "response.completed")
			require.Equal(t, "new-native-compaction", gjson.Get(events["response.completed"], "response.output.0.encrypted_content").String())

			scope, ok := responsesReasoningScope(ctx, state.CurrentCandidate.Channel, executor.requests[0])
			require.True(t, ok)
			t.Cleanup(func() { responsesReasoningRecoveries.Remove(scope) })
			// Pass-through drains the transformed stream in the background. The
			// successful terminal must reach the compatibility observer too.
			require.Eventually(t, func() bool {
				_, found := rememberedResponsesReasoningRule(scope, original)
				return found
			}, time.Second, time.Millisecond)

			items := gjson.GetBytes(original, "input").Array()
			nextInput := make([]json.RawMessage, 0, len(items)+1)
			for _, item := range items[:len(items)-1] {
				nextInput = append(nextInput, json.RawMessage(item.Raw))
			}
			nextInput = append(nextInput,
				json.RawMessage(`{"type":"reasoning","id":"rs_new_target","encrypted_content":"new-target-reasoning","summary":[]}`),
				json.RawMessage(`{"type":"compaction_trigger"}`),
			)
			next := *request
			next.Headers = request.Headers.Clone()
			next.Body, err = sjson.SetBytes(original, "input", nextInput)
			require.NoError(t, err)
			nextExecutor := &responsesReasoningPipelineExecutor{events: rejectedReasoningCompactionEvents()}
			_, result, err = runRejectedReasoningPipeline(t, ctx, &next, nextExecutor, "target-credential", scenario.passThrough, 1)
			require.NoError(t, err)
			drainRejectedReasoningPipeline(t, result)
			require.Len(t, nextExecutor.requests, 1, "replayed old ciphertext must not cost another failed attempt")
			require.Len(t, gjson.GetBytes(nextExecutor.requests[0].Body, `input.#(encrypted_content)#`).Array(), 1)
			require.Equal(t, "new-target-reasoning", gjson.GetBytes(nextExecutor.requests[0].Body, `input.#(id=="rs_new_target").encrypted_content`).String())
			require.JSONEq(t, `{"type":"compaction_trigger"}`, gjson.GetBytes(nextExecutor.requests[0].Body, `input.#(type=="compaction_trigger")`).Raw)

			for _, isolated := range []string{"credential", "thread", "window", "owner"} {
				t.Run(isolated, func(t *testing.T) {
					separate := *request
					separate.Headers = request.Headers.Clone()
					separate.Body = original
					credential, ownerCtx := "target-credential", ctx
					switch isolated {
					case "credential":
						credential = "another-target-credential"
					case "thread":
						separate.Headers.Set("Thread-Id", "another-thread")
					case "window":
						separate.Headers.Set("X-Codex-Window-Id", "another-window")
					case "owner":
						ownerCtx = shared.WithSessionScope(t.Context(), "api_key:2:project:2")
					}
					isolatedExecutor := &responsesReasoningPipelineExecutor{events: rejectedReasoningCompactionEvents()}
					_, isolatedResult, processErr := runRejectedReasoningPipeline(t, ownerCtx, &separate, isolatedExecutor, credential, scenario.passThrough, 1)
					require.NoError(t, processErr)
					drainRejectedReasoningPipeline(t, isolatedResult)
					require.Len(t, isolatedExecutor.requests, 1)
					require.Equal(t, "rejected-with-summary", gjson.GetBytes(isolatedExecutor.requests[0].Body, "input.2.encrypted_content").String())
				})
			}
		})
	}
}

func TestResponsesRejectedReasoningPipelineStandaloneCompact(t *testing.T) {
	ctx := shared.WithSessionScope(t.Context(), "api_key:1:project:1")
	request := rejectedReasoningPipelineRequest(t, llm.APIFormatOpenAIResponseCompact)
	executor := &responsesReasoningPipelineExecutor{
		failures: []error{&httpclient.Error{StatusCode: 400, Body: []byte(`{"error":{"code":"invalid_encrypted_content"}}`)}},
		response: &httpclient.Response{
			StatusCode: http.StatusOK,
			Headers:    http.Header{"Content-Type": {"application/json"}},
			Body:       []byte(`{"id":"cmp_response","object":"response.compaction","created_at":1,"model":"gpt-6-astra","output":[{"type":"compaction","id":"cmp_output","encrypted_content":"new-native-compaction"}]}`),
		},
	}
	state, result, err := runRejectedReasoningPipeline(t, ctx, request, executor, "compact-credential", true, 1)
	require.NoError(t, err)
	require.False(t, result.Stream)
	require.Equal(t, "new-native-compaction", gjson.GetBytes(result.Response.Body, "output.0.encrypted_content").String())
	require.Len(t, executor.requests, 2)
	require.Equal(t, string(llm.APIFormatOpenAIResponseCompact), executor.requests[1].APIFormat)
	require.Empty(t, gjson.GetBytes(executor.requests[1].Body, `input.#(encrypted_content)#`).Array())
	scope, ok := responsesReasoningScope(ctx, state.CurrentCandidate.Channel, executor.requests[0])
	require.True(t, ok)
	t.Cleanup(func() { responsesReasoningRecoveries.Remove(scope) })
	_, found := rememberedResponsesReasoningRule(scope, executor.requests[0].Body)
	require.True(t, found)
}

func TestResponsesRejectedReasoningPipelineHonorsRetryBudget(t *testing.T) {
	request := rejectedReasoningPipelineRequest(t, llm.APIFormatOpenAIResponse)
	executor := &responsesReasoningPipelineExecutor{
		failures: []error{&httpclient.Error{StatusCode: 400, Body: []byte(`{"error":{"code":"invalid_encrypted_content"}}`)}},
	}
	ctx := shared.WithSessionScope(t.Context(), "api_key:1:project:1")
	state, result, err := runRejectedReasoningPipeline(t, ctx, request, executor, "no-retry-credential", true, 0)
	require.Error(t, err)
	require.Nil(t, result)
	require.Len(t, executor.requests, 1)
	scope, ok := responsesReasoningScope(ctx, state.CurrentCandidate.Channel, executor.requests[0])
	require.True(t, ok)
	_, found := rememberedResponsesReasoningRule(scope, request.Body)
	require.False(t, found)
}

func rejectedReasoningPipelineRequest(t *testing.T, format llm.APIFormat) *httpclient.Request {
	t.Helper()
	body, err := sjson.SetBytes([]byte(responsesRejectedReasoningFixture), "model", "gpt-6-astra")
	require.NoError(t, err)
	if format == llm.APIFormatOpenAIResponse {
		body, err = sjson.SetBytes(body, "stream", true)
		require.NoError(t, err)
		body, err = sjson.SetRawBytes(body, "reasoning", []byte(`{"effort":"high","context":"all_turns"}`))
		require.NoError(t, err)
		input := []json.RawMessage{json.RawMessage(`{"type":"additional_tools","tools":[]}`)}
		for _, item := range gjson.GetBytes(body, "input").Array() {
			input = append(input, json.RawMessage(item.Raw))
		}
		input = append(input, json.RawMessage(`{"type":"compaction_trigger"}`))
		body, err = sjson.SetBytes(body, "input", input)
		require.NoError(t, err)
	}
	return &httpclient.Request{
		Method:      http.MethodPost,
		URL:         "/v1/responses",
		ContentType: "application/json",
		APIFormat:   string(format),
		Headers: http.Header{
			"Content-Type":      {"application/json"},
			"Session-Id":        {"shared-session"},
			"Thread-Id":         {"thread-" + t.Name()},
			"X-Codex-Window-Id": {"thread-" + t.Name() + ":0"},
		},
		Body: body,
	}
}

func runRejectedReasoningPipeline(
	t *testing.T,
	ctx context.Context,
	request *httpclient.Request,
	executor *responsesReasoningPipelineExecutor,
	credential string,
	responsePassThrough bool,
	maxRetries int,
) (*PersistenceState, *pipeline.Result, error) {
	t.Helper()
	provider, err := codex.NewOutboundTransformer(codex.Params{
		BaseURL:       "https://chatgpt.com/backend-api/codex#",
		TokenProvider: oauth.NewStaticTokenProvider(&oauth.OAuthCredentials{AccessToken: credential}),
	})
	require.NoError(t, err)
	candidate := &ChannelModelsCandidate{
		Channel: &biz.Channel{
			Channel: &ent.Channel{
				ID: 98001, Name: "codex-recovery", Type: entchannel.TypeCodex,
				Settings: &objects.ChannelSettings{PassThroughBody: lo.ToPtr(true)},
			},
			Outbound: provider,
		},
		Models:    []biz.ChannelModelEntry{{RequestModel: "gpt-6-astra", ActualModel: "gpt-6-astra", Source: "direct"}},
		APIFormat: request.APIFormat,
	}
	state := &PersistenceState{
		OriginalModel:              "gpt-6-astra",
		ChannelModelsCandidates:    []*ChannelModelsCandidate{candidate},
		DisableResponsePassThrough: !responsePassThrough,
	}
	var wrapped transformer.Inbound = responses.NewInboundTransformer()
	if request.APIFormat == string(llm.APIFormatOpenAIResponseCompact) {
		wrapped = responses.NewCompactInboundTransformer()
	}
	inbound, outbound := NewPersistentTransformers(state, wrapped)
	pipe := pipeline.NewFactory(executor).Pipeline(
		inbound, outbound,
		pipeline.WithRetry(0, maxRetries, 0),
		pipeline.WithMiddlewares(
			applyPassThroughResponse(outbound, nil),
			applyPassThroughStream(outbound, nil),
			applyPassThroughRequestBody(outbound, nil),
			applyPassThroughRequestHeaders(outbound),
			applyResponsesRejectedStatusCompatibility(outbound),
			finalizeTransportRequest(outbound),
			captureRawProviderResponse(outbound, nil),
			captureRawProviderStream(outbound, nil),
		),
	)
	result, err := pipe.Process(ctx, request)
	return state, result, err
}

func drainRejectedReasoningPipeline(t *testing.T, result *pipeline.Result) map[string]string {
	t.Helper()
	require.NotNil(t, result)
	require.True(t, result.Stream)
	events := make(map[string]string)
	for result.EventStream.Next() {
		event := result.EventStream.Current()
		events[gjson.GetBytes(event.Data, "type").String()] = string(event.Data)
	}
	require.NoError(t, result.EventStream.Err())
	require.NoError(t, result.EventStream.Close())
	return events
}

func rejectedReasoningCompactionEvents() []*httpclient.StreamEvent {
	return []*httpclient.StreamEvent{
		{Type: "response.created", Data: []byte(`{"type":"response.created","response":{"id":"resp_compact","object":"response","status":"in_progress","model":"gpt-6-astra","output":[]}}`)},
		{Type: "response.output_item.done", Data: []byte(`{"type":"response.output_item.done","output_index":0,"item":{"type":"compaction","id":"cmp_output","encrypted_content":"new-native-compaction"}}`)},
		{Type: "response.completed", Data: []byte(`{"type":"response.completed","response":{"id":"resp_compact","object":"response","status":"completed","model":"gpt-6-astra","output":[{"type":"compaction","id":"cmp_output","encrypted_content":"new-native-compaction"}],"usage":{"input_tokens":10,"output_tokens":1,"total_tokens":11}}}`)},
	}
}

type responsesReasoningPipelineExecutor struct {
	requests []*httpclient.Request
	failures []error
	events   []*httpclient.StreamEvent
	response *httpclient.Response
}

func (e *responsesReasoningPipelineExecutor) capture(request *httpclient.Request) error {
	snapshot := *request
	snapshot.Body = append([]byte(nil), request.Body...)
	snapshot.Headers = request.Headers.Clone()
	e.requests = append(e.requests, &snapshot)
	if len(e.requests) <= len(e.failures) {
		return e.failures[len(e.requests)-1]
	}
	return nil
}

func (e *responsesReasoningPipelineExecutor) Do(_ context.Context, request *httpclient.Request) (*httpclient.Response, error) {
	if err := e.capture(request); err != nil {
		return nil, err
	}
	return e.response, nil
}

func (e *responsesReasoningPipelineExecutor) DoStream(_ context.Context, request *httpclient.Request) (streams.Stream[*httpclient.StreamEvent], error) {
	if err := e.capture(request); err != nil {
		return nil, err
	}
	return streams.SliceStream(e.events), nil
}
