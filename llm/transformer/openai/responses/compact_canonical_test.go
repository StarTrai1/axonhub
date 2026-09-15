package responses

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm/httpclient"
)

func TestCompactRoundTripPreservesCanonicalWindow(t *testing.T) {
	providerBody := []byte(`{
		"id":"cmp_response","object":"response.compaction","created_at":1,"model":"gpt-6-astra",
		"output":[
			{"type":"message","id":"msg_retained","role":"user","content":[{"type":"input_text","text":"retained instruction"}],"internal_chat_message_metadata_passthrough":{"content_item_kinds":["plain_text"]}},
			{"type":"reasoning","id":"rs_retained","encrypted_content":"retained-reasoning","summary":[]},
			{"type":"function_call","id":"fc_retained","call_id":"call_retained","name":"read","namespace":"tools","arguments":"{}"},
			{"type":"function_call_output","call_id":"call_retained","output":"retained result"},
			{"type":"compaction","id":"cmp_retained","encrypted_content":"opaque-canonical-checkpoint"}
		]
	}`)
	unified, err := new(OutboundTransformer).transformCompactResponse(t.Context(), &httpclient.Response{StatusCode: http.StatusOK, Body: providerBody})
	require.NoError(t, err)
	returned, err := NewCompactInboundTransformer().TransformResponse(t.Context(), unified)
	require.NoError(t, err)
	require.JSONEq(t, gjson.GetBytes(providerBody, "output").Raw, gjson.GetBytes(returned.Body, "output").Raw)
	require.False(t, gjson.GetBytes(returned.Body, "output.0.status").Exists())
	require.False(t, gjson.GetBytes(returned.Body, "output.0.content.0.annotations").Exists())

	stored, err := json.Marshal(unified.Compact)
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(stored, "RawOutput").Exists())
	require.False(t, gjson.GetBytes(stored, "raw_output").Exists(), "the raw snapshot must not alter cache or persistence shapes")

	unified.Compact.RawOutput = nil
	unified.Compact.Output = nil
	returned, err = NewCompactInboundTransformer().TransformResponse(t.Context(), unified)
	require.NoError(t, err)
	require.Empty(t, gjson.GetBytes(returned.Body, "output").Array(), "a producer can explicitly replace the output and clear its raw snapshot")
}
