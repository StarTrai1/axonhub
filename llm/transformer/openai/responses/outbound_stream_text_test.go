package responses

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
)

func TestOutboundStreamRecoversTextDone(t *testing.T) {
	delta := `{"type":"response.output_text.delta","item_id":"msg_1","output_index":0,"content_index":0,"delta":"Hello"}`
	done := `{"type":"response.output_text.done","item_id":"msg_1","output_index":0,"content_index":0,"text":"Hello 世界"}`
	for _, tc := range []struct {
		name   string
		events []string
		chunks []string
	}{
		{name: "done only", events: []string{done}, chunks: []string{"Hello 世界"}},
		{name: "missing tail", events: []string{delta, done}, chunks: []string{"Hello", " 世界"}},
		{name: "duplicate done", events: []string{delta, done, done}, chunks: []string{"Hello", " 世界"}},
		{name: "same text", events: []string{delta, `{"type":"response.output_text.done","item_id":"msg_1","text":"Hello"}`}, chunks: []string{"Hello"}},
		{name: "conflicting prefix", events: []string{delta, `{"type":"response.output_text.done","item_id":"msg_1","text":"Other answer"}`}, chunks: []string{"Hello"}},
		{name: "shorter done", events: []string{delta, `{"type":"response.output_text.done","item_id":"msg_1","text":"He"}`}, chunks: []string{"Hello"}},
		{name: "different item", events: []string{delta, `{"type":"response.output_text.done","item_id":"msg_2","text":"Hello 世界"}`}, chunks: []string{"Hello"}},
		{name: "different index", events: []string{delta, `{"type":"response.output_text.done","item_id":"msg_1","output_index":1,"content_index":0,"text":"Hello 世界"}`}, chunks: []string{"Hello"}},
		{name: "known second part", events: []string{delta, done,
			`{"type":"response.content_part.added","item_id":"msg_1","output_index":0,"content_index":1,"part":{"type":"output_text","text":""}}`,
			`{"type":"response.output_text.done","item_id":"msg_1","output_index":0,"content_index":1,"text":"!"}`,
		}, chunks: []string{"Hello", " 世界", "!"}},
		{name: "multiple streamed parts", events: []string{delta, done,
			`{"type":"response.output_text.delta","item_id":"msg_2","output_index":1,"content_index":0,"delta":"Bye"}`,
			`{"type":"response.output_text.done","item_id":"msg_2","output_index":1,"content_index":0,"text":"Bye!"}`,
		}, chunks: []string{"Hello", " 世界", "Bye", "!"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			events := []string{
				`{"type":"response.created","response":{"id":"resp_text","model":"test","status":"in_progress"}}`,
				`{"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"msg_1","phase":"final_answer","role":"assistant"}}`,
			}
			events = append(events, tc.events...)
			events = append(events, `{"type":"response.completed","response":{"id":"resp_text","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Hello 世界"}]}],"usage":{"input_tokens":10,"output_tokens":3,"total_tokens":13}}}`)
			source := make([]*httpclient.StreamEvent, 0, len(events))
			for _, event := range events {
				source = append(source, &httpclient.StreamEvent{Data: []byte(event)})
			}
			stream := newResponsesOutboundStream(streams.SliceStream(source))
			defer stream.Close()
			results, err := streams.All(stream)
			require.NoError(t, err)
			var chunks []string
			for _, result := range results {
				for _, choice := range result.Choices {
					if choice.Delta != nil && choice.Delta.Content.Content != nil {
						chunks = append(chunks, *choice.Delta.Content.Content)
						require.Equal(t, "final_answer", lo.FromPtr(choice.Delta.Phase))
					}
				}
			}
			require.Equal(t, tc.chunks, chunks)
			require.Contains(t, results, llm.DoneResponse)
			require.True(t, stream.hasGeneratedOutput())
		})
	}
}

func TestOutboundStreamRecoversTerminalText(t *testing.T) {
	for _, tc := range []struct {
		name       string
		eventType  string
		status     string
		extra      string
		wantText   bool
		wantError  bool
		wantFinish string
	}{
		{name: "completed", eventType: "response.completed", status: "completed", wantText: true, wantFinish: "stop"},
		{name: "incomplete", eventType: "response.incomplete", status: "incomplete", wantText: true, wantFinish: "length"},
		{name: "completed but truncated", eventType: "response.completed", status: "incomplete", wantText: true, wantFinish: "length"},
		{name: "content filter", eventType: "response.incomplete", status: "incomplete", extra: `,"incomplete_details":{"reason":"content_filter"}`, wantText: true, wantFinish: "content_filter"},
		{name: "failed status", eventType: "response.completed", status: "failed", wantFinish: "error"},
		{name: "cancelled status", eventType: "response.completed", status: "cancelled", wantFinish: "cancelled"},
		{name: "explicit error", eventType: "response.completed", status: "completed", extra: `,"error":{"code":"server_error","message":"upstream failed"}`, wantError: true},
		{name: "failed event", eventType: "response.failed", status: "failed", extra: `,"error":{"code":"server_error","message":"upstream failed"}`, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			terminal := fmt.Sprintf(`{"type":%q,"response":{"id":"resp_terminal","model":"test","status":%q,"output":[{"type":"message","role":"assistant","phase":"commentary","content":[{"type":"output_text","text":"First."},{"type":"output_text","text":"Second."}]},{"type":"message","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"Answer."}]},{"type":"message","role":"user","content":[{"type":"output_text","text":"Not assistant text"}]}],"usage":{"input_tokens":10,"output_tokens":3,"total_tokens":13}%s}}`, tc.eventType, tc.status, tc.extra)
			require.True(t, json.Valid([]byte(terminal)))
			source := []*httpclient.StreamEvent{
				{Data: []byte(`{"type":"response.created","response":{"id":"resp_terminal","model":"test","status":"in_progress"}}`)},
				{Data: []byte(terminal)},
				{Data: []byte(`{"type":"response.output_text.done","text":"Late text"}`)},
			}
			stream := newResponsesOutboundStream(streams.SliceStream(source))
			defer stream.Close()
			var texts, phases, finishes []string
			usageSeen := false
			for stream.Next() {
				result := stream.Current()
				for _, choice := range result.Choices {
					if choice.Delta != nil && choice.Delta.Content.Content != nil {
						require.Empty(t, finishes, "recovered content must precede the terminal chunk")
						texts = append(texts, *choice.Delta.Content.Content)
						phases = append(phases, lo.FromPtr(choice.Delta.Phase))
					}
					if choice.FinishReason != nil {
						finishes = append(finishes, *choice.FinishReason)
					}
				}
				if result.Usage != nil {
					require.Equal(t, []string{tc.wantFinish}, finishes)
					require.EqualValues(t, 13, result.Usage.TotalTokens)
					usageSeen = true
				}
			}
			if tc.wantError {
				require.Error(t, stream.Err())
				require.Empty(t, finishes)
			} else {
				require.NoError(t, stream.Err())
				require.Equal(t, []string{tc.wantFinish}, finishes)
				require.True(t, usageSeen)
			}
			if tc.wantText {
				require.Equal(t, []string{"First.", "Second.", "Answer."}, texts)
				require.Equal(t, []string{"commentary", "commentary", "final_answer"}, phases)
			} else {
				require.Empty(t, texts)
			}
		})
	}
}
