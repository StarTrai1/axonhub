package responses

import (
 "encoding/json"
 "strings"
 "testing"

 "github.com/samber/lo"
 "github.com/stretchr/testify/require"
 "github.com/tidwall/gjson"

 "github.com/looplj/axonhub/llm"
 "github.com/looplj/axonhub/llm/httpclient"
 "github.com/looplj/axonhub/llm/streams"
)

func TestReasoningOutputUsesProtocolIdentityInHTTPAndStream(t *testing.T) {
 items := buildReasoningItems(llm.Message{ReasoningContent: lo.ToPtr("visible reasoning")})
 require.Len(t, items, 1)
 require.True(t, strings.HasPrefix(items[0].ID, "rs_"))
 native := buildReasoningItems(llm.Message{ReasoningItems: []llm.ReasoningItem{{ID: "rs_native", Content: "summary", Signature: "opaque"}}})
 require.Equal(t, "rs_native", native[0].ID)
 require.Equal(t, "opaque", *native[0].EncryptedContent)
 stream, err := NewInboundTransformer().TransformStream(t.Context(), streams.SliceStream([]*llm.Response{
  {ID: "resp_test", Object: "chat.completion.chunk", Model: "gpt-6.1-terra", Choices: []llm.Choice{{Index: 0, Delta: &llm.Message{Role: "assistant", ReasoningContent: lo.ToPtr("visible reasoning")}}}},
  {ID: "resp_test", Object: "chat.completion.chunk", Model: "gpt-6.1-terra", Choices: []llm.Choice{{Index: 0, Delta: &llm.Message{Content: llm.MessageContent{Content: lo.ToPtr("answer")}}, FinishReason: lo.ToPtr("stop")}}},
 }))
 require.NoError(t, err)
 var id string
 seen := map[string]bool{}
 for stream.Next() {
  data := stream.Current().Data
  typ := gjson.GetBytes(data, "type").String()
  item := gjson.GetBytes(data, "item")
  if item.Get("type").String() == "reasoning" {
   if id == "" { id = item.Get("id").String() }
   require.Equal(t, id, item.Get("id").String()); seen[typ] = true
  }
  if strings.Contains(typ, "reasoning_summary") { require.Equal(t, id, gjson.GetBytes(data, "item_id").String()) }
  for _, output := range gjson.GetBytes(data, "response.output").Array() {
   if output.Get("type").String() == "reasoning" { require.Equal(t, id, output.Get("id").String()); seen[typ] = true }
  }
 }
 require.NoError(t, stream.Err()); require.NoError(t, stream.Close())
 require.True(t, strings.HasPrefix(id, "rs_"))
 for _, typ := range []string{"response.output_item.added", "response.output_item.done", "response.completed"} { require.True(t, seen[typ], typ) }
}

func TestLegacyReasoningIDsOnRelayPreserveOpaqueHistory(t *testing.T) {
 body := []byte(`{"model":"gpt-6-astra","prompt_cache_key":"stable","input":[{"type":"compaction","id":"cmp_native","encrypted_content":"checkpoint"},{"type":"reasoning","id":"rs_native","encrypted_content":"native"},{"type":"reasoning","id":"item_9LAvofCOR76ckkeR","summary":[{"type":"summary_text","text":"visible"}],"encrypted_content":null},{"type":"reasoning","id":"item_encrypted","encrypted_content":"opaque"},{"type":"reasoning","id":"item_referenced","summary":[]},{"type":"item_reference","id":"item_referenced"},{"type":"function_call_output","call_id":"call_1","output":"{\"id\":\"item_business\"}"}]}`)
 for _, transport := range []string{TransportHTTP, TransportWebSocket} {
  t.Run(transport, func(t *testing.T) {
   outbound := &OutboundTransformer{config: &Config{Transport: transport}}
   req := &httpclient.Request{URL: "https://relay.example/v1/responses", Body: body, JSONBody: body}
   got := outbound.FinalizeTransportRequest(req)
   require.Equal(t, body, req.Body)
   var original, repaired map[string]json.RawMessage
   require.NoError(t, json.Unmarshal(body, &original)); require.NoError(t, json.Unmarshal(got.Body, &repaired))
   for _, idx := range []string{"2", "3"} { require.False(t, gjson.GetBytes(got.Body, "input."+idx+".id").Exists()) }
   for _, idx := range []string{"0", "1", "4", "5", "6"} { require.Equal(t, gjson.GetBytes(body,"input."+idx).Raw,gjson.GetBytes(got.Body,"input."+idx).Raw) }
   require.Equal(t, "opaque", gjson.GetBytes(got.Body,"input.3.encrypted_content").String())
   require.Equal(t, "visible", gjson.GetBytes(got.Body,"input.2.summary.0.text").String())
   require.Equal(t, "stable", gjson.GetBytes(got.Body,"prompt_cache_key").String())
   require.Equal(t, got.Body, got.JSONBody)
   require.Same(t, got, PrepareReplayItemIDs(got))
  })
 }
}
