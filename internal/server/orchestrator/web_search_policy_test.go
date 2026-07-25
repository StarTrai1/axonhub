package orchestrator

import (
	"encoding/json"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
)

func TestApplyWebSearchPolicy_MCPOnly(t *testing.T) {
	candidate := &biz.Channel{Channel: &ent.Channel{
		Type: channel.TypeCodex,
		Policies: objects.ChannelPolicies{
			WebSearch: objects.WebSearchPolicyMCPOnly,
		},
	}}
	req := &llm.Request{
		Tools: []llm.Tool{
			{Type: llm.ToolTypeWebSearch},
			{Type: "function", Function: llm.Function{Name: "mcp__grok-search-rs__web_search"}},
		},
		ToolChoice: &llm.ToolChoice{NamedToolChoice: &llm.NamedToolChoice{Type: "web_search"}},
		ProviderExtensions: &llm.ProviderExtensions{
			OpenAIResponses: &llm.OpenAIResponsesProviderExtensions{
				Request: &llm.OpenAIResponsesRequestExtensions{
					ToolSignatures: []string{"web_search", "function:mcp__grok-search-rs__web_search"},
					RawToolChoice:  json.RawMessage(`{"type":"web_search"}`),
					RawInputItems:  []llm.OpenAIResponsesRawFragment{{
						Type:          "additional_tools",
						OriginalIndex: 0,
						Raw:           json.RawMessage(`{
							"type":"additional_tools",
							"role":"developer",
							"tools":[
								{"type":"custom","name":"exec"},
								{"type":"web_search","external_web_access":true},
								{"type":"function","name":"mcp__grok-search-rs__web_search"},
								{"type":"namespace","name":"web","tools":[{"name":"run"}]}
							]
						}`),
					}},
				},
			},
		},
	}

	got := applyWebSearchPolicy(req, candidate)

	require.NotSame(t, req, got)
	require.Len(t, got.Tools, 1)
	require.Equal(t, "mcp__grok-search-rs__web_search", got.Tools[0].Function.Name)
	require.NotNil(t, got.ToolChoice)
	require.Equal(t, "auto", *got.ToolChoice.ToolChoice)
	require.Nil(t, got.ProviderExtensions.OpenAIResponses.Request.RawToolChoice)
	require.Equal(t, []string{"function:mcp__grok-search-rs__web_search"}, got.ProviderExtensions.OpenAIResponses.Request.ToolSignatures)

	additionalTools := got.ProviderExtensions.OpenAIResponses.Request.RawInputItems[0].Raw
	require.JSONEq(t, `{
		"type":"additional_tools",
		"role":"developer",
		"tools":[
			{"type":"custom","name":"exec"},
			{"type":"function","name":"mcp__grok-search-rs__web_search"}
		]
	}`, string(additionalTools))

	// Per-candidate filtering must not mutate the request used by later retries.
	require.Len(t, req.Tools, 2)
	require.Len(t, req.ProviderExtensions.OpenAIResponses.Request.RawInputItems, 1)
	require.Contains(t, string(req.ProviderExtensions.OpenAIResponses.Request.RawInputItems[0].Raw), "web_search")
}

func TestApplyWebSearchPolicy_ReindexesRawTools(t *testing.T) {
	candidate := &biz.Channel{Channel: &ent.Channel{
		Type: channel.TypeCodex,
		Policies: objects.ChannelPolicies{
			WebSearch: objects.WebSearchPolicyMCPOnly,
		},
	}}
	req := &llm.Request{
		Tools: []llm.Tool{
			{Type: "function", Function: llm.Function{Name: "first"}},
			{Type: llm.ToolTypeWebSearch},
		},
		ProviderExtensions: &llm.ProviderExtensions{
			OpenAIResponses: &llm.OpenAIResponsesProviderExtensions{
				Request: &llm.OpenAIResponsesRequestExtensions{
					ToolSignatures: []string{"function:first", "web_search"},
					RawTools: []llm.OpenAIResponsesRawFragment{
						{Type: "namespace", Name: "web", OriginalIndex: 1, Raw: json.RawMessage(`{"type":"namespace","name":"web"}`)},
						{Type: "tool_search", Name: "docs", OriginalIndex: 3, Raw: json.RawMessage(`{"type":"tool_search","name":"docs"}`)},
					},
				},
			},
		},
	}

	got := applyWebSearchPolicy(req, candidate)

	require.Len(t, got.Tools, 1)
	require.Len(t, got.ProviderExtensions.OpenAIResponses.Request.RawTools, 1)
	require.Equal(t, 1, got.ProviderExtensions.OpenAIResponses.Request.RawTools[0].OriginalIndex)
	require.Equal(t, []string{"function:first"}, got.ProviderExtensions.OpenAIResponses.Request.ToolSignatures)
}

func TestApplyWebSearchPolicy_OtherPoliciesAreUnchanged(t *testing.T) {
	req := &llm.Request{Tools: []llm.Tool{{Type: llm.ToolTypeWebSearch}}}

	for _, policy := range []objects.WebSearchPolicy{objects.WebSearchPolicyAuto, objects.WebSearchPolicyNative} {
		t.Run(string(policy), func(t *testing.T) {
			candidate := &biz.Channel{Channel: &ent.Channel{
				Type:     channel.TypeCodex,
				Policies: objects.ChannelPolicies{WebSearch: policy, SupportsWebSearch: lo.ToPtr(false)},
			}}
			require.Same(t, req, applyWebSearchPolicy(req, candidate))
		})
	}
}
