package gemini_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer/anthropic"
	"github.com/looplj/axonhub/llm/transformer/gemini"
)

func TestGeminiLaterInstructionsKeepHistoryPrefix(t *testing.T) {
	for _, role := range []string{"system", "developer"} {
		t.Run(role, func(t *testing.T) {
			req := &llm.Request{Model: "gemini-3-pro-preview", Messages: []llm.Message{
				{Role: "system", Content: llm.MessageContent{Content: lo.ToPtr("stable rules")}},
				{Role: "user", Content: llm.MessageContent{Content: lo.ToPtr("question")}},
				{Role: "assistant", Content: llm.MessageContent{Content: lo.ToPtr("answer")}},
			}}
			before := peerGeminiRequest(t, req)
			req.Messages = append(req.Messages,
				llm.Message{Role: role, Content: llm.MessageContent{Content: lo.ToPtr("new reminder")}},
				llm.Message{Role: "user", Content: llm.MessageContent{Content: lo.ToPtr("continue")}},
			)
			after := peerGeminiRequest(t, req)
			require.Equal(t, before.SystemInstruction, after.SystemInstruction)
			require.Equal(t, before.Contents, after.Contents[:len(before.Contents)])
			require.Len(t, after.Contents, 4)
			require.Equal(t, "new reminder", after.Contents[2].Parts[0].Text)
			require.Equal(t, "user", after.Contents[2].Role)
		})
	}
}

func TestGeminiSplitAnthropicResultsKeepOneResponseTurn(t *testing.T) {
	body := []byte(`{"model":"claude-test","max_tokens":1000,"messages":[
		{"role":"assistant","content":[{"type":"tool_use","id":"a","name":"first","input":{}},{"type":"tool_use","id":"b","name":"second","input":{}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"a","content":"result a"},{"type":"text","text":"reminder a"}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"b","content":"result b"},{"type":"text","text":"reminder b"}]}
	]}`)
	req, err := anthropic.NewInboundTransformer().TransformRequest(t.Context(), &httpclient.Request{Body: body, Headers: http.Header{"Content-Type": {"application/json"}}})
	require.NoError(t, err)
	req.Model = "gemini-3-pro-preview"
	original, err := json.Marshal(req)
	require.NoError(t, err)
	before := peerGeminiRequest(t, req)
	require.Len(t, before.Contents, 4)
	require.Len(t, before.Contents[1].Parts, 2)
	require.Equal(t, "a", before.Contents[1].Parts[0].FunctionResponse.ID)
	require.Equal(t, "b", before.Contents[1].Parts[1].FunctionResponse.ID)
	require.Equal(t, "reminder a", before.Contents[2].Parts[0].Text)
	require.Equal(t, "reminder b", before.Contents[3].Parts[0].Text)
	unchanged, err := json.Marshal(req)
	require.NoError(t, err)
	require.JSONEq(t, string(original), string(unchanged))
	req.Messages = append(req.Messages, llm.Message{Role: "user", Content: llm.MessageContent{Content: lo.ToPtr("continue")}})
	after := peerGeminiRequest(t, req)
	require.Equal(t, before.Contents, after.Contents[:len(before.Contents)])
}

func TestGeminiInstructionsBetweenParallelResultsKeepCallAdjacency(t *testing.T) {
	for _, platform := range []string{"", gemini.PlatformVertex} {
		t.Run(platform, func(t *testing.T) {
			req := &llm.Request{Model: "gemini-3-pro-preview", Messages: []llm.Message{
				{Role: "system", Content: llm.MessageContent{Content: lo.ToPtr("stable rules")}},
				{Role: "assistant", ToolCalls: []llm.ToolCall{
					{ID: "a", Type: "function", Function: llm.FunctionCall{Name: "first", Arguments: `{}`}},
					{ID: "b", Type: "function", Function: llm.FunctionCall{Name: "second", Arguments: `{}`}},
				}},
				{Role: "system", Content: llm.MessageContent{Content: lo.ToPtr("reminder a")}},
				{Role: "tool", ToolCallID: lo.ToPtr("a"), Content: llm.MessageContent{Content: lo.ToPtr("result a")}},
				{Role: "developer", Content: llm.MessageContent{Content: lo.ToPtr("reminder b")}},
				{Role: "tool", ToolCallID: lo.ToPtr("b"), Content: llm.MessageContent{Content: lo.ToPtr("result b")}},
			}}
			adapter, err := gemini.NewOutboundTransformerWithConfig(gemini.Config{PlatformType: platform})
			require.NoError(t, err)
			wire, err := adapter.TransformRequest(t.Context(), req)
			require.NoError(t, err)
			var result gemini.GenerateContentRequest
			require.NoError(t, json.Unmarshal(wire.Body, &result))
			require.Len(t, result.SystemInstruction.Parts, 1)
			require.Len(t, result.Contents, 4)
			require.Len(t, result.Contents[1].Parts, 2)
			for i, name := range []string{"first", "second"} {
				require.Equal(t, name, result.Contents[1].Parts[i].FunctionResponse.Name)
				if platform == gemini.PlatformVertex {
					require.Empty(t, result.Contents[1].Parts[i].FunctionResponse.ID)
				}
			}
			require.Equal(t, "reminder a", result.Contents[2].Parts[0].Text)
			require.Equal(t, "reminder b", result.Contents[3].Parts[0].Text)
		})
	}
}

func peerGeminiRequest(t *testing.T, req *llm.Request) *gemini.GenerateContentRequest {
	t.Helper()
	adapter, err := gemini.NewOutboundTransformer("", "fixture")
	require.NoError(t, err)
	wire, err := adapter.TransformRequest(t.Context(), req)
	require.NoError(t, err)
	var result gemini.GenerateContentRequest
	require.NoError(t, json.Unmarshal(wire.Body, &result))
	return &result
}
