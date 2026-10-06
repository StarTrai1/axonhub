package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer"
	"github.com/looplj/axonhub/llm/transformer/gemini"
)

func TestDocumentRequestConversion(t *testing.T) {
	for _, test := range []struct {
		name   string
		source string
		path   string
		value  string
	}{
		{"base64", `{"type":"base64","media_type":"application/pdf","data":"JVBERi0xLjQ="}`, "inlineData.data", "JVBERi0xLjQ="},
		{"url", `{"type":"url","url":"https://example.com/report.pdf"}`, "fileData.fileUri", "https://example.com/report.pdf"},
	} {
		t.Run(test.name, func(t *testing.T) {
			document := `{"type":"document","source":` + test.source + `,"title":"Report","context":"Reference only","citations":{"enabled":true},"cache_control":{"type":"ephemeral","ttl":"1h"}}`
			body := []byte(`{"model":"gemini-3-pro","max_tokens":1024,"messages":[{"role":"user","content":[` + document + `,{"type":"text","text":"Summarize"}]}]}`)
			request, err := NewInboundTransformer().TransformRequest(context.Background(), &httpclient.Request{
				Headers: http.Header{"Content-Type": {"application/json"}}, Body: body,
			})
			require.NoError(t, err)
			require.Len(t, request.Messages, 1)
			require.Len(t, request.Messages[0].Content.MultipleContent, 2)
			require.Equal(t, "document", request.Messages[0].Content.MultipleContent[0].Type)
			require.Equal(t, "1h", request.Messages[0].Content.MultipleContent[0].CacheControl.TTL)

			outbound, err := gemini.NewOutboundTransformer("https://example.com", "synthetic-key")
			require.NoError(t, err)
			converted, err := outbound.TransformRequest(context.Background(), request)
			require.NoError(t, err)
			require.Equal(t, test.value, gjson.GetBytes(converted.Body, "contents.0.parts.0."+test.path).String())
			require.Equal(t, "Summarize", gjson.GetBytes(converted.Body, "contents.0.parts.1.text").String())

			// JSON persistence must retain the document metadata for native replay.
			persisted, err := json.Marshal(request)
			require.NoError(t, err)
			require.NoError(t, json.Unmarshal(persisted, request))
			restored, err := json.Marshal(convertToAnthropicRequest(request))
			require.NoError(t, err)
			require.JSONEq(t, document, gjson.GetBytes(restored, "messages.0.content.0").Raw)
		})
	}
}

func TestDocumentToolResultConversion(t *testing.T) {
	document := `{"type":"document","source":{"type":"base64","media_type":"application/pdf","data":"JVBERi0xLjQ="},"title":"Tool attachment","citations":{"enabled":false}}`
	var native MessageRequest
	require.NoError(t, json.Unmarshal([]byte(`{"model":"claude-sonnet-5-5","max_tokens":1024,"messages":[{"role":"assistant","content":[{"type":"tool_use","id":"call_doc","name":"read_file","input":{}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_doc","content":[`+document+`]}]}]}`), &native))
	request, err := convertToLLMRequest(&native)
	require.NoError(t, err)
	require.Len(t, request.Messages, 2)
	require.Equal(t, "tool", request.Messages[1].Role)
	require.Equal(t, "document", request.Messages[1].Content.MultipleContent[0].Type)
	restored, err := json.Marshal(convertToAnthropicRequest(request))
	require.NoError(t, err)
	require.JSONEq(t, document, gjson.GetBytes(restored, "messages.1.content.0.content.0").Raw)
}

func TestDocumentInvalidSourceDoesNotDisappear(t *testing.T) {
	for _, source := range []string{`null`, `{"type":"base64","media_type":"application/pdf"}`, `{"type":"url"}`, `{"type":"file","file_id":"file_private"}`} {
		t.Run(source, func(t *testing.T) {
			_, err := NewInboundTransformer().TransformRequest(context.Background(), &httpclient.Request{
				Headers: http.Header{"Content-Type": {"application/json"}},
				Body:    []byte(`{"model":"gemini-3-pro","max_tokens":1024,"messages":[{"role":"user","content":[{"type":"document","source":` + source + `},{"type":"text","text":"Summarize"}]}]}`),
			})
			require.ErrorIs(t, err, transformer.ErrInvalidRequest)
		})
	}
}

func TestDocumentCacheControlAndBlockReuse(t *testing.T) {
	var block MessageContentBlock
	require.NoError(t, json.Unmarshal([]byte(`{"type":"document","source":{"type":"url","url":"https://example.com/report.pdf"},"citations":{"enabled":true},"cache_control":{"type":"ephemeral"}}`), &block))
	block.CacheControl = nil
	encoded, err := json.Marshal(block)
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(encoded, "cache_control").Exists())
	require.True(t, gjson.GetBytes(encoded, "citations.enabled").Bool())
	require.NoError(t, json.Unmarshal([]byte(`{"type":"text","text":"next"}`), &block))
	require.Empty(t, block.RawDocument)
	require.Nil(t, block.Source)
}

func TestOutboundDocumentOnlyRequest(t *testing.T) {
	outbound, err := NewOutboundTransformer("https://example.com", "synthetic-key")
	require.NoError(t, err)
	request, err := outbound.TransformRequest(t.Context(), &llm.Request{
		Model:     "claude-sonnet-5-5",
		MaxTokens: lo.ToPtr(int64(1024)),
		Messages: []llm.Message{{
			Role: "user",
			Content: llm.MessageContent{MultipleContent: []llm.MessageContentPart{{
				Type:     "document",
				Document: &llm.DocumentURL{URL: "data:application/pdf;base64,JVBERi0=", MIMEType: "application/pdf"},
			}}},
		}},
	})
	require.NoError(t, err)
	require.Equal(t, "document", gjson.GetBytes(request.Body, "messages.0.content.0.type").String())
	require.Equal(t, "JVBERi0=", gjson.GetBytes(request.Body, "messages.0.content.0.source.data").String())
}
