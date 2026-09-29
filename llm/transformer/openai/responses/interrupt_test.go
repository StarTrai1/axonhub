package responses

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm/httpclient"
)

func TestAggregateInterruptedResponseDropsPartialItems(t *testing.T) {
	chunks := []*httpclient.StreamEvent{
		{Data: []byte(`{"type":"response.created","response":{"id":"resp_interrupted","status":"in_progress"}}`)},
		{Data: []byte(`{"type":"response.output_item.added","output_index":0,"item":{"id":"rs_discard","type":"reasoning","summary":[]}}`)},
		{Data: []byte(`{"type":"response.reasoning_summary_text.delta","output_index":0,"item_id":"rs_discard","summary_index":0,"delta":"unfinished"}`)},
		{Data: []byte(`{"type":"response.output_item.interrupted","output_index":0,"item_id":"rs_discard"}`)},
		{Data: []byte(`{"type":"response.incomplete","response":{"id":"resp_interrupted","status":"incomplete","incomplete_details":{"reason":"interrupted"},"output":[{"id":"msg_keep","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"kept"}]}],"usage":{"input_tokens":10,"output_tokens":3,"total_tokens":13}}}`)},
	}
	body, _, err := AggregateStreamChunks(t.Context(), chunks)
	require.NoError(t, err)
	var response Response
	require.NoError(t, json.Unmarshal(body, &response))
	require.Equal(t, "incomplete", *response.Status)
	require.Equal(t, "interrupted", response.IncompleteDetails.Reason)
	require.Len(t, response.Output, 1)
	require.Equal(t, "msg_keep", response.Output[0].ID)
	require.NotContains(t, string(body), "rs_discard")
}
