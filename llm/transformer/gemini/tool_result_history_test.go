package gemini

import (
	"encoding/json"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
)

func TestToolResultsResolveRepeatedIDsFromPrecedingCalls(t *testing.T) {
	request := &llm.Request{Model: "gemini-2.5-flash", Messages: []llm.Message{
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "reused", Type: "function", Function: llm.FunctionCall{Name: "first_tool", Arguments: `{}`}}}},
		{Role: "tool", ToolCallID: lo.ToPtr("reused"), Content: llm.MessageContent{Content: lo.ToPtr(`{"value":"first"}`)}},
		{Role: "user", Content: llm.MessageContent{Content: lo.ToPtr("next turn")}},
		{Role: "assistant", ToolCalls: []llm.ToolCall{
			{ID: "reused", Type: "function", Function: llm.FunctionCall{Name: "second_tool", Arguments: `{}`}},
			{ID: "parallel", Type: "function", Function: llm.FunctionCall{Name: "parallel_tool", Arguments: `{}`}},
		}},
		{Role: "tool", ToolCallID: lo.ToPtr("parallel"), Content: llm.MessageContent{Content: lo.ToPtr(`{"value":"parallel"}`)}},
		{Role: "tool", ToolCallID: lo.ToPtr("reused"), Content: llm.MessageContent{Content: lo.ToPtr(`{"value":"second"}`)}},
		{Role: "tool", ToolCallID: lo.ToPtr("unknown"), Content: llm.MessageContent{Content: lo.ToPtr("unknown result")}},
		{Role: "tool", ToolCallID: lo.ToPtr("reused"), ToolCallName: lo.ToPtr("explicit_name"), Content: llm.MessageContent{Content: lo.ToPtr("explicit result")}},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "reused", Type: "function", Function: llm.FunctionCall{Name: "future_tool", Arguments: `{}`}}}},
	}}
	original, err := json.Marshal(request)
	require.NoError(t, err)
	outbound, err := NewOutboundTransformer("https://generativelanguage.googleapis.com", "test-key")
	require.NoError(t, err)
	got, err := outbound.TransformRequest(t.Context(), request)
	require.NoError(t, err)
	var payload GenerateContentRequest
	require.NoError(t, json.Unmarshal(got.Body, &payload))
	var results []*FunctionResponse
	for _, content := range payload.Contents {
		for _, part := range content.Parts {
			if part.FunctionResponse != nil {
				results = append(results, part.FunctionResponse)
			}
		}
	}
	require.Len(t, results, 5)
	for i, name := range []string{"first_tool", "parallel_tool", "second_tool", "", "explicit_name"} {
		require.Equal(t, name, results[i].Name)
	}
	require.Equal(t, "first", results[0].Response["value"])
	require.Equal(t, "second", results[2].Response["value"])
	after, err := json.Marshal(request)
	require.NoError(t, err)
	require.Equal(t, original, after)
}
