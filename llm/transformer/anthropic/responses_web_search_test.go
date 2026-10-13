package anthropic

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm/auth"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

func TestAnthropicNamedHostedSearchUsesResponsesAllowlist(t *testing.T) {
	out, err := responses.NewOutboundTransformerWithConfig(&responses.Config{BaseURL: "https://relay.example/v1", APIKeyProvider: auth.NewStaticKeyProvider("fixture")})
	require.NoError(t, err)
	for _, kind := range []string{"web_search_20250305", "web_search_20260209", ""} {
		t.Run(kind, func(t *testing.T) {
			body := fmt.Sprintf(`{"model":"gpt-6.1-sol","max_tokens":128,"messages":[{"role":"user","content":"search"}],"tools":[{"type":%q,"name":"web_search","input_schema":{"type":"object"}},{"name":"other","input_schema":{"type":"object"}}],"tool_choice":{"type":"tool","name":"web_search"}}`, kind)
			req, err := NewInboundTransformer().TransformRequest(t.Context(), &httpclient.Request{Body: []byte(body), Headers: http.Header{"Content-Type": {"application/json"}}})
			require.NoError(t, err)
			encoded, err := out.TransformRequest(t.Context(), req)
			require.NoError(t, err)
			want := `{"type":"allowed_tools","mode":"required","tools":[{"type":"web_search"}]}`
			if kind == "" {
				want = `{"type":"function","name":"web_search"}`
			}
			require.JSONEq(t, want, gjson.GetBytes(encoded.Body, "tool_choice").Raw)
			native := convertToAnthropicRequestWithConfig(req, &Config{Type: PlatformDirect})
			require.Equal(t, "tool", native.ToolChoice.Type)
			require.Equal(t, "web_search", *native.ToolChoice.Name)
		})
	}
}

func TestResponsesHostedSearchAnthropicTerminalAndReplay(t *testing.T) {
	out, err := responses.NewOutboundTransformerWithConfig(&responses.Config{BaseURL: "https://relay.example/v1", APIKeyProvider: auth.NewStaticKeyProvider("fixture")})
	require.NoError(t, err)
	for _, status := range []string{"completed", "failed", "incomplete", "in_progress"} {
		for _, streaming := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%v", status, streaming), func(t *testing.T) {
				item := fmt.Sprintf(`{"type":"web_search_call","id":"ws_original-id","status":%q,"action":{"type":"search","queries":["first","second"],"sources":[{"url":"https://invented.example"}]},"results":[{"url":"https://result.example","title":"Result","encrypted_content":"opaque-fixture"},{"url":"https://unreplayable.example"}],"error":{"code":"max_uses_exceeded"}}`, status)
				textItem := `{"type":"message","id":"msg_1","role":"assistant","content":[{"type":"output_text","text":"answer"}]}`
				body := `{"id":"resp_1","model":"gpt-6.1-sol","status":"completed","output":[` + item + `,` + textItem + `]}`
				var content []MessageContentBlock
				if !streaming {
					unified, err := out.TransformResponse(t.Context(), &httpclient.Response{StatusCode: 200, Body: []byte(body)})
					require.NoError(t, err)
					content = convertToAnthropicResponse(unified).Content
				} else {
					events := []*httpclient.StreamEvent{
						{Type: "response.created", Data: []byte(`{"type":"response.created","response":{"id":"resp_1","model":"gpt-6.1-sol"}}`)},
						{Type: "response.output_item.done", Data: []byte(`{"type":"response.output_item.done","output_index":0,"item":` + item + `}`)},
						{Type: "response.output_text.delta", Data: []byte(`{"type":"response.output_text.delta","item_id":"msg_1","output_index":1,"content_index":0,"delta":"answer"}`)},
						{Type: "response.completed", Data: []byte(`{"type":"response.completed","response":` + body + `}`)},
					}
					unified, err := out.TransformStream(t.Context(), nil, streams.SliceStream(events))
					require.NoError(t, err)
					encoded, err := NewInboundTransformer().TransformStream(t.Context(), unified)
					require.NoError(t, err)
					for encoded.Next() {
						var event StreamEvent
						require.NoError(t, json.Unmarshal(encoded.Current().Data, &event))
						if event.Type == "content_block_start" && event.ContentBlock != nil {
							content = append(content, *event.ContentBlock)
						}
					}
					require.NoError(t, encoded.Err())
					require.NoError(t, encoded.Close())
				}
				if status == "in_progress" {
					require.Len(t, content, 1)
					return
				}
				require.Len(t, content, 3, "search pair emitted once even when metadata is repeated at completion")
				require.Equal(t, "server_tool_use", content[0].Type)
				require.Equal(t, "web_search_tool_result", content[1].Type)
				require.Equal(t, content[0].ID, *content[1].ToolUseID)
				require.JSONEq(t, `{"query":"first second"}`, string(content[0].Input))
				result, err := json.Marshal(content[1].Content)
				require.NoError(t, err)
				if status == "completed" {
					require.JSONEq(t, `[{"type":"web_search_result","url":"https://result.example","title":"Result","encrypted_content":"opaque-fixture"}]`, string(result))
				} else {
					require.JSONEq(t, `{"type":"web_search_tool_result_error","error_code":"max_uses_exceeded"}`, string(result))
				}
				// Replay emitted search blocks as hosted history, preserving the upstream ID.
				unified, err := convertToLLMRequest(&MessageRequest{Model: "gpt-6.1-sol", MaxTokens: 128, Messages: []MessageParam{{Role: "assistant", Content: MessageContent{MultipleContent: content[:2]}}, {Role: "user", Content: MessageContent{Content: new("continue")}}}})
				require.NoError(t, err)
				req, err := out.TransformRequest(t.Context(), unified)
				require.NoError(t, err)
				require.Equal(t, "web_search_call", gjson.GetBytes(req.Body, "input.0.type").String())
				require.Equal(t, "ws_original-id", gjson.GetBytes(req.Body, "input.0.id").String())
				require.NotContains(t, string(req.Body), "function_call")
			})
		}
	}
}

func TestResponsesHostedSearchEmptyAndUnknownFailure(t *testing.T) {
	for _, tc := range []struct{ status, want string }{{"completed", "[]"}, {"failed", `{"type":"web_search_tool_result_error","error_code":"unavailable"}`}} {
		var raw any
		require.NoError(t, json.Unmarshal([]byte(`[{"type":"web_search_call","id":"ws_1","status":"`+tc.status+`","results":null,"error":{"code":"internal_private"}}]`), &raw))
		blocks := responsesWebSearchBlocks(map[string]any{responsesWebSearchMetadataKey: raw}, nil)
		require.Len(t, blocks, 2)
		content, err := json.Marshal(blocks[1].Content)
		require.NoError(t, err)
		require.JSONEq(t, tc.want, string(content))
	}
}

func TestResponsesHostedSearchOnlyTerminalEnvelope(t *testing.T) {
 out,err:=responses.NewOutboundTransformerWithConfig(&responses.Config{BaseURL:"https://relay.example/v1",APIKeyProvider:auth.NewStaticKeyProvider("fixture")})
 require.NoError(t,err)
 unified,err:=out.TransformStream(t.Context(),nil,streams.SliceStream([]*httpclient.StreamEvent{{Type:"response.completed",Data:[]byte(`{"type":"response.completed","response":{"id":"resp_search","model":"gpt-6.1-sol","status":"completed","output":[{"type":"web_search_call","id":"ws_only","status":"completed","action":{"type":"search","query":"fixture"}}]}}`)}}))
 require.NoError(t,err)
 encoded,err:=NewInboundTransformer().TransformStream(t.Context(),unified)
 require.NoError(t,err)
 var blocks []string
 for encoded.Next() {
  result:=gjson.GetBytes(encoded.Current().Data,"content_block.type")
  if result.Exists() {blocks=append(blocks,result.String())}
 }
 require.NoError(t,encoded.Err())
 require.Equal(t,[]string{"server_tool_use","web_search_tool_result"},blocks)
 require.NoError(t,encoded.Close())
}
