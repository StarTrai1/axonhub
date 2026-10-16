package orchestrator

import (
 "testing"
 "github.com/stretchr/testify/require"
 "github.com/tidwall/gjson"
 "github.com/looplj/axonhub/llm"
)

func TestCodex163OutputReplaySurvivesGateway(t *testing.T) {
 const body = `{"model":"gpt-6.1-sol","stream":true,"include":["reasoning.encrypted_content","output.encrypted_content"],"metadata":{"history_initialization":"cold_resume"},"input":[{"type":"message","id":"msg_old","role":"assistant","status":"completed","encrypted_content":"opaque-output-replay","content":[{"type":"output_text","text":"retained answer","annotations":[],"logprobs":[{"token":"retained","logprob":-0.1}]}]},{"type":"message","role":"user","content":"continue"}]}`
 for _, passthrough := range []bool{false,true} {
  request := rejectedReasoningPipelineRequest(t,llm.APIFormatOpenAIResponse)
  request.Body = []byte(body)
  executor := &responsesReasoningPipelineExecutor{events:rejectedReasoningCompactionEvents()}
  _,result,err := runRejectedReasoningPipeline(t,t.Context(),request,executor,"codex163-replay",passthrough,0)
  require.NoError(t,err)
  drainRejectedReasoningPipeline(t,result)
  require.Len(t,executor.requests,1)
  actual := executor.requests[0].Body
  for _,path := range []string{"input","include","metadata"} { require.JSONEq(t,gjson.Get(body,path).Raw,gjson.GetBytes(actual,path).Raw) }
 }
}
