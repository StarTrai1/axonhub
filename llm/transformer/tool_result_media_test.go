package transformer_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer/openai"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

func TestResponsesToolOutputFilesStayOutOfChatToolMessages(t *testing.T) {
	const raw = `{"model":"gpt-6-astra","input":[
		{"type":"function_call","call_id":"call_file","name":"fetch","arguments":"{}"},
		{"type":"function_call","call_id":"call_image","name":"capture","arguments":"{}"},
		{"type":"function_call_output","call_id":"call_file","output":[{"type":"input_text","text":"document attached"},{"type":"input_file","filename":"result.pdf","file_data":"data:application/pdf;base64,cGRm"}]},
		{"type":"function_call_output","call_id":"call_image","output":[{"type":"input_image","image_url":"https://example.com/result.png"}]},
		{"role":"user","content":"continue"}
	]}`
	request, err := responses.NewInboundTransformer().TransformRequest(t.Context(), &httpclient.Request{Body: []byte(raw), Headers: http.Header{"Content-Type": {"application/json"}}})
	require.NoError(t, err)
	before, err := json.Marshal(request.Messages)
	require.NoError(t, err)
	outbound, err := openai.NewOutboundTransformer("https://example.com/v1", "test-key")
	require.NoError(t, err)
	wire, err := outbound.TransformRequest(t.Context(), request)
	require.NoError(t, err)
	var sent openai.Request
	require.NoError(t, json.Unmarshal(wire.Body, &sent))
	var toolIndexes []int
	for i, message := range sent.Messages {
		if message.Role == "tool" {
			toolIndexes = append(toolIndexes, i)
			require.NotEmpty(t, lo.FromPtr(message.Content.Content))
			require.Empty(t, message.Content.MultipleContent)
		}
	}
	require.Len(t, toolIndexes, 2)
	require.Equal(t, toolIndexes[0]+1, toolIndexes[1])
	require.Equal(t, "document attached", lo.FromPtr(sent.Messages[toolIndexes[0]].Content.Content))
	mediaMessage := sent.Messages[toolIndexes[1]+1]
	require.Equal(t, "user", mediaMessage.Role)
	parts := mediaMessage.Content.MultipleContent
	require.Len(t, parts, 4)
	require.Contains(t, lo.FromPtr(parts[0].Text), "call_file")
	require.Equal(t, "file", parts[1].Type)
	require.Equal(t, "data:application/pdf;base64,cGRm", parts[1].File.FileData)
	require.Equal(t, "result.pdf", parts[1].File.Filename)
	require.Contains(t, lo.FromPtr(parts[2].Text), "call_image")
	require.Equal(t, "https://example.com/result.png", parts[3].ImageURL.URL)
	after, err := json.Marshal(request.Messages)
	require.NoError(t, err)
	require.JSONEq(t, string(before), string(after))
}
