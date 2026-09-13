package responses

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline/cc"
)

func TestResponsesOutboundPreservesMidConversationInstructions(t *testing.T) {
	outbound, err := NewOutboundTransformer("https://example.com/v1", "test-key")
	require.NoError(t, err)
	request := &llm.Request{
		Model:     "gpt-6-astra",
		APIFormat: llm.APIFormatAnthropicMessage,
		RawRequest: &httpclient.Request{
			Headers: http.Header{"User-Agent": []string{"claude-cli/2.1.231"}},
		},
		Messages: []llm.Message{
			{Role: "system", Content: llm.MessageContent{Content: lo.ToPtr("base instructions")}},
			{Role: "system", Content: llm.MessageContent{Content: lo.ToPtr("shared rules")}},
			{Role: "user", Content: llm.MessageContent{Content: lo.ToPtr("earlier question")}},
			{Role: "assistant", Content: llm.MessageContent{Content: lo.ToPtr("earlier answer")}},
			{Role: "system", Content: llm.MessageContent{Content: lo.ToPtr("new requirement")}},
			{Role: "developer", Content: llm.MessageContent{Content: lo.ToPtr("follow-up guidance")}},
			{Role: "user", Content: llm.MessageContent{Content: lo.ToPtr("continue")}},
		},
	}
	original, err := json.Marshal(request.Messages)
	require.NoError(t, err)
	prepared, err := cc.SystemCacheCompatibility().OnOutboundLlmRequest(t.Context(), request, llm.APIFormatOpenAIResponse)
	require.NoError(t, err)
	wire, err := outbound.TransformRequest(t.Context(), prepared)
	require.NoError(t, err)
	var sent Request
	require.NoError(t, json.Unmarshal(wire.Body, &sent))
	require.Equal(t, "base instructions\nshared rules", sent.Instructions)
	require.Len(t, sent.Input.Items, 5)
	require.Equal(t, []string{"user", "assistant", "system", "developer", "user"}, lo.Map(sent.Input.Items, func(item Item, _ int) string { return item.Role }))
	require.Equal(t, "new requirement", *sent.Input.Items[2].Content.Items[0].Text)
	require.Equal(t, "follow-up guidance", *sent.Input.Items[3].Content.Items[0].Text)
	after, err := json.Marshal(request.Messages)
	require.NoError(t, err)
	require.Equal(t, original, after)
}

func TestResponsesNativeInstructionOrderRoundTrip(t *testing.T) {
	body := []byte(`{"model":"gpt-6-astra","instructions":"stable prefix","input":[{"role":"user","content":"earlier question"},{"role":"assistant","content":"answer"},{"type":"configuration_update","reasoning":{"effort":"high"}},{"role":"system","content":"new requirement"},{"role":"user","content":"continue"}]}`)
	request, err := NewInboundTransformer().TransformRequest(t.Context(), &httpclient.Request{Body: body})
	require.NoError(t, err)
	outbound, err := NewOutboundTransformer("https://example.com/v1", "test-key")
	require.NoError(t, err)
	wire, err := outbound.TransformRequest(t.Context(), request)
	require.NoError(t, err)
	var sent Request
	require.NoError(t, json.Unmarshal(wire.Body, &sent))
	require.Equal(t, "stable prefix", sent.Instructions)
	require.Len(t, sent.Input.Items, 5)
	require.Equal(t, "configuration_update", sent.Input.Items[2].Type)
	require.Equal(t, "system", sent.Input.Items[3].Role)
	require.Equal(t, "new requirement", *sent.Input.Items[3].Content.Items[0].Text)
}

func TestResponsesSingleDeveloperMessageRetainsRole(t *testing.T) {
	input := convertInputFromMessages([]llm.Message{{Role: "developer", Content: llm.MessageContent{Content: lo.ToPtr("instructions")}}}, llm.TransformOptions{})
	require.Nil(t, input.Text)
	require.Len(t, input.Items, 1)
	require.Equal(t, "developer", input.Items[0].Role)
}
