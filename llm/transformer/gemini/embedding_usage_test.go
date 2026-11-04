package gemini

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer/openai"
)

func TestEmbeddingUsageReachesOpenAIResponse(t *testing.T) {
	for _, shape := range []struct {
		name  string
		data  string
		count int
	}{
		{"single", `"embedding":{"values":[0.25,0.5]}`, 1},
		{"batch", `"embeddings":[{"values":[0.25,0.5]},{"values":[0.75,1]}]`, 2},
	} {
		for _, metadata := range []struct {
			name    string
			json    string
			known   bool
			tokens  int64
			details []llm.ModalityTokenCount
		}{
			{"absent", "", false, 0, nil},
			{"null", `,"usageMetadata":null`, false, 0, nil},
			{"empty", `,"usageMetadata":{}`, false, 0, nil},
			{"null-count", `,"usageMetadata":{"promptTokenCount":null}`, false, 0, nil},
			{"negative", `,"usageMetadata":{"promptTokenCount":-1}`, false, 0, nil},
			{"zero", `,"usageMetadata":{"promptTokenCount":0}`, true, 0, nil},
			{"reported", `,"usageMetadata":{"promptTokenCount":258,"promptTokenDetails":[{"modality":"TEXT","tokenCount":10},{"modality":"IMAGE","tokenCount":248}]}`, true, 258, []llm.ModalityTokenCount{{Modality: "TEXT", TokenCount: 10}, {Modality: "IMAGE", TokenCount: 248}}},
			{"details-only", `,"usageMetadata":{"promptTokenDetails":[{"modality":"TEXT","tokenCount":7}]}`, false, 0, nil},
			{"invalid-details", `,"usageMetadata":{"promptTokenCount":7,"promptTokenDetails":[null,{"modality":"IMAGE","tokenCount":-1},{"modality":"TEXT","tokenCount":7}]}`, true, 7, []llm.ModalityTokenCount{{Modality: "TEXT", TokenCount: 7}}},
			{"generation-fields", `,"usageMetadata":{"promptTokenCount":9,"candidatesTokenCount":99,"thoughtsTokenCount":88,"totalTokenCount":196}`, true, 9, nil},
		} {
			t.Run(shape.name+"/"+metadata.name, func(t *testing.T) {
				body := []byte(fmt.Sprintf("{%s%s}", shape.data, metadata.json))
				response, err := newTestEmbeddingTransformer().TransformResponse(context.Background(), &httpclient.Response{
					StatusCode: http.StatusOK,
					Body:       body,
					Request:    &httpclient.Request{RequestType: string(llm.RequestTypeEmbedding), APIFormat: string(llm.APIFormatGeminiEmbedding)},
				})
				require.NoError(t, err)
				require.Len(t, response.Embedding.Data, shape.count)
				require.Equal(t, []float64{0.25, 0.5}, response.Embedding.Data[0].Embedding.Embedding)
				if metadata.known {
					require.NotNil(t, response.Usage)
					require.Equal(t, metadata.tokens, response.Usage.PromptTokens)
					require.Equal(t, metadata.tokens, response.Usage.TotalTokens)
					require.Zero(t, response.Usage.CompletionTokens)
					require.Equal(t, metadata.details, response.Usage.PromptModalityTokenDetails)
				} else {
					require.Nil(t, response.Usage)
				}
				wire, err := openai.NewEmbeddingInboundTransformer().TransformResponse(context.Background(), response)
				require.NoError(t, err)
				var decoded openai.EmbeddingResponse
				require.NoError(t, json.Unmarshal(wire.Body, &decoded))
				require.Len(t, decoded.Data, shape.count)
				require.Equal(t, metadata.tokens, decoded.Usage.PromptTokens)
				require.Equal(t, metadata.tokens, decoded.Usage.TotalTokens)
			})
		}
	}
}
