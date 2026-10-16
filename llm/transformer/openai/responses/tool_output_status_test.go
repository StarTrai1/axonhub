package responses_test

import (
 "fmt"
 "testing"
 "github.com/stretchr/testify/require"
 "github.com/tidwall/gjson"
 "github.com/looplj/axonhub/llm/httpclient"
 "github.com/looplj/axonhub/llm/transformer/anthropic"
 "github.com/looplj/axonhub/llm/transformer/openai/responses"
)

func TestResponsesIncompleteToolOutputReachesAnthropic(t *testing.T) {
 for _, kind := range []string{"function", "custom_tool"} {
  for _, status := range []string{"incomplete", "completed"} {
   t.Run(kind+"/"+status,func(t *testing.T) {
    call := `{"type":"function_call","call_id":"call_1","name":"lookup","arguments":"{}"}`
    if kind == "custom_tool" { call = `{"type":"custom_tool_call","call_id":"call_1","name":"lookup","input":"query"}` }
    body := []byte(fmt.Sprintf(`{"model":"claude-sonnet-4-6","input":[%s,{"type":"%s_call_output","call_id":"call_1","status":%q,"output":"tool partial result"}]}`,call,kind,status))
    request, err := responses.NewInboundTransformer().TransformRequest(t.Context(), &httpclient.Request{Body:body})
    require.NoError(t,err)
    outbound, err := anthropic.NewOutboundTransformer("https://api.anthropic.com","synthetic")
    require.NoError(t,err)
    converted, err := outbound.TransformRequest(t.Context(),request)
    require.NoError(t,err)
    var found bool
    for _, message := range gjson.GetBytes(converted.Body,"messages").Array() {
     for _, part := range message.Get("content").Array() {
      if part.Get("type").String() == "tool_result" { found = true; require.Equal(t,status=="incomplete",part.Get("is_error").Bool()); require.Equal(t,"tool partial result",part.Get("content").String()) }
     }
    }
    require.True(t,found)
   })
  }
 }
}
