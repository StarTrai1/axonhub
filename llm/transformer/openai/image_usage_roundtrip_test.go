package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
)

func TestImageUsageRoundTripPreservesOutputModalityCounts(t *testing.T) {
	outbound, err := NewOutboundTransformer("https://api.openai.com/v1", "test-key")
	require.NoError(t, err)

	for _, inbound := range []*ImageInboundTransformer{
		NewImageGenerationInboundTransformer(),
		NewImageEditInboundTransformer(),
		NewImageVariationInboundTransformer(),
	} {
		t.Run(string(inbound.APIFormat()), func(t *testing.T) {
			for _, tc := range []struct {
				name      string
				details   string
				image     *int64
				text      *int64
				reasoning int64
			}{
				{name: "mixed output", details: `,"output_tokens_details":{"image_tokens":1120,"text_tokens":232}`, image: lo.ToPtr(int64(1120)), text: lo.ToPtr(int64(232))},
				{name: "explicit zero", details: `,"output_tokens_details":{"image_tokens":0,"text_tokens":0}`, image: lo.ToPtr(int64(0)), text: lo.ToPtr(int64(0))},
				{name: "image only", details: `,"output_tokens_details":{"image_tokens":1120}`, image: lo.ToPtr(int64(1120))},
				{name: "text only", details: `,"output_tokens_details":{"text_tokens":232}`, text: lo.ToPtr(int64(232))},
				{name: "legacy reasoning", details: `,"output_tokens_details":{"reasoning_tokens":50}`, reasoning: 50},
				{name: "empty details", details: `,"output_tokens_details":{}`},
				{name: "legacy absent details"},
			} {
				t.Run(tc.name, func(t *testing.T) {
					body := []byte(`{"created":1730000000,"data":[{"b64_json":"AAA","generation_id":"gen_first"}],"usage":{"input_tokens":15,"output_tokens":1352,"total_tokens":1367,"input_tokens_details":{"image_tokens":0,"text_tokens":15}` + tc.details + `}}`)
					upstream := &httpclient.Response{
						StatusCode: http.StatusOK,
						Body:       body,
						Request:    &httpclient.Request{APIFormat: string(inbound.APIFormat())},
					}

					unified, err := outbound.TransformResponse(context.Background(), upstream)
					require.NoError(t, err)
					require.NotNil(t, unified.Usage)

					// Exercise the persisted unified JSON shape before converting back.
					stored, err := json.Marshal(unified)
					require.NoError(t, err)
					var restored llm.Response
					require.NoError(t, json.Unmarshal(stored, &restored))
					require.NotNil(t, restored.Usage)
					assert.Equal(t, int64(15), restored.Usage.PromptTokens)
					assert.Equal(t, int64(1352), restored.Usage.CompletionTokens)
					assert.Equal(t, int64(1367), restored.Usage.TotalTokens)
					if tc.details == "" {
						assert.Nil(t, restored.Usage.CompletionTokensDetails)
					} else {
						require.NotNil(t, restored.Usage.CompletionTokensDetails)
						assert.Equal(t, tc.image, restored.Usage.CompletionTokensDetails.ImageTokens)
						assert.Equal(t, tc.text, restored.Usage.CompletionTokensDetails.TextTokens)
						assert.Equal(t, tc.reasoning, restored.Usage.CompletionTokensDetails.ReasoningTokens)
					}

					response, err := inbound.TransformResponse(context.Background(), &restored)
					require.NoError(t, err)
					assert.JSONEq(t, string(body), string(response.Body))
					assert.Equal(t, body, upstream.Body)
				})
			}
		})
	}
}
