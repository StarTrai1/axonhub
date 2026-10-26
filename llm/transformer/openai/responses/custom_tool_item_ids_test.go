package responses

import (
	"encoding/json"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
)

func TestCustomToolItemIdentitySurvivesHTTPConversion(t *testing.T) {
	for _, sourceID := range []string{"ctc_native", "call_legacy", ""} {
		t.Run(sourceID, func(t *testing.T) {
			item := Item{Type: "custom_tool_call", ID: sourceID, CallID: "call_pair", Name: "exec", Input: lo.ToPtr("exact input")}
			message, err := convertItemToMessage(&item)
			require.NoError(t, err)
			result := convertToResponsesAPIResponse(&llm.Response{Choices: []llm.Choice{{Message: message}}})
			require.Len(t, result.Output, 1)
			require.Equal(t, "call_pair", result.Output[0].CallID)
			require.Equal(t, "exact input", *result.Output[0].Input)
			require.Contains(t, result.Output[0].ID, "ctc_")
			require.NotEqual(t, result.Output[0].CallID, result.Output[0].ID)
			if sourceID == "ctc_native" {
				require.Equal(t, sourceID, result.Output[0].ID)
			}
			outputMessage := convertOutputToMessage([]Item{item}, nil)
			outputResult := convertToResponsesAPIResponse(&llm.Response{Choices: []llm.Choice{{Message: &outputMessage}}})
			require.Equal(t, result.Output, outputResult.Output)
		})
	}
}

func TestCustomToolItemIdentitySurvivesStreamConversion(t *testing.T) {
	for _, sourceID := range []string{"ctc_native", "call_legacy"} {
		t.Run(sourceID, func(t *testing.T) {
			item := Item{Type: "custom_tool_call", ID: sourceID, CallID: "call_pair", Name: "exec", Input: lo.ToPtr("")}
			added, err := json.Marshal(StreamEvent{Type: StreamEventTypeOutputItemAdded, Item: &item})
			require.NoError(t, err)
			delta, err := json.Marshal(StreamEvent{Type: StreamEventTypeCustomToolCallInputDelta, ItemID: &sourceID, Delta: "exact input"})
			require.NoError(t, err)
			converted := newResponsesOutboundStream(streams.SliceStream([]*httpclient.StreamEvent{
				{Data: []byte(`{"type":"response.created","response":{"id":"resp_test","model":"test","status":"in_progress","output":[]}}`)},
				{Data: added}, {Data: delta},
				{Data: []byte(`{"type":"response.completed","response":{"id":"resp_test","status":"completed","output":[]}}`)},
			}))
			downstream, err := NewInboundTransformer().TransformStream(t.Context(), converted)
			require.NoError(t, err)
			events, err := streams.All(downstream)
			require.NoError(t, err)
			identity := ""
			seen := make(map[string]bool)
			for _, event := range events {
				var decoded StreamEvent
				if string(event.Data) == "[DONE]" {
					continue
				}
				require.NoError(t, json.Unmarshal(event.Data, &decoded))
				if decoded.Item != nil && decoded.Item.Type == "custom_tool_call" {
					if identity == "" {
						identity = decoded.Item.ID
					}
					require.Equal(t, identity, decoded.Item.ID)
					require.Equal(t, "call_pair", decoded.Item.CallID)
					seen[decoded.Type] = true
				}
				if decoded.Type == StreamEventTypeCustomToolCallInputDelta || decoded.Type == StreamEventTypeCustomToolCallInputDone {
					require.Equal(t, identity, lo.FromPtr(decoded.ItemID))
					seen[decoded.Type] = true
				}
				if decoded.Type == StreamEventTypeResponseCompleted {
					require.Len(t, decoded.Response.Output, 1)
					require.Equal(t, identity, decoded.Response.Output[0].ID)
					require.Equal(t, "exact input", *decoded.Response.Output[0].Input)
					seen[decoded.Type] = true
				}
			}
			require.Len(t, seen, 5)
			require.Contains(t, identity, "ctc_")
			if sourceID == "ctc_native" {
				require.Equal(t, sourceID, identity)
			}
		})
	}
}

func TestLegacyCustomToolReplayKeepsCorrelationAndOpaqueState(t *testing.T) {
	body := []byte(`{"input":[{"type":"reasoning","id":"rs_native","encrypted_content":"cipher"},{"type":"custom_tool_call","id":"call_legacy","call_id":"call_legacy","name":"exec","input":"exact input","async":true,"extra":9007199254740993},{"type":"custom_tool_call_output","call_id":"call_legacy","output":{"id":"call_business","encrypted_content":"business"}},{"type":"custom_tool_call","id":"ctc_native","call_id":"call_native","name":"exec","input":"native"}]}`)
	for _, url := range []string{"https://api.openai.com/v1/responses", "wss://chatgpt.com/backend-api/codex/responses", "https://relay.example/v1/responses"} {
		request := &httpclient.Request{URL: url, Body: body, JSONBody: body}
		repaired := PrepareReplayItemIDs(request)
		require.NotSame(t, request, repaired)
		require.Equal(t, body, request.Body)
		require.Equal(t, repaired.Body, repaired.JSONBody)
		require.False(t, gjson.GetBytes(repaired.Body, "input.1.id").Exists())
		for _, path := range []string{"input.0", "input.1.call_id", "input.1.input", "input.1.async", "input.1.extra", "input.2", "input.3"} {
			require.Equal(t, gjson.GetBytes(body, path).Raw, gjson.GetBytes(repaired.Body, path).Raw, path)
		}
		require.Same(t, repaired, PrepareReplayItemIDs(repaired))
	}
	for _, input := range []string{
		`[{"type":"custom_tool_call","id":"call_ref","call_id":"call_ref","name":"exec","input":"value"},{"type":"item_reference","id":"call_ref"}]`,
		`[{"type":"custom_tool_call","id":"call_incomplete","call_id":"call_incomplete","name":"exec"}]`,
		`[{"type":"custom_tool_call","id":"call_different","call_id":"call_pair","name":"exec","input":"value"}]`,
		`[{"type":"future_type","id":"call_unknown","call_id":"call_unknown","name":"exec","input":"value"}]`,
	} {
		request := &httpclient.Request{URL: "https://relay.example/v1/responses", Body: []byte(`{"input":` + input + `}`)}
		require.Same(t, request, PrepareReplayItemIDs(request))
	}
}
