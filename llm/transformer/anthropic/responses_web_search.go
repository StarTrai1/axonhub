package anthropic

import (
 "encoding/hex"
 "encoding/json"
 "strings"

 "github.com/samber/lo"
)

const responsesWebSearchMetadataKey = "openai_responses_web_search_calls"

// Read only terminal hosted searches. Source links are not search results and
// must never be turned into invented encrypted result payloads.
func responsesWebSearchBlocks(metadata map[string]any, emitted map[string]bool) []MessageContentBlock {
 raw, ok := metadata[responsesWebSearchMetadataKey]
 if !ok { return nil }
 data, err := json.Marshal(raw)
 if err != nil { return nil }
 var calls []struct {
  ID string `json:"id"`
  Type string `json:"type"`
  Status string `json:"status"`
  Action struct { Query string `json:"query"`; Queries []string `json:"queries"` } `json:"action"`
  Results json.RawMessage `json:"results"`
  Error struct { Code string `json:"code"` } `json:"error"`
 }
 if json.Unmarshal(data, &calls) != nil { return nil }
 var blocks []MessageContentBlock
 for _, call := range calls {
  if call.Type != "web_search_call" || call.ID == "" || emitted[call.ID] { continue }
  if call.Status != "completed" && call.Status != "failed" && call.Status != "incomplete" { continue }
  query := strings.TrimSpace(call.Action.Query)
  if len(call.Action.Queries) > 0 { query = strings.Join(call.Action.Queries, " ") }
  input, err := json.Marshal(map[string]string{"query":query})
  if err != nil { continue }
  id := "srvtoolu_oai_" + hex.EncodeToString([]byte(call.ID))
  content := json.RawMessage("[]")
  if call.Status != "completed" {
   code := call.Error.Code
   switch code {
   case "too_many_requests", "invalid_tool_input", "max_uses_exceeded", "query_too_long", "request_too_large", "unavailable":
   default: code = "unavailable"
   }
   content, _ = json.Marshal(map[string]string{"type":"web_search_tool_result_error", "error_code":code})
  } else {
   var results []map[string]json.RawMessage
   if json.Unmarshal(call.Results, &results) == nil {
    filtered := make([]map[string]json.RawMessage, 0, len(results))
    for _, result := range results {
     var encrypted string
     if json.Unmarshal(result["encrypted_content"], &encrypted) != nil || encrypted == "" { continue }
     entry := map[string]json.RawMessage{"type":json.RawMessage(`"web_search_result"`)}
     for _, key := range []string{"url", "title", "page_age", "encrypted_content"} {
      if value, ok := result[key]; ok { entry[key] = value }
     }
     filtered = append(filtered, entry)
    }
    content, _ = json.Marshal(filtered)
   }
  }
  blocks = append(blocks,
   MessageContentBlock{Type:"server_tool_use",ID:id,Name:lo.ToPtr("web_search"),Input:input},
   MessageContentBlock{Type:"web_search_tool_result",ToolUseID:lo.ToPtr(id),Content:&MessageContent{Raw:content}},
  )
  if emitted != nil { emitted[call.ID] = true }
 }
 return blocks
}

func (s *anthropicInboundStream) emitResponsesWebSearch(metadata map[string]any) error {
 if s.emittedResponsesSearches == nil { s.emittedResponsesSearches = make(map[string]bool) }
 blocks := responsesWebSearchBlocks(metadata, s.emittedResponsesSearches)
 if len(blocks) == 0 { return nil }
 if err := s.flushBufferedReasoningItems(); err != nil { return err }
 if err := s.closeOpenContentBlocks(); err != nil { return err }
 for _, block := range blocks {
  if err := s.enqueEvent(&StreamEvent{Type:"content_block_start",Index:&s.contentIndex,ContentBlock:&block}); err != nil { return err }
  if err := s.enqueEvent(&StreamEvent{Type:"content_block_stop",Index:&s.contentIndex}); err != nil { return err }
  s.contentIndex++
 }
 return nil
}
