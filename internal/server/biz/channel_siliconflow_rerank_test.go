package biz

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/llm"
)

func TestSiliconFlowDeclaresEmbeddingsAndNativeRerank(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:siliconflow-rerank?mode=memory&_fk=1")
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	service := NewChannelServiceForTest(client)
	t.Cleanup(service.Stop)
	ch, err := service.buildChannelWithOutbounds(&ent.Channel{Type: channel.TypeSiliconflow, BaseURL: "https://api.siliconflow.cn/v1", Credentials: objects.ChannelCredentials{APIKey: "synthetic-key"}})
	require.NoError(t, err)
	formats := make(map[string]bool)
	for _, endpoint := range ch.ResolveEndpoints() {
		formats[endpoint.APIFormat] = true
	}
	require.True(t, formats[llm.APIFormatOpenAIEmbedding.String()])
	require.True(t, formats[llm.APIFormatJinaRerank.String()])
	require.False(t, formats[llm.APIFormatOpenAISpeech.String()])
	outbound, err := BuildOutboundByAPIFormat(ch, llm.APIFormatJinaRerank.String())
	require.NoError(t, err)
	req, err := outbound.TransformRequest(t.Context(), &llm.Request{Model: "Qwen/Qwen3-Reranker-8B", RequestType: llm.RequestTypeRerank, APIFormat: llm.APIFormatJinaRerank, Rerank: &llm.RerankRequest{Query: "query", Documents: []string{"first", "second"}}})
	require.NoError(t, err)
	require.Equal(t, "https://api.siliconflow.cn/v1/rerank", req.URL)
	require.JSONEq(t, `{"model":"Qwen/Qwen3-Reranker-8B","query":"query","documents":["first","second"]}`, string(req.Body))
}
