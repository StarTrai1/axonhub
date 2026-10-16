package responses

import (
 "testing"
 "github.com/stretchr/testify/require"
 "github.com/tidwall/gjson"
 "github.com/looplj/axonhub/llm"
 "github.com/looplj/axonhub/llm/httpclient"
)

func TestAggregateCodex163OutputReplayAndIncompleteUsage(t *testing.T) {
 const terminal = `{"type":"response.incomplete","response":{"id":"resp_163","model":"gpt-6.1-sol","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output":[{"type":"message","id":"msg_163","role":"assistant","status":"completed","encrypted_content":"message-replay","content":[{"type":"output_text","text":"answer","logprobs":[{"token":"answer","logprob":-0.1}]}]},{"type":"function_call","id":"fc_163","call_id":"call_163","name":"lookup","arguments":"{}","status":"incomplete","encrypted_content":"tool-replay"},{"type":"custom_tool_call","id":"ct_163","call_id":"custom_163","name":"edit","input":"patch","status":"completed","encrypted_content":"custom-replay"}],"usage":{"input_tokens":7,"output_tokens":4,"total_tokens":11}}}`
 body,meta,err := AggregateStreamChunks(t.Context(),[]*httpclient.StreamEvent{{Type:"response.incomplete",Data:[]byte(terminal)}})
 require.NoError(t,err)
 for _,path := range []string{"id","model","status","incomplete_details","output.0.encrypted_content","output.0.content.0.logprobs","output.1.encrypted_content","output.2.encrypted_content"} {
  require.Equal(t,gjson.Get(terminal,"response."+path).Raw,gjson.GetBytes(body,path).Raw,path)
 }
 require.Equal(t,llm.AccountingEvidenceExplicitFinal,meta.AccountingEvidence)
 require.EqualValues(t,11,meta.Usage.TotalTokens)
}
