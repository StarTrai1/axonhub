package responses

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/llm/httpclient"
)

func TestResponsesUsageCacheWriteAliases(t *testing.T) {
	for _, field := range []string{"cache_write_tokens", "write_cached_tokens", "cache_creation_tokens", "cached_creation_tokens"} {
		t.Run(field, func(t *testing.T) {
			body := []byte(fmt.Sprintf(`{"id":"resp_test","object":"response","status":"completed","output":[],"usage":{"input_tokens":100,"output_tokens":5,"total_tokens":105,"input_tokens_details":{"cached_tokens":30,%q:20}}}`, field))
			outbound, err := NewOutboundTransformer("https://example.invalid", "synthetic-key")
			require.NoError(t, err)
			response, err := outbound.TransformResponse(t.Context(), &httpclient.Response{StatusCode: 200, Body: body})
			require.NoError(t, err)
			require.Equal(t, int64(100), response.Usage.PromptTokens)
			require.Equal(t, int64(20), response.Usage.PromptTokensDetails.WriteCachedTokens)
			require.Equal(t, int64(30), response.Usage.PromptTokensDetails.CachedTokens)
			wire, err := json.Marshal(ConvertLLMUsageToResponsesUsage(response.Usage))
			require.NoError(t, err)
			require.Equal(t, int64(20), gjson.GetBytes(wire, "input_tokens_details.cache_write_tokens").Int())
			// A canonical explicit zero wins over a compatibility alias.
			if field != "cache_write_tokens" {
				body, err = sjson.SetBytes(body, "usage.input_tokens_details.cache_write_tokens", 0)
				require.NoError(t, err)
				response, err = outbound.TransformResponse(t.Context(), &httpclient.Response{StatusCode: 200, Body: body})
				require.NoError(t, err)
				require.Zero(t, response.Usage.PromptTokensDetails.WriteCachedTokens)
			}
		})
	}
}
