package responses

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm/httpclient"
)

func TestPrepareReplayItemIDsPreservesHistory(t *testing.T) {
	body := []byte(`{"model":"gpt-6-astra","input":[{"type":"reasoning","id":"item_foreign","summary":[{"type":"summary_text","text":"retained reasoning"}],"encrypted_content":null,"extra":{"value":9007199254740993}},{"type":"function_call","id":"item_tool","call_id":"call_original","name":"lookup","arguments":"{}","async":true},{"type":"function_call_output","id":"item_result","call_id":"call_original","output":"retained result"},{"type":"reasoning","id":"rs_native","encrypted_content":"opaque-native"},{"type":"reasoning","id":"item_encrypted","encrypted_content":"opaque-foreign"},{"type":"item_reference","id":"item_reference"},{"type":"configuration_update","reasoning":{"effort":"high"}}]}`)
	request := &httpclient.Request{URL: "wss://chatgpt.com/backend-api/codex/responses", Body: body}
	result := PrepareReplayItemIDs(request)
	require.NotSame(t, request, result)
	require.Equal(t, body, request.Body)
	require.True(t, gjson.GetBytes(request.Body, "input.0.id").Exists())
	for _, index := range []string{"0", "1", "2", "4"} {
		require.False(t, gjson.GetBytes(result.Body, "input."+index+".id").Exists())
	}
	require.Equal(t, "retained reasoning", gjson.GetBytes(result.Body, "input.0.summary.0.text").String())
	require.Equal(t, "9007199254740993", gjson.GetBytes(result.Body, "input.0.extra.value").Raw)
	require.Equal(t, "call_original", gjson.GetBytes(result.Body, "input.1.call_id").String())
	require.Equal(t, "call_original", gjson.GetBytes(result.Body, "input.2.call_id").String())
	require.True(t, gjson.GetBytes(result.Body, "input.1.async").Bool())
	require.Equal(t, "retained result", gjson.GetBytes(result.Body, "input.2.output").String())
	require.Equal(t, "rs_native", gjson.GetBytes(result.Body, "input.3.id").String())
	require.Equal(t, "opaque-native", gjson.GetBytes(result.Body, "input.3.encrypted_content").String())
	require.Equal(t, "opaque-foreign", gjson.GetBytes(result.Body, "input.4.encrypted_content").String())
	require.Equal(t, "item_reference", gjson.GetBytes(result.Body, "input.5.id").String())
	require.Equal(t, "high", gjson.GetBytes(result.Body, "input.6.reasoning.effort").String())
	require.Same(t, result, PrepareReplayItemIDs(result))
}

func TestPrepareReplayItemIDsLeavesOtherContractsUntouched(t *testing.T) {
	for _, testCase := range []struct {
		name string
		url  string
		body string
	}{
		{name: "relay", url: "https://relay.example/v1/responses", body: `{"input":[{"type":"reasoning","id":"item_relay","summary":[]}]}`},
		{name: "lookalike host", url: "https://chatgpt.com.example/responses", body: `{"input":[{"type":"reasoning","id":"item_relay"}]}`},
		{name: "native IDs", url: "https://api.openai.com/v1/responses", body: `{"input":[{"type":"reasoning","id":"rs_native"},{"type":"message","id":"msg_native","content":"item_literal"}]}`},
		{name: "unknown item", url: "https://api.openai.com/v1/responses", body: `{"input":[{"type":"future_type","id":"item_future"}]}`},
		{name: "string input", url: "https://api.openai.com/v1/responses", body: `{"input":"item_literal"}`},
		{name: "invalid JSON", url: "https://api.openai.com/v1/responses", body: `{"input":[{"type":"reasoning","id":"item_bad"}]`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			request := &httpclient.Request{URL: testCase.url, Body: []byte(testCase.body)}
			require.Same(t, request, PrepareReplayItemIDs(request))
		})
	}
	require.Nil(t, PrepareReplayItemIDs(nil))
}

func TestResponsesFinalizerNormalizesReplayIDsForBothTransports(t *testing.T) {
	for _, transport := range []string{TransportHTTP, TransportWebSocket} {
		t.Run(transport, func(t *testing.T) {
			outbound := &OutboundTransformer{config: &Config{Transport: transport}}
			request := &httpclient.Request{URL: "https://api.openai.com/v1/responses", Body: []byte(`{"input":[{"type":"message","id":"item_message","content":"preserved"}]}`)}
			result := outbound.FinalizeTransportRequest(request)
			require.False(t, gjson.GetBytes(result.Body, "input.0.id").Exists())
			require.Equal(t, "preserved", gjson.GetBytes(result.Body, "input.0.content").String())
		})
	}
}
