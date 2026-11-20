package responses

import (
 "encoding/json"
 "testing"

 "github.com/stretchr/testify/require"
 "github.com/looplj/axonhub/llm/httpclient"
)

func TestRequestCountrySurvivesResponsesConversion(t *testing.T) {
 for _, value := range []string{`"DE"`, `null`, ``} {
  t.Run(value, func(t *testing.T) {
   body := `{"model":"gpt-6.2-sol","input":"continue"`
   if value != "" { body += `,"request_country":` + value }
   body += `}`
   unified, err := NewInboundTransformer().TransformRequest(t.Context(), &httpclient.Request{Body: []byte(body)})
   require.NoError(t, err)
   // Exercise the persisted common representation as well as the raw inbound.
   saved, err := json.Marshal(unified)
   require.NoError(t, err)
   require.NoError(t, json.Unmarshal(saved, unified))
   outbound, err := NewOutboundTransformer("https://example.com/v1", "synthetic-key")
   require.NoError(t, err)
   request, err := outbound.TransformRequest(t.Context(), unified)
   require.NoError(t, err)
   var fields map[string]json.RawMessage
   require.NoError(t, json.Unmarshal(request.Body, &fields))
   require.Equal(t, value, string(fields["request_country"]))
  })
 }
}
