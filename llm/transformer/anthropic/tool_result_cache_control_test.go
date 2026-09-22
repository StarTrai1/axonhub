package anthropic

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
)

func TestToolResultCacheControlOnOutboundBlock(t *testing.T) {
	for _, automatic := range []bool{false, true} {
		for _, partMarker := range []bool{false, true} {
			t.Run(fmt.Sprintf("automatic=%v/part=%v", automatic, partMarker), func(t *testing.T) {
				parts := []llm.MessageContentPart{
					{Type: "text", Text: lo.ToPtr("capture result")},
					{Type: "image_url", ImageURL: &llm.ImageURL{URL: "data:image/png;base64,aW1hZ2U="}},
				}
				wantTTL := "5m"
				if partMarker {
					parts[0].CacheControl = &llm.CacheControl{Type: "ephemeral", TTL: "1h"}
					parts[1].CacheControl = &llm.CacheControl{Type: "ephemeral", TTL: "5m"}
					wantTTL = "1h"
				}
				req := &llm.Request{
					Model: "claude-sonnet-4-6", MaxTokens: lo.ToPtr(int64(128)),
					Messages: []llm.Message{
						{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "call_capture", Type: "function", Function: llm.FunctionCall{Name: "capture", Arguments: "{}"}}}},
						{Role: "tool", ToolCallID: lo.ToPtr("call_capture"), ToolCallIsError: lo.ToPtr(true), CacheControl: &llm.CacheControl{Type: "ephemeral", TTL: "5m"}, Content: llm.MessageContent{MultipleContent: parts}},
					},
				}
				if automatic {
					req.TransformerMetadata = map[string]any{TransformerMetadataKeyCacheControl: &CacheControl{Type: "ephemeral"}}
				}
				before, err := json.Marshal(req)
				require.NoError(t, err)
				outbound, err := NewOutboundTransformer("https://example.com", "test-key")
				require.NoError(t, err)
				wire, err := outbound.TransformRequest(t.Context(), req)
				require.NoError(t, err)
				var sent MessageRequest
				require.NoError(t, json.Unmarshal(wire.Body, &sent))
				require.Len(t, sent.Messages, 2)
				require.Len(t, sent.Messages[1].Content.MultipleContent, 1)
				result := sent.Messages[1].Content.MultipleContent[0]
				require.Equal(t, "tool_result", result.Type)
				require.Equal(t, "call_capture", lo.FromPtr(result.ToolUseID))
				require.True(t, lo.FromPtr(result.IsError))
				require.NotNil(t, result.CacheControl)
				require.Equal(t, wantTTL, result.CacheControl.TTL)
				require.NotNil(t, result.Content)
				require.Len(t, result.Content.MultipleContent, 2)
				require.Equal(t, "capture result", lo.FromPtr(result.Content.MultipleContent[0].Text))
				require.Equal(t, "aW1hZ2U=", result.Content.MultipleContent[1].Source.Data)
				for _, part := range result.Content.MultipleContent {
					require.Nil(t, part.CacheControl)
				}
				after, err := json.Marshal(req)
				require.NoError(t, err)
				require.JSONEq(t, string(before), string(after))
			})
		}
	}
}

func TestCacheOptimizerDoesNotPlaceBreakpointsInsideToolResults(t *testing.T) {
	req := &MessageRequest{
		System: &SystemPrompt{MultiplePrompts: []SystemPromptPart{{Type: "text", Text: "system", CacheControl: &CacheControl{Type: "ephemeral"}}}},
		Messages: []MessageParam{{Role: "user", Content: MessageContent{MultipleContent: []MessageContentBlock{{
			Type: "tool_result", ToolUseID: lo.ToPtr("call_capture"),
			Content: &MessageContent{MultipleContent: []MessageContentBlock{{Type: "text", Text: lo.ToPtr("result")}}},
		}}}}},
	}
	optimizeCacheControl(req)
	result := req.Messages[0].Content.MultipleContent[0]
	require.NotNil(t, result.CacheControl)
	require.Nil(t, result.Content.MultipleContent[0].CacheControl)
	require.Equal(t, 2, countCacheControls(req))
}
