package anthropic

import (
	"encoding/json"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm/httpclient"
)

func TestAggregateStreamIgnoresNegativeContentIndices(t *testing.T) {
	var chunks []*httpclient.StreamEvent
	for _, raw := range []string{
		`{"type":"message_start","message":{"id":"msg_test","type":"message","role":"assistant","model":"test"}}`,
		`{"type":"content_block_start","index":-1,"content_block":{"type":"text","text":"poison"}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":-1,"delta":{"type":"text_delta","text":"poison"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`,
		`{"type":"message_stop"}`,
	} { chunks = append(chunks, &httpclient.StreamEvent{Data:[]byte(raw)}) }
	body, _, err := AggregateStreamChunks(t.Context(), chunks, PlatformDirect)
	require.NoError(t, err)
	var response Message
	require.NoError(t, json.Unmarshal(body, &response))
	require.Len(t, response.Content, 1)
	require.Equal(t, "hello", lo.FromPtr(response.Content[0].Text))
}
