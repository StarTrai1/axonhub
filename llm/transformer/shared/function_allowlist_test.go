package shared_test

import (
	"encoding/json"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer"
	"github.com/looplj/axonhub/llm/transformer/anthropic"
	"github.com/looplj/axonhub/llm/transformer/gemini"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

const functionAllowlistRequest = `{"model":"test-model","input":"hello","tools":[{"type":"function","name":"blocked","parameters":{"type":"object","properties":{}}},{"type":"function","name":"allowed","description":"keep description","strict":true,"parameters":{"type":"object","properties":{"value":{"type":"integer","minimum":7}},"required":["value"],"additionalProperties":false}},{"type":"web_search"}],"tool_choice":{"type":"allowed_tools","mode":"auto","tools":[{"type":"function","name":"allowed"}]}}`

func TestFunctionAllowlistAcrossConvertedProviders(t *testing.T) {
	for _, mode := range []string{"auto", "required"} {
		for _, provider := range []string{"gemini", "anthropic"} {
			t.Run(provider+"/"+mode, func(t *testing.T) {
				body, err := sjson.Set(functionAllowlistRequest, "tool_choice.mode", mode)
				require.NoError(t, err)
				input, err := responses.NewInboundTransformer().TransformRequest(t.Context(), &httpclient.Request{Body: []byte(body)})
				require.NoError(t, err)
				before, err := json.Marshal(input)
				require.NoError(t, err)
				// Persistence must not lose the discriminator or the allowlist.
				require.NoError(t, json.Unmarshal(before, input))
				var outbound transformer.Outbound
				if provider == "gemini" {
					outbound, err = gemini.NewOutboundTransformer("https://example.invalid", "synthetic-key")
				} else {
					outbound, err = anthropic.NewOutboundTransformer("https://example.invalid", "synthetic-key")
				}
				require.NoError(t, err)
				wire, err := outbound.TransformRequest(t.Context(), input)
				require.NoError(t, err)
				if provider == "gemini" {
					require.Len(t, gjson.GetBytes(wire.Body, "tools").Array(), 1)
					decls := gjson.GetBytes(wire.Body, "tools.0.functionDeclarations").Array()
					require.Len(t, decls, 1)
					require.Equal(t, "allowed", decls[0].Get("name").String())
					require.Equal(t, int64(7), decls[0].Get("parametersJsonSchema.properties.value.minimum").Int())
					want := "AUTO"
					if mode == "required" {
						want = "ANY"
					}
					require.Equal(t, want, gjson.GetBytes(wire.Body, "toolConfig.functionCallingConfig.mode").String())
				} else {
					decls := gjson.GetBytes(wire.Body, "tools").Array()
					require.Len(t, decls, 1)
					require.Equal(t, "allowed", decls[0].Get("name").String())
					require.True(t, decls[0].Get("strict").Bool())
					require.Equal(t, int64(7), decls[0].Get("input_schema.properties.value.minimum").Int())
					want := "auto"
					if mode == "required" {
						want = "any"
					}
					require.Equal(t, want, gjson.GetBytes(wire.Body, "tool_choice.type").String())
				}
				after, err := json.Marshal(input)
				require.NoError(t, err)
				require.JSONEq(t, string(before), string(after))
			})
		}
	}
}

func TestFunctionAllowlistRejectsUnrepresentableRestrictions(t *testing.T) {
	for _, scenario := range []string{"empty", "unknown", "hosted", "duplicate option", "duplicate declaration", "namespace", "invalid mode", "missing mode"} {
		t.Run(scenario, func(t *testing.T) {
			req := &llm.Request{Tools: []llm.Tool{{Type: "function", Function: llm.Function{Name: "f"}}}, ToolChoice: &llm.ToolChoice{
				ToolChoice: lo.ToPtr("auto"), NamedToolChoice: &llm.NamedToolChoice{Type: "allowed_tools"}, Tools: []llm.ToolOption{{Type: "function", Name: "f"}},
			}}
			switch scenario {
			case "empty":
				req.ToolChoice.Tools = nil
			case "unknown":
				req.ToolChoice.Tools[0].Name = "missing"
			case "hosted":
				req.ToolChoice.Tools[0].Type = "web_search"
			case "duplicate option":
				req.ToolChoice.Tools = append(req.ToolChoice.Tools, req.ToolChoice.Tools[0])
			case "duplicate declaration":
				req.Tools = append(req.Tools, req.Tools[0])
			case "namespace":
				req.Tools[0].Function.Namespace = "ns"
			case "invalid mode":
				req.ToolChoice.ToolChoice = lo.ToPtr("unexpected")
			case "missing mode":
				req.ToolChoice.ToolChoice = nil
			}
			got, err := shared.RestrictFunctionAllowlist(req)
			require.ErrorIs(t, err, transformer.ErrInvalidRequest)
			require.Nil(t, got)
			req.Model = "test-model"
			req.Messages = []llm.Message{{Role: "user", Content: llm.MessageContent{Content: lo.ToPtr("hello")}}}
			geminiOutbound, err := gemini.NewOutboundTransformer("https://example.invalid", "synthetic-key")
			require.NoError(t, err)
			anthropicOutbound, err := anthropic.NewOutboundTransformer("https://example.invalid", "synthetic-key")
			require.NoError(t, err)
			for _, outbound := range []transformer.Outbound{geminiOutbound, anthropicOutbound} {
				wire, err := outbound.TransformRequest(t.Context(), req)
				require.ErrorIs(t, err, transformer.ErrInvalidRequest)
				require.Nil(t, wire)
			}
		})
	}
}

func TestFunctionAllowlistPreservesDeclarationOrderAndOrdinaryRequests(t *testing.T) {
	req := &llm.Request{
		Tools: []llm.Tool{
			{Type: "function", Function: llm.Function{Name: "second"}},
			{Type: "function", Function: llm.Function{Name: "blocked"}},
			{Type: "function", Function: llm.Function{Name: "first"}},
		},
		ToolChoice: &llm.ToolChoice{ToolChoice: lo.ToPtr("required")},
	}
	unchanged, err := shared.RestrictFunctionAllowlist(req)
	require.NoError(t, err)
	require.Same(t, req, unchanged)
	req.ToolChoice.NamedToolChoice = &llm.NamedToolChoice{Type: "allowed_tools"}
	req.ToolChoice.Tools = []llm.ToolOption{{Type: "function", Name: "first"}, {Type: "function", Name: "second"}}
	adapted, err := shared.RestrictFunctionAllowlist(req)
	require.NoError(t, err)
	require.Len(t, adapted.Tools, 2)
	require.Equal(t, "second", adapted.Tools[0].Function.Name)
	require.Equal(t, "first", adapted.Tools[1].Function.Name)
	require.Equal(t, "required", *adapted.ToolChoice.ToolChoice)
	adapted.Tools[0].Function.Name = "changed"
	require.Equal(t, "second", req.Tools[0].Function.Name)
}
