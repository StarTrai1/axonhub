package anthropic

import (
	"encoding/json"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
)

func TestManagedCacheControlKeepsToolResultBoundary(t *testing.T) {
	for _, raw := range []bool{false, true} {
		t.Run(map[bool]string{false: "typed", true: "raw"}[raw], func(t *testing.T) {
			content := MessageContent{MultipleContent: []MessageContentBlock{{Type: "tool_result", Content: &MessageContent{MultipleContent: []MessageContentBlock{{Type: "text", Text: lo.ToPtr("exact result")}}}}}}
			if raw {
				content = MessageContent{Raw: json.RawMessage(`[{"type":"tool_result","tool_use_id":"call","content":[{"type":"text","text":"exact result","vendor":"preserve"}]}]`)}
			request := &MessageRequest{Messages: []MessageParam{{Role: "user", Content: content}}, CacheControl: &CacheControl{Type: "ephemeral"}}
			require.True(t, moveTopLevelCacheControlToLastBlock(request))
			encoded, err := json.Marshal(request.Messages[0].Content)
			require.NoError(t, err)
			var blocks []map[string]any
			require.NoError(t, json.Unmarshal(encoded, &blocks))
			require.Contains(t, blocks[0], "cache_control")
			inner := blocks[0]["content"].([]any)[0].(map[string]any)
			require.NotContains(t, inner, "cache_control")
			require.Equal(t, "exact result", inner["text"])
			if raw {
				require.Equal(t, "preserve", inner["vendor"])
			}
		})
	}
}

func TestManagedCacheControlDoesNotSelectDeferredTools(t *testing.T) {
	request := &MessageRequest{Tools: []Tool{{Name: "deferred", DeferLoading: lo.ToPtr(true)}}, CacheControl: &CacheControl{Type: "ephemeral"}}
	require.False(t, moveTopLevelCacheControlToLastBlock(request))
	require.Nil(t, request.Tools[0].CacheControl)
}
