package gemini_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
	"github.com/looplj/axonhub/llm/transformer"
	"github.com/looplj/axonhub/llm/transformer/anthropic"
	"github.com/looplj/axonhub/llm/transformer/gemini"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

func TestGeminiPartlessRefusalToAnthropic(t *testing.T) {
	for _, reason := range []string{"SAFETY", "RECITATION", "PROHIBITED_CONTENT", "SPII", "BLOCKLIST", "MALFORMED_FUNCTION_CALL", "IMAGE_SAFETY", "IMAGE_PROHIBITED_CONTENT", "IMAGE_RECITATION"} {
		t.Run(reason, func(t *testing.T) {
			payload, err := json.Marshal(map[string]any{"responseId": "refusal", "candidates": []any{map[string]any{"finishReason": reason}}, "usageMetadata": map[string]any{"promptTokenCount": 20, "candidatesTokenCount": 1}})
			require.NoError(t, err)
			outbound, err := gemini.NewOutboundTransformer("", "fixture")
			require.NoError(t, err)
			unified, err := outbound.TransformResponse(t.Context(), &httpclient.Response{StatusCode: 200, Body: payload})
			require.NoError(t, err)
			inbound := anthropic.NewInboundTransformer()
			response, err := inbound.TransformResponse(t.Context(), unified)
			require.NoError(t, err)
			var message anthropic.Message
			require.NoError(t, json.Unmarshal(response.Body, &message))
			require.Equal(t, "refusal", *message.StopReason)

			stream, err := outbound.TransformStream(t.Context(), &httpclient.Request{}, streams.SliceStream([]*httpclient.StreamEvent{{Data: payload}}))
			require.NoError(t, err)
			converted, err := inbound.TransformStream(t.Context(), stream)
			require.NoError(t, err)
			defer converted.Close()
			var types []string
			for converted.Next() {
				event := converted.Current()
				types = append(types, event.Type)
				if event.Type == "message_delta" {
					var delta anthropic.StreamEvent
					require.NoError(t, json.Unmarshal(event.Data, &delta))
					require.Equal(t, "refusal", *delta.Delta.StopReason)
					require.EqualValues(t, 20, delta.Usage.InputTokens)
				}
			}
			require.NoError(t, converted.Err())
			require.Equal(t, []string{"message_start", "content_block_start", "content_block_stop", "message_delta", "message_stop"}, types)
		})
	}
}

func TestResponsesToolMediaToGemini(t *testing.T) {
	for _, model := range []string{"gemini-2.5-flash", "gemini-3-pro-preview", "models/gemini-3.5-flash", "custom-model", "gemini-"} {
		t.Run(model, func(t *testing.T) {
			body := []byte(`{"model":"fixture","input":[{"type":"function_call","call_id":"one","name":"inspect","arguments":"{}"},{"type":"function_call","call_id":"two","name":"read","arguments":"{}"},{"type":"function_call_output","call_id":"one","output":[{"type":"input_text","text":"image result"},{"type":"input_image","image_url":"data:image/png;base64,aW1hZ2U="}]},{"type":"function_call_output","call_id":"two","output":[{"type":"input_text","text":"document result"},{"type":"input_file","filename":"result.pdf","file_data":"data:application/pdf;base64,cGRm"}]}]}`)
			request, err := responses.NewInboundTransformer().TransformRequest(t.Context(), &httpclient.Request{Headers: http.Header{"Content-Type": []string{"application/json"}}, Body: body})
			require.NoError(t, err)
			request.Model = model
			outbound, err := gemini.NewOutboundTransformer("", "fixture")
			require.NoError(t, err)
			wire, err := outbound.TransformRequest(t.Context(), request)
			require.NoError(t, err)
			var result gemini.GenerateContentRequest
			require.NoError(t, json.Unmarshal(wire.Body, &result))
			last := result.Contents[len(result.Contents)-1]
			require.Equal(t, "user", last.Role)
			require.Equal(t, "inspect", last.Parts[0].FunctionResponse.Name)
			require.Equal(t, "read", last.Parts[1].FunctionResponse.Name)
			require.Equal(t, map[string]any{"result": "image result"}, last.Parts[0].FunctionResponse.Response)
			require.Equal(t, map[string]any{"result": "document result"}, last.Parts[1].FunctionResponse.Response)
			if model == "gemini-3-pro-preview" || model == "models/gemini-3.5-flash" {
				require.Len(t, last.Parts, 2)
				require.Equal(t, "aW1hZ2U=", last.Parts[0].FunctionResponse.Parts[0].InlineData.Data)
				require.Equal(t, "application/pdf", last.Parts[1].FunctionResponse.Parts[0].InlineData.MIMEType)
				// The native Gemini entrance must also retain these attached parts.
				wire.Path = "/v1beta/models/gemini-3-pro-preview:generateContent"
				replayed, err := gemini.NewInboundTransformer().TransformRequest(t.Context(), wire)
				require.NoError(t, err)
				require.Len(t, replayed.Messages[len(replayed.Messages)-1].Content.MultipleContent, 2)
				require.Equal(t, "document", replayed.Messages[len(replayed.Messages)-1].Content.MultipleContent[1].Type)
			} else {
				require.Len(t, last.Parts, 4)
				require.Equal(t, "image/png", last.Parts[2].InlineData.MIMEType)
				require.Equal(t, "application/pdf", last.Parts[3].InlineData.MIMEType)
			}
		})
	}
}

func TestGeminiToolMediaWithoutPortablePayloadIsRejected(t *testing.T) {
	adapter, err := gemini.NewOutboundTransformer("", "fixture")
	require.NoError(t, err)
	_, err = adapter.TransformRequest(t.Context(), &llm.Request{Model: "gemini-3-pro-preview", Messages: []llm.Message{{Role: "tool", Content: llm.MessageContent{MultipleContent: []llm.MessageContentPart{{Type: "document", Document: &llm.DocumentURL{FileID: "file_external"}}}}}}})
	require.ErrorIs(t, err, transformer.ErrInvalidRequest)
}

func TestAnthropicToolMediaKeepsRemoteFilesOutsideFunctionResponse(t *testing.T) {
	payload := []byte(`{"model":"claude-sonnet-5-5","max_tokens":1000,"messages":[{"role":"assistant","content":[{"type":"tool_use","id":"one","name":"inspect","input":{}},{"type":"tool_use","id":"two","name":"read","input":{}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"one","content":[{"type":"text","text":"remote image"},{"type":"image","source":{"type":"url","url":"https://example.com/image.png"}}]},{"type":"tool_result","tool_use_id":"two","content":[{"type":"document","source":{"type":"base64","media_type":"application/pdf","data":"cGRm"}}]}]}]}`)
	request, err := anthropic.NewInboundTransformer().TransformRequest(t.Context(), &httpclient.Request{Headers: http.Header{"Content-Type": []string{"application/json"}}, Body: payload})
	require.NoError(t, err)
	request.Model = "gemini-3-pro-preview"
	adapter, err := gemini.NewOutboundTransformer("", "fixture")
	require.NoError(t, err)
	wire, err := adapter.TransformRequest(t.Context(), request)
	require.NoError(t, err)
	var result gemini.GenerateContentRequest
	require.NoError(t, json.Unmarshal(wire.Body, &result))
	parts := result.Contents[len(result.Contents)-1].Parts
	require.Len(t, parts, 3)
	require.Equal(t, "one", parts[0].FunctionResponse.ID)
	require.Equal(t, "two", parts[1].FunctionResponse.ID)
	require.Empty(t, parts[0].FunctionResponse.Parts)
	require.Equal(t, "cGRm", parts[1].FunctionResponse.Parts[0].InlineData.Data)
	require.Equal(t, "https://example.com/image.png", parts[2].FileData.FileURI)
}
