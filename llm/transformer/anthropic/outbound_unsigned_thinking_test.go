package anthropic

import (
	"encoding/json"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm"
)

func TestTranslatedClaudeHistoryOmitsUnsignedThinking(t *testing.T) {
	for _, format := range []llm.APIFormat{llm.APIFormatOpenAIChatCompletion, llm.APIFormatOpenAIResponse, llm.APIFormatOpenAIResponseWebSocket} {
		t.Run(format.String(), func(t *testing.T) {
			src := &llm.Request{
				APIFormat: format, Model: "claude-opus-5-5",
				Messages: []llm.Message{
					{Role: "user", Content: llm.MessageContent{Content: lo.ToPtr("Look up the value")}},
					{Role: "assistant", ReasoningContent: lo.ToPtr("unsigned summary")},
					{Role: "assistant", ReasoningContent: lo.ToPtr("plan"), Content: llm.MessageContent{Content: lo.ToPtr("Checking")}, ToolCalls: []llm.ToolCall{
						{ID: "call_lookup", Type: "function", Function: llm.FunctionCall{Name: "lookup", Arguments: `{"key":"a"}`}},
					}},
					{Role: "tool", ToolCallID: lo.ToPtr("call_lookup"), Content: llm.MessageContent{Content: lo.ToPtr("42")}},
				},
			}
			before, err := json.Marshal(src)
			require.NoError(t, err)
			got := convertToAnthropicRequestWithConfig(src, &Config{Type: PlatformDirect})
			body, err := json.Marshal(got)
			require.NoError(t, err)
			require.Len(t, got.Messages, 3)
			require.NotContains(t, string(body), `"thinking"`)
			require.Contains(t, string(body), `"Checking"`)
			require.Contains(t, string(body), `"id":"call_lookup"`)
			require.Contains(t, string(body), `"tool_use_id":"call_lookup"`)
			require.Contains(t, string(body), `"key":"a"`)
			require.Contains(t, string(body), `42`)
			after, err := json.Marshal(src)
			require.NoError(t, err)
			require.JSONEq(t, string(before), string(after))
		})
	}
}

func TestTranslatedThinkingPreservesSignaturesAndOtherProviders(t *testing.T) {
	for _, tc := range []struct {
		name, model, signature string
		platform               PlatformType
		format                 llm.APIFormat
	}{
		{name: "signed Claude", model: "claude-opus-5-5", platform: PlatformDirect, signature: "Y2xhdWRlLW9wdXMtNS01LXN5bnRoZXRpYy1zaWduYXR1cmU=", format: llm.APIFormatOpenAIResponse},
		{name: "DeepSeek", model: "deepseek-reasoner", platform: PlatformDeepSeek, format: llm.APIFormatOpenAIChatCompletion},
		{name: "native Messages", model: "claude-opus-5-5", platform: PlatformDirect, format: llm.APIFormatAnthropicMessage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := &llm.Request{APIFormat: tc.format, Model: tc.model, Messages: []llm.Message{{
				Role: "assistant", ReasoningContent: lo.ToPtr("plan"), ReasoningSignature: lo.EmptyableToPtr(tc.signature),
				RedactedReasoningContent: lo.ToPtr("opaque-redacted-reasoning"),
				Content: llm.MessageContent{Content: lo.ToPtr("answer")},
			}}}
			body, err := json.Marshal(convertToAnthropicRequestWithConfig(src, &Config{Type: tc.platform}))
			require.NoError(t, err)
			require.Contains(t, string(body), `"thinking"`)
			require.Contains(t, string(body), `"data":"opaque-redacted-reasoning"`)
			if tc.signature != "" {
				require.Equal(t, tc.signature, gjson.GetBytes(body, "messages.0.content.0.signature").String())
			}
		})
	}
}
