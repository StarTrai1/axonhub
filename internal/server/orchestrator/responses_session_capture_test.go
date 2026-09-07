package orchestrator

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

func TestResponsesSessionPublishesNativeHistoryBeforePassThroughTerminal(t *testing.T) {
	for _, synthesized := range []bool{false, true} {
		name := "provider_terminal"
		if synthesized {
			name = "synthesized_terminal"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(shared.WithSessionScope(context.Background(), "owner"), 3*time.Second)
			defer cancel()
			store := newResponsesSessionStore(func(context.Context, string) ([]byte, []byte, bool, error) {
				t.Fatal("continuation must not race unfinished database persistence")
				return nil, nil, false, nil
			})
			outbound := newCodexResponsesPassThroughOutbound()
			outbound.state.responsesSessions = store
			outbound.state.RawProviderRequest.Body = []byte(`{"model":"gpt-6-astra","input":[{"role":"user","content":"original history"}]}`)
			events := []*httpclient.StreamEvent{
				{Type: "response.created", Data: json.RawMessage(`{"type":"response.created","response":{"id":"resp_native","model":"gpt-6-astra","status":"in_progress","output":[]}}`)},
				{Type: "response.output_item.done", Data: json.RawMessage(`{"type":"response.output_item.done","output_index":0,"item":{"type":"reasoning","id":"rs_native","summary":[],"encrypted_content":"preserve-ciphertext"}}`)},
				{Type: "response.output_item.done", Data: json.RawMessage(`{"type":"response.output_item.done","output_index":1,"item":{"type":"function_call","id":"fc_native","call_id":"call_native","name":"exec","arguments":"{}","status":"completed"}}`)},
			}
			var source streams.Stream[*httpclient.StreamEvent]
			if synthesized {
				source = newEventsThenBlockingStream(events)
			} else {
				events = append(events, &httpclient.StreamEvent{Type: "response.completed", Data: json.RawMessage(`{"type":"response.completed","response":{"id":"resp_native","model":"gpt-6-astra","status":"completed","output":[{"type":"reasoning","id":"rs_native","summary":[],"encrypted_content":"preserve-ciphertext"},{"type":"function_call","id":"fc_native","call_id":"call_native","name":"exec","arguments":"{}","status":"completed"}]}}`)})
				source = testHTTPStream(events)
			}
			defer source.Close()
			pipelineStream, err := captureRawProviderStreamWithTerminalGrace(outbound, nil, 5*time.Millisecond).
				OnOutboundRawStream(ctx, source)
			require.NoError(t, err)
			defer pipelineStream.Close()

			terminal := false
			for !terminal {
				select {
				case event, open := <-outbound.state.RawStreamCh:
					require.True(t, open, "terminal event must reach the pass-through consumer")
					terminal = event.Type == "response.completed"
				case <-ctx.Done():
					t.Fatal("pass-through terminal was not published")
				}
			}

			prepared, _ := store.prepare(ctx, []byte(`{"previous_response_id":"resp_native","input":[{"type":"function_call_output","call_id":"call_native","output":"done"}]}`))
			require.False(t, gjson.GetBytes(prepared, "previous_response_id").Exists())
			require.Equal(t, int64(4), gjson.GetBytes(prepared, "input.#").Int())
			require.Equal(t, "original history", gjson.GetBytes(prepared, "input.0.content").String())
			require.Equal(t, "preserve-ciphertext", gjson.GetBytes(prepared, "input.1.encrypted_content").String())
			require.Equal(t, "fc_native", gjson.GetBytes(prepared, "input.2.id").String())
			require.Equal(t, "call_native", gjson.GetBytes(prepared, "input.3.call_id").String())
			require.Nil(t, store.lookup(shared.WithSessionScope(ctx, "other-owner"), "resp_native"))
			_, err = streams.All(pipelineStream)
			require.NoError(t, err)
		})
	}
}
