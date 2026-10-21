package anthropic

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm/httpclient"
)

func TestSystemCacheSkipsEmptyTrailingBlocks(t *testing.T) {
	for _, automatic := range []bool{false, true} {
		t.Run(fmt.Sprint(automatic), func(t *testing.T) {
			top := ""
			if automatic {
				top = `,"cache_control":{"type":"ephemeral"}`
			}
			body := []byte(`{"model":"claude-sonnet-4-6","max_tokens":32,"system":[{"type":"text","text":"keep the actual instructions"},{"type":"text","text":" \n","cache_control":{"type":"ephemeral"}}],"messages":[{"role":"user","content":[{"type":"text","text":"continue","cache_control":{"type":"ephemeral"}}]}]` + top + `}`)
			req, err := NewInboundTransformer().TransformRequest(t.Context(), &httpclient.Request{Body: body})
			require.NoError(t, err)
			outbound, err := NewOutboundTransformer("https://example.invalid", "synthetic-cache-key")
			require.NoError(t, err)
			got, err := outbound.TransformRequest(t.Context(), req)
			require.NoError(t, err)
			require.False(t, gjson.GetBytes(got.Body, "system.1.cache_control").Exists())
			require.Equal(t, "keep the actual instructions", gjson.GetBytes(got.Body, "system.0.text").String())
			require.Equal(t, " \n", gjson.GetBytes(got.Body, "system.1.text").String(), "eligibility must not rewrite prompt text")
			require.Equal(t, automatic, gjson.GetBytes(got.Body, "cache_control").Exists())
			if !automatic {
				require.Equal(t, "ephemeral", gjson.GetBytes(got.Body, "system.0.cache_control.type").String())
			}
		})
	}
}

func TestManagedCacheDoesNotSelectEmptySystemBlock(t *testing.T) {
	request := &MessageRequest{
		System: &SystemPrompt{MultiplePrompts: []SystemPromptPart{
			{Type: "text", Text: "nonempty instructions"},
			{Type: "text", Text: " \n"},
		}},
		CacheControl: &CacheControl{Type: "ephemeral", TTL: "1h"},
	}
	require.True(t, moveTopLevelCacheControlToLastBlock(request))
	require.Equal(t, "1h", request.System.MultiplePrompts[0].CacheControl.TTL)
	require.Nil(t, request.System.MultiplePrompts[1].CacheControl)
}
