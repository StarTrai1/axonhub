package gemini

import (
 "testing"
 "context"
 "github.com/stretchr/testify/require"
 "github.com/looplj/axonhub/llm"
 "github.com/looplj/axonhub/llm/auth"
)

func TestVertexFullPublisherPrefixAcrossEndpoints(t *testing.T) {
 for _,base := range []string{"https://relay.invalid", "https://relay.invalid/v1", "https://relay.invalid/v1beta1", "https://relay.invalid/v1/projects/p/locations/us-central1", "https://relay.invalid/v1/projects/p/locations/us-central1/publishers/google/models/"} {
  t.Run(base,func(t *testing.T) {
   prefix := base
   switch base {
   case "https://relay.invalid": prefix += "/v1/publishers/google/models"
   case "https://relay.invalid/v1/projects/p/locations/us-central1/publishers/google/models/": prefix = base[:len(base)-1]
   default: prefix += "/publishers/google/models"
   }
   tr := &OutboundTransformer{config:Config{PlatformType:PlatformVertex,BaseURLProvider:func(context.Context) string { return base },APIKeyProvider:auth.NewStaticKeyProvider("synthetic")}}
   ctx := t.Context()
   require.Equal(t,prefix+"/model:generateContent",tr.buildFullRequestURLWithContext(ctx,&llm.Request{Model:"model"}))
   require.Equal(t,prefix+"/model:embedContent",tr.buildEmbeddingURL(ctx,"model",false))
   require.Equal(t,prefix+"/model:generateContent",tr.buildImageRequestURL(ctx,&llm.Request{Model:"model"}))
  })
 }
}
