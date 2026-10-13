package llm

import "encoding/json"

// HasTerminalHostedSearch reports server work that must not be retried as an
// empty answer. An in-progress marker alone is not a commitment boundary.
func HasTerminalHostedSearch(metadata map[string]any) bool {
 raw, ok := metadata["openai_responses_web_search_calls"]
 if !ok { return false }
 data, err := json.Marshal(raw)
 if err != nil { return false }
 var calls []struct { ID string `json:"id"`; Type string `json:"type"`; Status string `json:"status"` }
 if json.Unmarshal(data,&calls)!=nil { return false }
 for _,call:=range calls {
  if call.ID!="" && call.Type=="web_search_call" && (call.Status=="completed" || call.Status=="failed" || call.Status=="incomplete") { return true }
 }
 return false
}
