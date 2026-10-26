package openai

import (
 "testing"
 "github.com/samber/lo"
 "github.com/stretchr/testify/require"
 "github.com/looplj/axonhub/llm"
)

func TestChatImageDetailPreservesClientSelection(t *testing.T) {
 for _, detail := range []*string{nil, lo.ToPtr("original"), lo.ToPtr("high"), lo.ToPtr("low"), lo.ToPtr("auto")} {
  part := llm.MessageContentPart{Type: "image_url", ImageURL: &llm.ImageURL{URL: "https://example.test/image.png", Detail: detail}}
  got := MessageContentPartFromLLM(part)
  expected := detail
  if detail != nil && *detail == "original" { expected = lo.ToPtr("high") }
  require.Equal(t, expected, got.ImageURL.Detail)
  require.Equal(t, detail, part.ImageURL.Detail)
 }
}
