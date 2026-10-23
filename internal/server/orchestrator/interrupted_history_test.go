package orchestrator

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm/httpclient"
)

func TestInterruptedSnapshotDoesNotRestoreDiscardedItems(t *testing.T) {
	for _, reason := range []string{"interrupted", "max_output_tokens", "content_filter"} {
		t.Run(reason, func(t *testing.T) {
			stream := &responsesSessionStream{}
			stream.current = &httpclient.StreamEvent{Type:"response.output_item.done", Data:[]byte(`{"type":"response.output_item.done","output_index":0,"item":{"type":"message","id":"discarded","role":"assistant","content":[{"type":"output_text","text":"discard this"}]}}`)}
			stream.captureCompletedItem()
			stream.current = &httpclient.StreamEvent{Type:"response.incomplete", Data:[]byte(fmt.Sprintf(`{"type":"response.incomplete","response":{"id":"resp_stopped","status":"incomplete","incomplete_details":{"reason":%q},"output":[]}}`, reason))}
			body, rejected := stream.terminalSnapshot()
			require.Equal(t, reason != "interrupted", rejected)
			if !rejected { require.Empty(t, gjson.GetBytes(body, "output").Array()); require.NotContains(t, string(body), "discard this") }
		})
	}
}
