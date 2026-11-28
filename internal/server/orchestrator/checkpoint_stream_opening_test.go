package orchestrator

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/internal/ent/requestexecution"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
)

func checkpointOpeningErrorEvent(kind string) *httpclient.StreamEvent {
	detail := `{"type":"invalid_request_error","code":"invalid_request_unclassified","message":"` + rejectedUnclassifiedCiphertextMessage + `"}`
	if kind == "response.failed" {
		return &httpclient.StreamEvent{Type: kind, Data: []byte(`{"type":"response.failed","status":400,"response":{"status":"failed","output":[],"error":` + detail + `}}`)}
	}
	return &httpclient.StreamEvent{Type: "error", Data: []byte(`{"type":"error","status":400,"error":` + detail + `}`)}
}

type checkpointOpeningExecutor struct {
	localCompactionRetryExecutor
	kind    string
	partial bool
}

func (e *checkpointOpeningExecutor) DoStream(ctx context.Context, req *httpclient.Request) (streams.Stream[*httpclient.StreamEvent], error) {
	if len(e.bodies) != 1 {
		return e.localCompactionRetryExecutor.DoStream(ctx, req)
	}
	e.bodies = append(e.bodies, append([]byte(nil), req.Body...))
	events := []*httpclient.StreamEvent{{Type: "response.created", Data: []byte(`{"type":"response.created","response":{"id":"resp_rejected","status":"in_progress","output":[]}}`)}}
	if e.partial {
		events = append(events, &httpclient.StreamEvent{Type: "response.output_text.delta", Data: []byte(`{"type":"response.output_text.delta","delta":"partial summary"}`)})
	}
	events = append(events, checkpointOpeningErrorEvent(e.kind))
	return streams.SliceStream(events), nil
}

func TestCheckpointStreamOpeningSharesCompatibilityBudget(t *testing.T) {
	for _, kind := range []string{"error", "response.failed"} {
		for _, scenario := range []struct {
			name    string
			budget  int
			partial bool
			want    int
		}{
			{name: "metadata then encrypted SSE", budget: 2, want: 3},
			{name: "one shared retry", budget: 1, want: 2},
			{name: "disabled retry", budget: 0, want: 1},
			{name: "partial summary is never retried", budget: 2, partial: true, want: 2},
		} {
			t.Run(kind+"/"+scenario.name, func(t *testing.T) {
				outbound := newCodexResponsesPassThroughOutbound()
				outbound.state.Settings = &RequestSettings{RetryPolicy: biz.RetryPolicy{Enabled: scenario.budget > 0, MaxSingleChannelRetries: scenario.budget}}
				req := outbound.state.RawProviderRequest
				req.Body = []byte(responsesRejectedReasoningFixture)
				original := append([]byte(nil), req.Body...)
				executor := &checkpointOpeningExecutor{kind: kind, partial: scenario.partial}
				executor.failure = &httpclient.Error{StatusCode: http.StatusBadRequest, Body: []byte(`{"error":{"code":"unknown_parameter","param":"input[0].internal_chat_message_metadata_passthrough.content_token_count"}}`)}
				executor.events = []*httpclient.StreamEvent{{Type: "response.completed", Data: []byte(`{"type":"response.completed","response":{"status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"completed summary"}]}]}}`)}}
				adapter := newRemoteCompactionAdapter(nil, nil, nil)
				stream, _, err := adapter.startLocalCompactionStream(t.Context(), outbound, req, executor, applyResponsesRejectedStatusCompatibility(outbound))
				if err == nil {
					_, err = collectLocalCompactionBridgeStream(stream, nil)
					require.NoError(t, stream.Close())
				}
				require.Len(t, executor.bodies, scenario.want)
				require.Equal(t, original, executor.bodies[0])
				if scenario.want != 3 {
					require.Error(t, err)
					return
				}
				require.NoError(t, err)
				require.NotEmpty(t, encryptedResponsesReasoningHashes(executor.bodies[1]))
				require.Empty(t, encryptedResponsesReasoningHashes(executor.bodies[2]))
				for _, value := range []string{"visible summary", "visible rationale", "keep function output", "keep custom output", "keep-cache-key"} {
					require.Contains(t, string(executor.bodies[2]), value)
				}
			})
		}
	}
}

type checkpointAncestorStreamExecutor struct {
	rejectedCompactionBridgeExecutor
	bodies         [][]byte
	kind           string
	rejectAncestor bool
}

func (e *checkpointAncestorStreamExecutor) DoStream(ctx context.Context, req *httpclient.Request) (streams.Stream[*httpclient.StreamEvent], error) {
	e.bodies = append(e.bodies, append([]byte(nil), req.Body...))
	if gjson.GetBytes(req.Body, "input.0.internal_chat_message_metadata_passthrough").Exists() {
		return nil, &httpclient.Error{StatusCode: 400, Body: []byte(`{"error":{"code":"invalid_request_unclassified","message":"Unknown parameter: 'input[0].internal_chat_message_metadata_passthrough.content_token_count'. [trace_id=synthetic code=30000]"}}`)}
	}
	if len(encryptedResponsesReasoningHashes(req.Body)) > 0 || e.rejectAncestor && gjson.GetBytes(req.Body, `input.#(id=="cmp_ancestor")`).Exists() {
		return streams.SliceStream([]*httpclient.StreamEvent{
			{Type: "response.created", Data: []byte(`{"type":"response.created","response":{"status":"in_progress","output":[]}}`)},
			checkpointOpeningErrorEvent(e.kind),
		}), nil
	}
	return e.rejectedCompactionBridgeExecutor.DoStream(ctx, req)
}

// Reproduce #108021: metadata rejection, then an in-band encryption rejection
// while summarizing a retained source which itself contains a native ancestor.
func TestCheckpointStreamOpeningRestoresNestedSource(t *testing.T) {
	for _, kind := range []string{"error", "response.failed"} {
		for _, raw := range []bool{true, false} {
			for _, rejectAncestor := range []bool{true, false} {
				t.Run(fmt.Sprintf("%s/raw=%t/reject-ancestor=%t", kind, raw, rejectAncestor), func(t *testing.T) {
					ctx, client, state, adapter := unifiedHistoryFixture(t)
					state.CurrentCandidate.Channel.Settings.PassThroughBody = lo.ToPtr(raw)
					state.Settings = &RequestSettings{RetryPolicy: biz.RetryPolicy{Enabled: true, MaxSingleChannelRetries: 3}}
					latest := &remoteCompactionReference{ID: "cmp_latest", EncryptedContent: "latest-cipher"}
					ancestor := &remoteCompactionReference{ID: "cmp_ancestor", EncryptedContent: "ancestor-cipher"}
					first := []byte(`{"model":"gpt-6-astra","client_metadata":{"thread_id":"recovery-thread"},"input":[{"type":"message","role":"user","content":"retained detail","internal_chat_message_metadata_passthrough":{"content_token_count":5}},{"role":"assistant","content":"complete ancestor detail"},{"type":"compaction_trigger"}]}`)
					current := []byte(`{"model":"gpt-6-astra","prompt_cache_key":"preserve-summary-cache","client_metadata":{"thread_id":"recovery-thread"},"input":[{"type":"message","role":"user","content":"retained detail","internal_chat_message_metadata_passthrough":{"content_token_count":5}},{"type":"compaction","id":"cmp_ancestor","encrypted_content":"ancestor-cipher"},{"type":"reasoning","id":"rs_source","encrypted_content":"source-cipher","summary":[{"type":"summary_text","text":"readable source reasoning"}]},{"type":"custom_tool_call","call_id":"call_source","name":"inspect","input":"exact input 17.125"},{"type":"custom_tool_call_output","call_id":"call_source","output":"exact result 17.250"},{"type":"compaction_trigger"}]}`)
					require.NoError(t, adapter.retainNativeCompactionSource(ctx, ancestor, state, &remoteCompactionSource{body: first}))
					require.NoError(t, adapter.retainNativeCompactionSource(ctx, latest, state, &remoteCompactionSource{body: current}))
					executor := &checkpointAncestorStreamExecutor{kind: kind, rejectAncestor: rejectAncestor}
					summary, err := adapter.summaryForCompaction(ctx, remoteCompactionCacheKey(latest), latest, "recovery-thread", "gpt-6-astra", state, executor)
					require.NoError(t, err)
					require.Equal(t, "verified retained source summary", summary)
					require.Len(t, executor.summaries, 1)
					last := executor.summaries[0]
					for _, value := range []string{"readable source reasoning", "exact input 17.125", "exact result 17.250", "preserve-summary-cache"} {
						require.Contains(t, string(last), value)
					}
					require.Equal(t, !rejectAncestor, gjson.GetBytes(last, `input.#(id=="cmp_ancestor")`).Exists())
					if rejectAncestor {
						require.Contains(t, string(last), "complete ancestor detail")
					}
					records, err := client.RequestExecution.Query().Order(requestexecution.ByID()).All(ctx)
					require.NoError(t, err)
					require.Len(t, records, len(executor.bodies))
					for _, record := range records[:len(records)-1] {
						require.Equal(t, requestexecution.StatusFailed, record.Status)
						require.NotEmpty(t, record.ErrorMessage)
					}
					require.Equal(t, requestexecution.StatusCompleted, records[len(records)-1].Status)
					stored, err := adapter.loadNativeCompactionSource(ctx, latest, state)
					require.NoError(t, err)
					require.JSONEq(t, string(current), string(stored.body))
					before := len(executor.bodies)
					adapter = newRemoteCompactionAdapter(adapter.requestService, nil, adapter.systemService)
					summary, err = adapter.summaryForCompaction(ctx, remoteCompactionCacheKey(latest), latest, "recovery-thread", "gpt-6-astra", state, executor)
					require.NoError(t, err)
					require.NotEmpty(t, summary)
					require.Len(t, executor.bodies, before, "reuse durable summary after restart")
				})
			}
		}
	}
}

func checkpointErrorWithOutput(t *testing.T, event *httpclient.StreamEvent) *httpclient.StreamEvent {
	t.Helper()
	data, err := sjson.SetRawBytes(event.Data, "response.output", []byte(`[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"partial"}]}]`))
	require.NoError(t, err)
	return &httpclient.StreamEvent{Type: event.Type, Data: data}
}

func TestCheckpointStreamOpeningPreservesCommittedEvents(t *testing.T) {
	for _, first := range []*httpclient.StreamEvent{
		{Type: "response.output_text.delta", Data: []byte(`{"type":"response.output_text.delta","delta":"partial summary"}`)},
		{Type: "response.reasoning_summary_text.delta", Data: []byte(`{"type":"response.reasoning_summary_text.delta","delta":"partial reasoning"}`)},
		{Type: "response.output_item.added", Data: []byte(`{"type":"response.output_item.added","item":{"type":"function_call","name":"exec","arguments":"{}"}}`)},
		{Type: "future.event", Data: []byte(`{"type":"future.event","state":"opaque"}`)},
		checkpointErrorWithOutput(t, checkpointOpeningErrorEvent("response.failed")),
		{Type: "response.in_progress", Data: []byte(`{"type":"response.in_progress","response":{"usage":{"output_tokens":1}}}`)},
	} {
		t.Run(first.Type, func(t *testing.T) {
			outbound := newCodexResponsesPassThroughOutbound()
			outbound.state.Settings = &RequestSettings{RetryPolicy: biz.RetryPolicy{Enabled: true, MaxSingleChannelRetries: 3}}
			outbound.state.RawProviderRequest.Body = []byte(responsesRejectedReasoningFixture)
			events := []*httpclient.StreamEvent{
				{Type: "response.created", Data: []byte(`{"type":"response.created","response":{"output":[]}}`)}, first, checkpointOpeningErrorEvent("error"),
			}
			executor := &localCompactionRetryExecutor{events: events}
			stream, _, err := newRemoteCompactionAdapter(nil, nil, nil).startLocalCompactionStream(t.Context(), outbound, outbound.state.RawProviderRequest, executor, applyResponsesRejectedStatusCompatibility(outbound))
			require.NoError(t, err)
			var got []*httpclient.StreamEvent
			for stream.Next() {
				got = append(got, stream.Current())
			}
			require.NoError(t, stream.Close())
			require.Equal(t, events, got, "pre-reading must preserve event order and payloads")
			require.Len(t, executor.bodies, 1)
		})
	}
}

func TestCheckpointStreamOpeningFailureBounds(t *testing.T) {
	for _, scenario := range []string{"ordinary refusal", "repeated encryption", "event limit", "byte limit", "canceled"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			outbound := newCodexResponsesPassThroughOutbound()
			outbound.state.Settings = &RequestSettings{RetryPolicy: biz.RetryPolicy{Enabled: true, MaxSingleChannelRetries: 2}}
			outbound.state.RawProviderRequest.Body = []byte(responsesRejectedReasoningFixture)
			executor := &localCompactionRetryExecutor{events: []*httpclient.StreamEvent{checkpointOpeningErrorEvent("error")}}
			want := 1
			switch scenario {
			case "ordinary refusal":
				executor.events = []*httpclient.StreamEvent{{Type: "error", Data: []byte(`{"type":"error","status":400,"error":{"code":"content_blocked","message":"content-blocked"}}`)}}
			case "repeated encryption":
				want = 2
			case "event limit":
				executor.events = nil
				for range 129 {
					executor.events = append(executor.events, &httpclient.StreamEvent{Type: "response.in_progress", Data: []byte(`{"type":"response.in_progress","response":{"output":[]}}`)})
				}
			case "byte limit":
				executor.events = []*httpclient.StreamEvent{{Type: "response.created", Data: []byte(`{"type":"response.created","padding":"` + strings.Repeat("x", 1<<20) + `"}`)}}
			case "canceled":
				want = 0
				cancel()
			}
			stream, _, err := newRemoteCompactionAdapter(nil, nil, nil).startLocalCompactionStream(ctx, outbound, outbound.state.RawProviderRequest, executor, applyResponsesRejectedStatusCompatibility(outbound))
			require.Error(t, err)
			require.Nil(t, stream)
			require.Len(t, executor.bodies, want)
		})
	}
}

func TestCheckpointStreamOpeningStillRequiresCompletedSummary(t *testing.T) {
	for _, events := range [][]*httpclient.StreamEvent{
		{{Type: "response.created", Data: []byte(`{"type":"response.created","response":{"output":[]}}`)}},
		{&llm.DoneStreamEvent},
	} {
		outbound := newCodexResponsesPassThroughOutbound()
		executor := &localCompactionRetryExecutor{events: events}
		stream, _, err := newRemoteCompactionAdapter(nil, nil, nil).startLocalCompactionStream(t.Context(), outbound, outbound.state.RawProviderRequest, executor)
		require.NoError(t, err)
		_, err = collectLocalCompactionBridgeStream(stream, nil)
		require.ErrorIs(t, err, llm.ErrStreamIncomplete)
		require.NoError(t, stream.Close())
		require.Len(t, executor.bodies, 1)
	}
}
