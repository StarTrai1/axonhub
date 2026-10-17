package responses

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
)

func TestResponsesEncryptedFunctionArgsHistoryRoundTrip(t *testing.T) {
	for _, prefix := range []string{"", `{"type":"reasoning","id":"rs_before","encrypted_content":"opaque-reasoning"},`} {
		for _, marker := range []string{"", `null`, `[]`, `["message"]`, `"future-marker"`, `{"future":true}`} {
			t.Run(fmt.Sprintf("reasoning=%t/marker=%s", prefix != "", marker), func(t *testing.T) {
				field := ""
				if marker != "" {
					field = `,"encrypted_function_args":` + marker
				}
				body := `{"model":"gpt-6-astra","input":[` + prefix + `{"type":"function_call","call_id":"call_agent","namespace":"agents","name":"spawn_agent","arguments":"{\"message\":\"opaque-instructions\"}"` + field + `},{"type":"function_call_output","call_id":"call_agent","output":"queued"}]}`
				request, err := NewInboundTransformer().TransformRequest(t.Context(), &httpclient.Request{Body: []byte(body)})
				require.NoError(t, err)
				// The provider metadata must also survive persisted common history.
				encoded, err := json.Marshal(request)
				require.NoError(t, err)
				var restored llm.Request
				require.NoError(t, json.Unmarshal(encoded, &restored))
				outbound, err := NewOutboundTransformer("https://relay.example", "synthetic-key")
				require.NoError(t, err)
				for _, history := range []*llm.Request{request, &restored} {
					result, err := outbound.TransformRequest(t.Context(), history)
					require.NoError(t, err)
					call := gjson.GetBytes(result.Body, `input.#(type=="function_call")`)
					require.Equal(t, "call_agent", call.Get("call_id").String())
					require.Equal(t, "opaque-instructions", gjson.Get(call.Get("arguments").String(), "message").String())
					if marker == "" {
						require.False(t, call.Get("encrypted_function_args").Exists())
					} else {
						require.JSONEq(t, marker, call.Get("encrypted_function_args").Raw)
					}
					require.Equal(t, "call_agent", gjson.GetBytes(result.Body, `input.#(type=="function_call_output").call_id`).String())
				}
			})
		}
	}
}
