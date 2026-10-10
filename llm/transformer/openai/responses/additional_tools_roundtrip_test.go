package responses

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/auth"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer"
	"github.com/looplj/axonhub/llm/transformer/openai"
)

// additionalToolsLiteRequest is the shape Codex CLI sends for a model it marks as
// Responses Lite: the tool definitions ride inside a `developer` input item
// instead of the top-level `tools` array.
const additionalToolsLiteRequest = `{
	"model": "gpt-6-luna",
	"tools": [{"type":"function","name":"top_level","parameters":{"type":"object"}}],
	"input": [
		{
			"type": "additional_tools",
			"id": "at_1",
			"role": "developer",
			"tools": [
				{
					"type": "namespace",
					"name": "functions",
					"tools": [
						{"type": "custom", "name": "exec", "description": "run a script", "x_nested": {"enabled": true}},
						{"type": "function", "name": "shell", "parameters": {"type": "object", "properties": {}}}
					]
				}
			]
		},
		{"type": "message", "role": "user", "content": "Hello"}
	]
}`

func additionalToolsOutbound(t *testing.T) *OutboundTransformer {
	t.Helper()

	out, err := NewOutboundTransformerWithConfig(&Config{
		BaseURL:        "https://example.com",
		APIKeyProvider: auth.NewStaticKeyProvider("test"),
	})
	require.NoError(t, err)

	return out
}

// additionalToolsInput runs the lite request through the inbound and outbound
// transformers and returns the replayed `input` array of the outgoing body.
func additionalToolsInput(t *testing.T, out *OutboundTransformer) []json.RawMessage {
	t.Helper()

	req, err := NewInboundTransformer().TransformRequest(
		t.Context(), &httpclient.Request{Body: []byte(additionalToolsLiteRequest)})
	require.NoError(t, err)

	wire, err := out.TransformRequest(t.Context(), req)
	require.NoError(t, err)

	var body struct {
		Input []json.RawMessage `json:"input"`
	}
	require.NoError(t, json.Unmarshal(wire.Body, &body))

	return body.Input
}

func TestAdditionalTools_ReplayedForEveryResponsesUpstream(t *testing.T) {
	input := additionalToolsInput(t, additionalToolsOutbound(t))

	require.Len(t, input, 2)
	require.Contains(t, string(input[0]), `"additional_tools"`)
	require.Contains(t, string(input[0]), `"exec"`)
	require.Contains(t, string(input[0]), `"shell"`)
	require.Contains(t, string(input[0]), `"x_nested":{"enabled":true}`)
	require.Contains(t, string(input[1]), "Hello")
}

func TestAdditionalTools_RepeatedTransformDoesNotDuplicateItems(t *testing.T) {
	req, err := NewInboundTransformer().TransformRequest(
		t.Context(), &httpclient.Request{Body: []byte(additionalToolsLiteRequest)})
	require.NoError(t, err)
	out := additionalToolsOutbound(t)

	first, err := out.TransformRequest(t.Context(), req)
	require.NoError(t, err)
	second, err := out.TransformRequest(t.Context(), req)
	require.NoError(t, err)

	var firstBody, secondBody struct {
		Input []json.RawMessage `json:"input"`
	}
	require.NoError(t, json.Unmarshal(first.Body, &firstBody))
	require.NoError(t, json.Unmarshal(second.Body, &secondBody))
	require.Len(t, firstBody.Input, 2)
	require.Len(t, secondBody.Input, 2)
	require.Equal(t, firstBody.Input, secondBody.Input)
}

func TestAdditionalTools_ReplayedForResponsesUpstream(t *testing.T) {
	input := additionalToolsInput(t, additionalToolsOutbound(t))

	require.Len(t, input, 2)
	require.Contains(t, string(input[0]), `"additional_tools"`)
	require.Contains(t, string(input[0]), `"exec"`)
	require.Contains(t, string(input[0]), `"shell"`)
	require.Contains(t, string(input[1]), "Hello")
}

const additionalToolsMixedRequest = `{
	"model": "gpt-6-luna",
	"input": [
		{"type": "additional_tools", "id": "at_1", "role": "developer", "tools": []},
		{"type": "message", "role": "user", "content": "Hello"},
		{"type": "web_search_call", "id": "ws_1", "status": "completed"}
	]
}`

func additionalToolsMixedInput(t *testing.T) []json.RawMessage {
	t.Helper()

	req, err := NewInboundTransformer().TransformRequest(
		t.Context(), &httpclient.Request{Body: []byte(additionalToolsMixedRequest)})
	require.NoError(t, err)

	wire, err := additionalToolsOutbound(t).TransformRequest(t.Context(), req)
	require.NoError(t, err)

	var body struct {
		Input []json.RawMessage `json:"input"`
	}
	require.NoError(t, json.Unmarshal(wire.Body, &body))

	return body.Input
}

func TestAdditionalTools_MixedRawItemsKeepTheirPlace(t *testing.T) {
	input := additionalToolsMixedInput(t)

	require.Len(t, input, 3)
	require.Contains(t, string(input[0]), "additional_tools")
	require.Contains(t, string(input[1]), "Hello")
	require.Contains(t, string(input[2]), "web_search_call")
}

func TestAdditionalToolsNativeSearchReplacesBridgeInCreateAndCompact(t *testing.T) {
	const body = `{"model":"gpt-6.1-sol","input":[
		{"type":"reasoning","summary":[]},
		{"type":"additional_tools","id":"at_native","role":"developer","tools":[]},
		{"type":"message","role":"user","content":"before"},
		{"type":"web_search_call","id":"ws_first","status":"completed","action":{"type":"search","query":"first"},"x_future":{"kept":true}},
		{"type":"file_search_call","id":"fs_after","queries":["memo"]},
		{"type":"web_search_call","status":"completed"},
		{"type":"web_search_call","id":"ws_second","status":"completed","action":{"type":"search","query":"second"}},
		{"type":"function_call","call_id":"client_call","name":"web_search","arguments":"{}"},
		{"type":"function_call_output","call_id":"client_call","output":"local result"},
		{"type":"input_file","file_id":"file_native","x_future":{"kept":true}},
		{"type":"message","role":"user","content":"after"}
	]}`
	for name, inbound := range map[string]transformer.Inbound{
		"create":  NewInboundTransformer(),
		"compact": NewCompactInboundTransformer(),
	} {
		t.Run(name, func(t *testing.T) {
			req, err := inbound.TransformRequest(t.Context(), &httpclient.Request{Body: []byte(body)})
			require.NoError(t, err)
			out := additionalToolsOutbound(t)
			for range 2 {
				cloned := *req
				cloned.ProviderExtensions = llm.CloneProviderExtensions(req.ProviderExtensions)
				wire, err := out.TransformRequest(t.Context(), &cloned)
				require.NoError(t, err)
				input := gjson.GetBytes(wire.Body, "input").Array()
				require.Len(t, input, 10)
				for i, typ := range []string{"additional_tools", "message", "web_search_call", "file_search_call", "web_search_call", "web_search_call", "function_call", "function_call_output", "input_file", "message"} {
					require.Equal(t, typ, input[i].Get("type").String())
				}
				original := gjson.Get(body, "input").Array()
				for _, i := range []int{1, 3, 4, 5, 6, 9} {
					require.JSONEq(t, original[i].Raw, input[i-1].Raw)
				}
				require.Equal(t, "client_call", input[6].Get("call_id").String())
				require.Equal(t, "client_call", input[7].Get("call_id").String())
				require.Contains(t, input[9].Raw, "after")
			}
		})
	}
}

func TestAdditionalToolsHistoryOrderSurvivesChatConversion(t *testing.T) {
	const body = `{"model":"gpt-6.1-sol","input":[
		{"type":"function_call","call_id":"call_order","name":"run","arguments":"{}"},
		{"type":"additional_tools","id":"at_order","role":"developer","tools":[]},
		{"type":"message","role":"user","content":"interjection"},
		{"type":"web_search_call","id":"ws_order","status":"completed","action":{"type":"search","query":"kept"}},
		{"type":"function_call_output","call_id":"call_order","output":"result stays with its original item"},
		{"type":"message","role":"assistant","phase":"partial_answer","content":[{"type":"output_text","text":"continue"}]}
	]}`
	for name, inbound := range map[string]transformer.Inbound{
		"create": NewInboundTransformer(), "compact": NewCompactInboundTransformer(),
	} {
		t.Run(name, func(t *testing.T) {
			req, err := inbound.TransformRequest(t.Context(), &httpclient.Request{Body: []byte(body)})
			require.NoError(t, err)
			before, err := json.Marshal(req)
			require.NoError(t, err)
			chat := openai.RequestFromLLM(t.Context(), req, openai.ReasoningFieldContent)
			require.Equal(t, "tool", chat.Messages[1].Role)
			require.Equal(t, "call_order", *chat.Messages[1].ToolCallID)
			after, err := json.Marshal(req)
			require.NoError(t, err)
			require.Equal(t, before, after)
			for range 2 {
				wire, err := additionalToolsOutbound(t).TransformRequest(t.Context(), req)
				require.NoError(t, err)
				items := gjson.GetBytes(wire.Body, "input").Array()
				require.Len(t, items, 6)
				for i, typ := range []string{"function_call", "additional_tools", "message", "web_search_call", "function_call_output", "message"} {
					require.Equal(t, typ, items[i].Get("type").String())
				}
				original := gjson.Get(body, "input").Array()
				for _, i := range []int{1, 3} {
					require.JSONEq(t, original[i].Raw, items[i].Raw)
				}
				require.Equal(t, "call_order", items[4].Get("call_id").String())
				require.Equal(t, "result stays with its original item", items[4].Get("output").String())
				require.Equal(t, "partial_answer", items[5].Get("phase").String())
			}
		})
	}
}

func TestAdditionalToolsAfterSkippedReasoningItemIsPreserved(t *testing.T) {
	const request = `{
		"model": "gpt-6-luna",
		"input": [
			{"type": "reasoning", "summary": []},
			{"type": "message", "role": "user", "content": "Hello"},
			{"type": "additional_tools", "id": "at_1", "role": "developer", "tools": [{"type": "custom", "name": "exec"}]}
		]
	}`

	req, err := NewInboundTransformer().TransformRequest(
		t.Context(), &httpclient.Request{Body: []byte(request)})
	require.NoError(t, err)

	wire, err := additionalToolsOutbound(t).TransformRequest(t.Context(), req)
	require.NoError(t, err)

	var body struct {
		Input []json.RawMessage `json:"input"`
	}
	require.NoError(t, json.Unmarshal(wire.Body, &body))
	require.Len(t, body.Input, 2)
	require.Contains(t, string(body.Input[0]), "Hello")
	require.Contains(t, string(body.Input[1]), "additional_tools")
}

func TestAdditionalToolsBeforeMessageStaysBeforeMessageAfterSkippedReasoning(t *testing.T) {
	const request = `{
		"model": "gpt-6-luna",
		"input": [
			{"type": "reasoning", "summary": []},
			{"type": "additional_tools", "id": "at_1", "role": "developer", "tools": [{"type": "custom", "name": "exec"}]},
			{"type": "message", "role": "user", "content": "Hello"}
		]
	}`

	req, err := NewInboundTransformer().TransformRequest(
		t.Context(), &httpclient.Request{Body: []byte(request)})
	require.NoError(t, err)

	wire, err := additionalToolsOutbound(t).TransformRequest(t.Context(), req)
	require.NoError(t, err)

	var body struct {
		Input []json.RawMessage `json:"input"`
	}
	require.NoError(t, json.Unmarshal(wire.Body, &body))
	require.Len(t, body.Input, 2)
	require.Contains(t, string(body.Input[0]), "additional_tools")
	require.Contains(t, string(body.Input[1]), "Hello")
}
