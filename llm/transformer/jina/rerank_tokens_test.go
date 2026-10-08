package jina

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm/httpclient"
)

func TestRerankPreservesSiliconFlowNativeTokens(t *testing.T) {
	outbound, err := NewOutboundTransformer("https://api.siliconflow.cn/v1", "synthetic-key")
	require.NoError(t, err)
	for _, tc := range []struct {
		name                      string
		extra                     string
		prompt, completion, total int64
	}{
		{name: "native tokens", prompt: 12, completion: 2, total: 14},
		{name: "explicit usage wins", extra: `,"usage":{"prompt_tokens":4,"total_tokens":4}`, prompt: 4, total: 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response, err := outbound.transformRerankResponse(t.Context(), &httpclient.Response{Body: []byte(`{"id":"rank_test","results":[{"index":1,"relevance_score":0.9,"document":{"text":"second"}}],"tokens":{"input_tokens":12,"output_tokens":2}` + tc.extra + `}`)})
			require.NoError(t, err)
			require.Equal(t, "rank_test", response.ID)
			require.Equal(t, "second", response.Rerank.Results[0].Document.Text)
			require.Equal(t, tc.prompt, response.Usage.PromptTokens)
			require.Equal(t, tc.completion, response.Usage.CompletionTokens)
			require.Equal(t, tc.total, response.Usage.TotalTokens)
		})
	}
}
