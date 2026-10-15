package vertex

import (
 "fmt"
 "net/http"
 "testing"

 "github.com/stretchr/testify/require"
 "golang.org/x/oauth2"
 "golang.org/x/oauth2/google"

 "github.com/looplj/axonhub/llm/httpclient"
)

func TestVertexMultiRegionRequestURLs(t *testing.T) {
 for _, tc := range []struct{region, host string}{
  {"global", "aiplatform.googleapis.com"},
  {"us", "aiplatform.us.rep.googleapis.com"},
  {"eu", "aiplatform.eu.rep.googleapis.com"},
  {"us-central1", "us-central1-aiplatform.googleapis.com"},
 } {
  t.Run(tc.region, func(t *testing.T) {
   executor, err := NewExecutor(tc.region, "synthetic-project", &google.Credentials{TokenSource: oauth2.StaticTokenSource(&oauth2.Token{AccessToken:"synthetic"})})
   require.NoError(t, err)
   for _, stream := range []bool{false,true} {
    request := &httpclient.Request{Method:http.MethodPost, URL:"/v1/messages", Body:[]byte(fmt.Sprintf(`{"model":"claude-synthetic","stream":%t}`,stream))}
    wire, err := executor.transformRequest(request)
    require.NoError(t, err)
    action := "rawPredict"
    if stream { action = "streamRawPredict" }
    require.Equal(t, fmt.Sprintf("https://%s/v1/projects/synthetic-project/locations/%s/publishers/anthropic/models/claude-synthetic:%s",tc.host,tc.region,action),wire.URL)
    require.NotContains(t,string(wire.Body),`"model"`)
    require.Contains(t,string(request.Body),`"model"`)
   }
   wire, err := executor.transformRequest(&httpclient.Request{Method:http.MethodPost, URL:"/v1/messages/count_tokens",Body:[]byte(`{"model":"claude-synthetic"}`)})
   require.NoError(t,err)
   require.Equal(t,fmt.Sprintf("https://%s/v1/projects/synthetic-project/locations/%s/publishers/anthropic/models/count-tokens:rawPredict",tc.host,tc.region),wire.URL)
  })
 }
}
