package responses

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm/httpclient"
)

// codex2api 0d2b585f identified the native Codex option; verify conversion and
// persisted unified requests, where metadata pointers become JSON scalars.
func TestReasoningSummaryDeliveryRoundTrip(t *testing.T) {
	for _, options := range []string{`{"reasoning_summary_delivery":"sequential_cutoff"}`, `{"reasoning_summary_delivery":"sequential_cutoff","include_obfuscation":false}`, `{}`} {
		t.Run(options, func(t *testing.T) {
			body := `{"model":"gpt-6.2-sol","input":"continue","stream":true,"stream_options":` + options + `}`
			request, err := NewInboundTransformer().TransformRequest(t.Context(), &httpclient.Request{Body: []byte(body)})
			require.NoError(t, err)
			saved, err := json.Marshal(request)
			require.NoError(t, err)
			require.NoError(t, json.Unmarshal(saved, request))
			outbound, err := NewOutboundTransformer("https://example.test/v1", "synthetic-key")
			require.NoError(t, err)
			result, err := outbound.TransformRequest(t.Context(), request)
			require.NoError(t, err)
			var fields map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(result.Body, &fields))
			if options == `{}` {
				require.NotContains(t, fields, "stream_options")
			} else {
				require.JSONEq(t, options, string(fields["stream_options"]))
			}
		})
	}
	for _, invalid := range []string{`true`, `42`, `[]`, `{}`} {
		_, err := NewInboundTransformer().TransformRequest(t.Context(), &httpclient.Request{Body: []byte(`{"model":"gpt-6.2-sol","input":"continue","stream_options":{"reasoning_summary_delivery":` + invalid + `}}`)})
		require.Error(t, err)
	}
}

func TestCodex172ToolMetadataRoundTrip(t *testing.T) {
	const metadata = `{"executed_tool_calls":[{"name":"lookup","arguments":{"q":"fixture"},"tool_result":{"content":[{"type":"text","text":"synthetic result"}]},"tool_result_call_id":"nested_fixture"}]}`
	body := `{"model":"gpt-6.2-sol","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}],"internal_chat_message_metadata_passthrough":`+metadata+`}]}`
	request, err := NewInboundTransformer().TransformRequest(t.Context(), &httpclient.Request{Body: []byte(body)})
	require.NoError(t, err)
	saved, err := json.Marshal(request)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(saved, request))
	outbound, err := NewOutboundTransformer("https://example.test/v1", "synthetic-key")
	require.NoError(t, err)
	result, err := outbound.TransformRequest(t.Context(), request)
	require.NoError(t, err)
	var payload struct { Input []map[string]json.RawMessage `json:"input"` }
	require.NoError(t, json.Unmarshal(result.Body, &payload))
	require.Len(t, payload.Input, 1)
	require.JSONEq(t, metadata, string(payload.Input[0]["internal_chat_message_metadata_passthrough"]))
}
