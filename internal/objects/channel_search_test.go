package objects

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWebSearchPolicyRetiresAutomaticFallback(t *testing.T) {
	for _, value := range []string{"auto", "native", "", "mcp_only", "other_channels"} {
		t.Run(value, func(t *testing.T) {
			var policies ChannelPolicies
			require.NoError(t, json.Unmarshal([]byte(`{"webSearch":"`+value+`","supportsWebSearch":false}`), &policies))
			want := WebSearchPolicyNative
			if value == "mcp_only" || value == "other_channels" {
				want = WebSearchPolicy(value)
			}
			require.Equal(t, want, policies.WebSearch)
			var output bytes.Buffer
			WebSearchPolicy(value).MarshalGQL(&output)
			require.Equal(t, `"`+string(want)+`"`, output.String())
		})
	}
}

func TestCodexSearchModelValidation(t *testing.T) {
	for _, model := range []string{"gpt-5.6-sol", "gpt-5.6-terra", "gpt-6-astra", "gpt-6.1-sol", "gpt-7-sol", "gpt-5.10-sol"} {
		require.True(t, IsCodexSearchModel(model), model)
		require.NoError(t, ValidateCodexSearchModel(&ChannelSettings{CodexSearchModel: model}))
	}
	for _, model := range []string{"gpt-5.5", "gpt-5-sol", "gpt-4.1", "claude-sonnet", "arbitrary-alias", "gpt-5.6oops", "gpt-6/../../x"} {
		require.False(t, IsCodexSearchModel(model), model)
		require.Error(t, ValidateCodexSearchModel(&ChannelSettings{CodexSearchModel: model}))
	}
	settings := &ChannelSettings{CodexSearchModel: "  "}
	if err := ValidateCodexSearchModel(settings); err != nil {
		t.Fatal(err)
	}
	if settings.CodexSearchModel != "" {
		t.Fatal("blank search model must follow the request")
	}
}

func TestWebSearchPolicyGraphQLAcceptsOtherChannels(t *testing.T) {
 var policy WebSearchPolicy
 require.NoError(t, policy.UnmarshalGQL("other_channels"))
 require.Equal(t, WebSearchPolicyOtherChannels, policy)
 require.Error(t, policy.UnmarshalGQL("auto"))
 require.Error(t, policy.UnmarshalGQL("disabled"))
}
