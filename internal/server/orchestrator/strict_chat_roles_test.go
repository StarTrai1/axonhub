package orchestrator

import (
	"bytes"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
)

func TestStrictChatRolesAfterPassThrough(t *testing.T) {
	const original = `{"model":"alias","precision":9007199254740993,"messages":[{"role":"user","content":"earlier"},{"role":"assistant","content":"answer"},{"role":"developer","name":"notice","extra":{"n":9007199254740993},"content":[{"type":"text","text":"keep this"},{"type":"image_url","image_url":{"url":"https://example.com/image.png"}}]},{"role":"user","content":"next"}]}`
	for _, selectedType := range []channel.Type{channel.TypeDeepseek, channel.TypeMoonshot, channel.TypeZai, channel.TypeZhipu} {
		t.Run(string(selectedType), func(t *testing.T) {
			raw := &httpclient.Request{Body: []byte(original)}
			outbound := &PersistentOutboundTransformer{state: &PersistenceState{
				CurrentCandidate: &ChannelModelsCandidate{Channel: &biz.Channel{Channel: &ent.Channel{
					Type:     selectedType,
					Settings: &objects.ChannelSettings{PassThroughBody: lo.ToPtr(true)},
				}}},
				LlmRequest: &llm.Request{Model: "actual-model", APIFormat: llm.APIFormatOpenAIChatCompletion, RawRequest: raw},
			}}
			wire := &httpclient.Request{URL: "https://relay.example/v1/chat/completions", APIFormat: string(llm.APIFormatOpenAIChatCompletion), Body: []byte(`{"messages":[]}`)}
			wire, err := applyPassThroughRequestBody(outbound, nil).OnOutboundRawRequest(t.Context(), wire)
			require.NoError(t, err)
			require.True(t, outbound.state.PassThroughApplied)
			wire, err = applyStrictChatRoles(outbound).OnOutboundRawRequest(t.Context(), wire)
			require.NoError(t, err)
			expected := bytes.Replace([]byte(original), []byte(`"alias"`), []byte(`"actual-model"`), 1)
			expected = bytes.Replace(expected, []byte(`"developer"`), []byte(`"system"`), 1)
			require.Equal(t, expected, wire.Body)
			require.Equal(t, original, string(raw.Body))
			require.Equal(t, "system", gjson.GetBytes(wire.Body, "messages.2.role").String())

			// A later attempt on an ordinary OpenAI channel starts with the original role.
			outbound.state.CurrentCandidate.Channel.Type = channel.TypeOpenai
			wire = &httpclient.Request{URL: "https://api.openai.com/v1/chat/completions", APIFormat: string(llm.APIFormatOpenAIChatCompletion)}
			wire, err = applyPassThroughRequestBody(outbound, nil).OnOutboundRawRequest(t.Context(), wire)
			require.NoError(t, err)
			wire, err = applyStrictChatRoles(outbound).OnOutboundRawRequest(t.Context(), wire)
			require.NoError(t, err)
			require.Equal(t, "developer", gjson.GetBytes(wire.Body, "messages.2.role").String())
		})
	}
}

func TestStrictChatRolesDestinationAndProtocol(t *testing.T) {
	for _, test := range []struct {
		name   string
		url    string
		format llm.APIFormat
		want   string
	}{
		{"deepseek", "https://api.deepseek.com/v1/chat/completions", llm.APIFormatOpenAIChatCompletion, "system"},
		{"kimi", "https://API.KIMI.COM:443/coding/v1/chat/completions", llm.APIFormatOpenAIChatCompletion, "system"},
		{"moonshot cn", "https://api.moonshot.cn/v1/chat/completions", llm.APIFormatOpenAIChatCompletion, "system"},
		{"moonshot ai", "https://api.moonshot.ai/v1/chat/completions", llm.APIFormatOpenAIChatCompletion, "system"},
		{"zhipu", "https://open.bigmodel.cn/api/paas/v4/chat/completions", llm.APIFormatOpenAIChatCompletion, "system"},
		{"zai", "https://api.z.ai/api/paas/v4/chat/completions", llm.APIFormatOpenAIChatCompletion, "system"},
		{"suffix is not provider", "https://api.z.ai.example/v1/chat/completions", llm.APIFormatOpenAIChatCompletion, "developer"},
		{"provider in path", "https://example.com/api.deepseek.com", llm.APIFormatOpenAIChatCompletion, "developer"},
		{"responses unchanged", "https://api.deepseek.com/v1/responses", llm.APIFormatOpenAIResponse, "developer"},
		{"anthropic unchanged", "https://api.z.ai/v1/messages", llm.APIFormatAnthropicMessage, "developer"},
	} {
		t.Run(test.name, func(t *testing.T) {
			outbound := &PersistentOutboundTransformer{state: &PersistenceState{
				CurrentCandidate: &ChannelModelsCandidate{Channel: &biz.Channel{Channel: &ent.Channel{Type: channel.TypeOpenai}}},
			}}
			request := &httpclient.Request{URL: test.url, APIFormat: string(test.format), Body: []byte(`{"model":"deepseek-alias","messages":[{"role":"developer","content":"instruction"}]}`)}
			result, err := applyStrictChatRoles(outbound).OnOutboundRawRequest(t.Context(), request)
			require.NoError(t, err)
			require.Equal(t, test.want, gjson.GetBytes(result.Body, "messages.0.role").String())
		})
	}
}
