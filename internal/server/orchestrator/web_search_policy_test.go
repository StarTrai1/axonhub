package orchestrator

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

func TestApplyResponsesLiteWebSearchFallback_AutoInjectsAfterPassThrough(t *testing.T) {
	rawBody := []byte(`{
		"model":"gpt-5.6-sol",
		"input":[
			{"type":"additional_tools","role":"developer","tools":[{"type":"namespace","name":"web","tools":[{"name":"run"}]}]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"search the web"}]}
		],
		"stream":true
	}`)
	candidate := &biz.Channel{Channel: &ent.Channel{
		ID:   7,
		Name: "responses-lite-auto",
		Type: channel.TypeCodex,
		Settings: &objects.ChannelSettings{
			PassThroughBody: lo.ToPtr(true),
		},
		Policies: objects.ChannelPolicies{WebSearch: objects.WebSearchPolicyAuto},
	}}
	outbound := &PersistentOutboundTransformer{state: &PersistenceState{
		CurrentCandidate: &ChannelModelsCandidate{Channel: candidate},
		LlmRequest: &llm.Request{
			Model:       "gpt-5.6-sol",
			RequestType: llm.RequestTypeChat,
			APIFormat:   llm.APIFormatOpenAIResponse,
			RawRequest: &httpclient.Request{
				Headers: http.Header{responses.ResponsesLiteHeader: []string{"true"}},
				Body:    rawBody,
			},
		},
	}}
	providerRequest := &httpclient.Request{
		APIFormat: string(llm.APIFormatOpenAIResponse),
		Body:      []byte(`{"model":"gpt-5.6-sol","input":[],"tools":[{"type":"function","name":"discarded_by_pass_through"}],"stream":true}`),
	}

	processed, err := applyPassThroughRequestBody(outbound, nil).OnOutboundRawRequest(context.Background(), providerRequest)
	require.NoError(t, err)
	require.True(t, outbound.state.PassThroughApplied)
	require.False(t, jsonObjectHasKey(processed.Body, "tools"))

	processed, err = applyResponsesLiteWebSearchFallback(outbound).OnOutboundRawRequest(context.Background(), processed)
	require.NoError(t, err)

	var payload struct {
		Tools []struct {
			Type               string   `json:"type"`
			ExternalWebAccess  bool     `json:"external_web_access"`
			SearchContentTypes []string `json:"search_content_types"`
		} `json:"tools"`
	}
	require.NoError(t, json.Unmarshal(processed.Body, &payload))
	require.Len(t, payload.Tools, 1)
	require.Equal(t, "web_search", payload.Tools[0].Type)
	require.True(t, payload.Tools[0].ExternalWebAccess)
	require.Equal(t, []string{"text", "image"}, payload.Tools[0].SearchContentTypes)
	require.False(t, jsonObjectHasKey(rawBody, "tools"))
}

func TestApplyResponsesLiteWebSearchFallback_RequiresExactAutoLiteShape(t *testing.T) {
	missingToolsBody := []byte(`{"model":"gpt-5.6-sol","input":[{"type":"additional_tools","role":"developer","tools":[]}],"stream":true}`)

	tests := []struct {
		name         string
		policy       objects.WebSearchPolicy
		header       string
		rawBody      []byte
		providerBody []byte
	}{
		{name: "native policy", policy: objects.WebSearchPolicyNative, header: "true", rawBody: missingToolsBody, providerBody: missingToolsBody},
		{name: "MCP only policy", policy: objects.WebSearchPolicyMCPOnly, header: "true", rawBody: missingToolsBody, providerBody: missingToolsBody},
		{name: "not Responses Lite", policy: objects.WebSearchPolicyAuto, rawBody: missingToolsBody, providerBody: missingToolsBody},
		{name: "explicit empty top-level tools", policy: objects.WebSearchPolicyAuto, header: "true", rawBody: []byte(`{"model":"gpt-5.6-sol","input":[{"type":"additional_tools","tools":[]}],"tools":[]}`), providerBody: missingToolsBody},
		{name: "no additional tools", policy: objects.WebSearchPolicyAuto, header: "true", rawBody: []byte(`{"model":"gpt-5.6-sol","input":[{"type":"message","role":"user","content":[]}]}`), providerBody: missingToolsBody},
		{name: "outbound already has tools", policy: objects.WebSearchPolicyAuto, header: "true", rawBody: missingToolsBody, providerBody: []byte(`{"model":"gpt-5.6-sol","input":[],"tools":[{"type":"function","name":"existing"}]}`)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := &biz.Channel{Channel: &ent.Channel{
				Type:     channel.TypeCodex,
				Policies: objects.ChannelPolicies{WebSearch: test.policy},
			}}
			outbound := &PersistentOutboundTransformer{state: &PersistenceState{
				CurrentCandidate: &ChannelModelsCandidate{Channel: candidate},
				LlmRequest: &llm.Request{
					RequestType: llm.RequestTypeChat,
					APIFormat:   llm.APIFormatOpenAIResponse,
					RawRequest: &httpclient.Request{
						Headers: http.Header{responses.ResponsesLiteHeader: []string{test.header}},
						Body:    test.rawBody,
					},
				},
			}}
			request := &httpclient.Request{Body: append([]byte(nil), test.providerBody...)}

			processed, err := applyResponsesLiteWebSearchFallback(outbound).OnOutboundRawRequest(context.Background(), request)
			require.NoError(t, err)
			require.Equal(t, string(test.providerBody), string(processed.Body))
		})
	}
}

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
					RawInputItems: []llm.OpenAIResponsesRawFragment{{
						Type:          "additional_tools",
						OriginalIndex: 0,
						Raw: json.RawMessage(`{
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
