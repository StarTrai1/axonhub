package orchestrator

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
)

func TestOpenCodeDeepSeekRolesUseMappedModelAndSelectedDestination(t *testing.T) {
	for _, test := range []struct { model, endpoint string; kind channel.Type; role string }{
		{"deepseek-v4.1-flash", "https://relay.example/v1/chat/completions", channel.TypeOpencodeGo, "system"},
		{"deepseek-v4.1-flash", "https://opencode.ai/zen/v1/chat/completions", channel.TypeOpenai, "system"},
		{"gpt-6.1-sol", "https://opencode.ai/zen/v1/chat/completions", channel.TypeOpencodeGo, "developer"},
		{"deepseek-v4.1-flash", "https://opencode.ai.example/v1/chat/completions", channel.TypeOpenai, "developer"},
	} {
		body, err := sjson.Set(`{"model":"alias","seed":9007199254740993,"messages":[{"role":"user","content":"before"},{"role":"developer","content":"keep exact text"}]}`, "model", test.model)
		require.NoError(t, err)
		outbound := &PersistentOutboundTransformer{state:&PersistenceState{CurrentCandidate:&ChannelModelsCandidate{Channel:&biz.Channel{Channel:&ent.Channel{Type:test.kind}}}}}
		request := &httpclient.Request{URL:test.endpoint, APIFormat:string(llm.APIFormatOpenAIChatCompletion), Body:[]byte(body)}
		got, err := applyStrictChatRoles(outbound).OnOutboundRawRequest(t.Context(), request)
		require.NoError(t, err)
		require.Equal(t, test.role, gjson.GetBytes(got.Body,"messages.1.role").String())
		require.Equal(t, "9007199254740993", gjson.GetBytes(got.Body,"seed").Raw)
		require.Equal(t, "keep exact text", gjson.GetBytes(got.Body,"messages.1.content").String())
	}
}
