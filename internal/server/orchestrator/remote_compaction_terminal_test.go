package orchestrator

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
)

func TestRemoteCompactionRequiresSuccessfulTerminal(t *testing.T) {
	for _, tc := range []struct{ name, terminal, want string }{
		{"clean EOF", "", "terminal event"},
		{"done marker", "[DONE]", "terminal event"},
		{"unfinished completion", `{"type":"response.completed","response":{"status":"in_progress"}}`, "terminal event"},
		{"standalone error", `{"type":"error","status":429,"error":{"code":"insufficient_quota","message":"synthetic quota exhausted"}}`, "synthetic quota exhausted"},
		{"failed response", `{"type":"response.failed","response":{"status":"failed","error":{"code":"server_error","message":"synthetic upstream failure"}}}`, "synthetic upstream failure"},
		{"token limit", `{"type":"response.incomplete","response":{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"}}}`, "max_output_tokens"},
		{"canceled", `{"type":"response.cancelled","response":{"status":"cancelled"}}`, "canceled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			events := []*httpclient.StreamEvent{{Data: []byte(`{"type":"response.created","response":{"id":"resp_summary","status":"in_progress","output":[]}}`)}, {Data: []byte(`{"type":"response.output_text.delta","delta":"partial summary must never be published"}`)}}
			if tc.terminal != "" {
				events = append(events, &httpclient.StreamEvent{Data: []byte(tc.terminal)})
			}
			perf := new(biz.PerformanceRecord)
			chunks, err := collectLocalCompactionBridgeStream(streams.SliceStream(events), perf)
			require.ErrorContains(t, err, tc.want)
			require.Nil(t, chunks)
			require.False(t, perf.RequestCompleted)
			if tc.name == "standalone error" {
				var failure *llm.ResponseError
				require.ErrorAs(t, err, &failure)
				require.Equal(t, 429, failure.StatusCode)
				require.Equal(t, "insufficient_quota", failure.Detail.Code)
			}
		})
	}
}
