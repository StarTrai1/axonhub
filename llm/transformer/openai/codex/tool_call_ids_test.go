package codex

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer"
	"github.com/looplj/axonhub/llm/transformer/openai"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

func TestCodexTransportRepairsLegacyCustomItemIDs(t *testing.T) {
	for _, transport := range []string{responses.TransportHTTP, responses.TransportWebSocket} {
		t.Run(transport, func(t *testing.T) {
			outbound := newTestCodexOutbound(t)
			outbound.transport = transport
			body := []byte(`{"input":[{"type":"custom_tool_call","id":"call_legacy","call_id":"call_legacy","name":"exec","input":"exact input"},{"type":"custom_tool_call_output","call_id":"call_legacy","output":"exact output"}]}`)
			request := &httpclient.Request{URL: "https://relay.example/v1/responses", Body: body}
			wire := outbound.FinalizeTransportRequest(request)
			require.False(t, gjson.GetBytes(wire.Body, "input.0.id").Exists())
			require.Equal(t, "call_legacy", gjson.GetBytes(wire.Body, "input.0.call_id").String())
			require.Equal(t, "call_legacy", gjson.GetBytes(wire.Body, "input.1.call_id").String())
			require.Equal(t, "exact input", gjson.GetBytes(wire.Body, "input.0.input").String())
			require.Equal(t, "exact output", gjson.GetBytes(wire.Body, "input.1.output").String())
			require.Equal(t, body, request.Body)
		})
	}
}

func TestCodexChatToolCallIDsPreservePairingAcrossAttempts(t *testing.T) {
	longA := strings.Repeat("a", 70) + "A"
	longB := strings.Repeat("a", 70) + "B"
	digest := sha256.Sum256([]byte(longA))
	// Reserve a legal ID equal to the first shortening candidate, after the
	// long ID in the request. Future calls must not steal existing IDs.
	collision := "call_" + hex.EncodeToString(digest[:24])
	ids := []string{longA, longB, collision, strings.Repeat("s", 64), strings.Repeat("界", 70)}
	var calls []map[string]any
	for index, id := range ids {
		calls = append(calls, map[string]any{
			"id": id, "type": "function",
			"function": map[string]any{"name": fmt.Sprintf("tool_%d", index), "arguments": `{"keep":9007199254740993}`},
		})
	}
	messages := []map[string]any{
		{"role": "user", "content": "Run the tools"},
		{"role": "assistant", "tool_calls": calls},
	}
	for index := len(ids) - 1; index >= 0; index-- {
		messages = append(messages, map[string]any{"role": "tool", "tool_call_id": ids[index], "content": fmt.Sprintf("result_%d", index)})
	}
	// Reusing a source ID in a later turn keeps the same mapping.
	messages = append(messages, map[string]any{"role": "assistant", "tool_calls": calls[:1]},
		map[string]any{"role": "tool", "tool_call_id": longA, "content": "result_again"})
	body, err := json.Marshal(map[string]any{"model": "gpt-6.1-sol", "messages": messages})
	require.NoError(t, err)
	for _, transport := range []string{responses.TransportHTTP, responses.TransportWebSocket} {
		t.Run(transport, func(t *testing.T) {
			request, err := openai.NewInboundTransformer().TransformRequest(t.Context(), &httpclient.Request{
				Body: body, Headers: http.Header{"Content-Type": {"application/json"}},
			})
			require.NoError(t, err)
			before, err := json.Marshal(request.Messages)
			require.NoError(t, err)
			outbound := newTestCodexOutbound(t)
			outbound.transport = transport
			var previousInput string
			for range 2 {
				wire, err := outbound.TransformRequest(t.Context(), request)
				require.NoError(t, err)
				wire = outbound.FinalizeTransportRequest(wire)
				input := gjson.GetBytes(wire.Body, "input")
				if previousInput != "" {
					require.JSONEq(t, previousInput, input.Raw)
				}
				previousInput = input.Raw
				byName, byOutput := map[string]string{}, map[string]string{}
				for _, item := range input.Array() {
					switch item.Get("type").String() {
					case "function_call":
						id := item.Get("call_id").String()
						require.LessOrEqual(t, len(id), 64)
						byName[item.Get("name").String()] = id
						require.Equal(t, `{"keep":9007199254740993}`, item.Get("arguments").String())
					case "function_call_output":
						byOutput[item.Get("output").String()] = item.Get("call_id").String()
					}
				}
				require.Len(t, byName, len(ids))
				require.Len(t, byOutput, len(ids)+1)
				unique := map[string]bool{}
				for index := range ids {
					id := byName[fmt.Sprintf("tool_%d", index)]
					require.False(t, unique[id])
					unique[id] = true
					require.Equal(t, id, byOutput[fmt.Sprintf("result_%d", index)])
				}
				require.Equal(t, collision, byName["tool_2"])
				require.Equal(t, ids[3], byName["tool_3"])
				require.Equal(t, byName["tool_0"], byOutput["result_again"])
			}
			after, err := json.Marshal(request.Messages)
			require.NoError(t, err)
			require.Equal(t, before, after)
		})
	}
}

func TestCodexNativeHistoryKeepsLongCallIDs(t *testing.T) {
	longID := strings.Repeat("native", 16)
	// Match the recognized OpenAI envelope shape used by the existing
	// synthetic history fixtures; unrelated provider signatures are filtered.
	body := []byte(fmt.Sprintf(`{"model":"gpt-6.1-sol","input":[{"type":"reasoning","id":"rs_native","encrypted_content":"gAAAA_NATIVE_SYNTHETIC_BLOB","summary":[]},{"type":"function_call","call_id":%q,"name":"tool","arguments":"{}"},{"type":"function_call_output","call_id":%q,"output":"result"}]}`, longID, longID))
	for _, inbound := range []transformer.Inbound{responses.NewInboundTransformer(), responses.NewCompactInboundTransformer()} {
		for _, transport := range []string{responses.TransportHTTP, responses.TransportWebSocket} {
			t.Run(fmt.Sprintf("%T/%s", inbound, transport), func(t *testing.T) {
				request, err := inbound.TransformRequest(t.Context(), &httpclient.Request{Body: body})
				require.NoError(t, err)
				outbound := newTestCodexOutbound(t)
				outbound.transport = transport
				wire, err := outbound.TransformRequest(t.Context(), request)
				require.NoError(t, err)
				wire = outbound.FinalizeTransportRequest(wire)
				require.Equal(t, "gAAAA_NATIVE_SYNTHETIC_BLOB", gjson.GetBytes(wire.Body, "input.0.encrypted_content").String())
				require.Equal(t, longID, gjson.GetBytes(wire.Body, "input.1.call_id").String())
				require.Equal(t, longID, gjson.GetBytes(wire.Body, "input.2.call_id").String())
			})
		}
	}
}
