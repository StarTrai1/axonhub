package gemini

import (
 "testing"
 "github.com/samber/lo"
 "github.com/stretchr/testify/require"
 "github.com/tidwall/gjson"
 "github.com/looplj/axonhub/llm"
 "github.com/looplj/axonhub/llm/auth"
)

func TestVertexDropsOnlyTrailingReasoningTurns(t *testing.T) {
 for _, platform := range []string{"", PlatformVertex} {
  for _, visible := range []bool{false, true} {
   t.Run(platform+"/visible="+lo.Ternary(visible,"yes","no"),func(t *testing.T) {
    transformer, err := NewOutboundTransformerWithConfig(Config{PlatformType:platform, APIKeyProvider:auth.NewStaticKeyProvider("synthetic")})
    require.NoError(t,err)
    tail := llm.Message{Role:"assistant",ReasoningContent:lo.ToPtr("unfinished reasoning")}
    if visible { tail.Content.Content = lo.ToPtr("visible answer") }
    request, err := transformer.TransformRequest(t.Context(), &llm.Request{Model:"gemini-3.1-pro",Messages:[]llm.Message{{Role:"user",Content:llm.MessageContent{Content:lo.ToPtr("hello")}},tail}})
    require.NoError(t,err)
    want := 2
    if platform == PlatformVertex && !visible { want = 1 }
    require.Len(t,gjson.GetBytes(request.Body,"contents").Array(),want)
    if visible { require.Contains(t,string(request.Body),"visible answer") }
   })
  }
 }
}
