package antigravity

import (
	"encoding/json"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/transformer/gemini"
)

func TestSanitizeArrayItemsPreservesSchemaBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		want string
	}{
		{"missing type", `{"items":{"type":"string"}}`, `{"type":"array","items":{"type":"string"}}`},
		{"explicit non-array", `{"type":"string","items":{"type":"number"}}`, `{"type":"string"}`},
		{"uppercase array", `{"type":"ARRAY","items":{"type":"STRING"}}`, `{"type":"ARRAY","items":{"type":"STRING"}}`},
		{"union selects array", `{"type":["string","array"],"items":{"type":"string"}}`, `{"type":"array","items":{"type":"string"},"description":"Accepts: string | array"}`},
		{"nullable uppercase union", `{"type":["STRING","ARRAY","NULL"],"items":{"type":"STRING"}}`, `{"type":"ARRAY","items":{"type":"STRING"},"description":"Accepts: STRING | ARRAY (nullable)"}`},
		{"array without items", `{"type":"array"}`, `{"type":"array","items":{"type":"string"}}`},
		{"boolean items", `{"type":"array","items":false}`, `{"type":"array","items":{"type":"string"}}`},
		{"null items", `{"type":"ARRAY","items":null}`, `{"type":"ARRAY","items":{"type":"string"}}`},
		{"empty schema items", `{"type":"array","items":{}}`, `{"type":"array","items":{"type":"string"}}`},
		{"empty tuple", `{"type":"array","items":[]}`, `{"type":"array","items":{"type":"string"}}`},
		{"legacy tuple", `{"type":"array","items":[{"type":"string"},{"type":"object","properties":{"x":{"type":"number"}}}]}`, `{"type":"array","items":{"type":"object","properties":{"x":{"type":"number"}}}}`},
		{"prefix tuple infers array", `{"prefixItems":[{"type":"integer"}],"items":false}`, `{"type":"array","items":{"type":"integer"}}`},
		{"empty prefix tuple", `{"prefixItems":[]}`, `{"type":"array","items":{"type":"string"}}`},
		{"prefix tuple replaces empty items", `{"type":"array","prefixItems":[{"type":"boolean"}],"items":{}}`, `{"type":"array","items":{"type":"boolean"}}`},
		{"concrete items take precedence", `{"type":"array","prefixItems":[{"type":"string"}],"items":{"type":"number"}}`, `{"type":"array","items":{"type":"number"}}`},
		{"non-array drops prefix", `{"type":"string","prefixItems":[{"type":"integer"}]}`, `{"type":"string"}`},
		{"prefix selects nullable array", `{"type":["string","ARRAY","null"],"prefixItems":[{"type":"integer"}]}`, `{"type":"ARRAY","items":{"type":"integer"},"description":"Accepts: string | ARRAY (nullable)"}`},
		{"tuple inside union", `{"anyOf":[{"type":"array","prefixItems":[{"type":"number"}]},{"type":"null"}]}`, `{"type":"array","items":{"type":"number"},"description":"Accepts: array | null"}`},
		{"constant tuple item", `{"type":"array","prefixItems":[{"type":"null"},{"const":true}]}`, `{"type":"array","items":{"type":"boolean","enum":[true]}}`},
		{"nested tuple", `{"type":"array","items":{"prefixItems":[{"type":"integer"}]}}`, `{"type":"array","items":{"type":"array","items":{"type":"integer"}}}`},
		{"uppercase array tuple member", `{"type":"array","items":[{"type":"STRING"},{"type":"ARRAY"}]}`, `{"type":"array","items":{"type":"ARRAY","items":{"type":"string"}}}`},
		{"prefix-only union member", `{"anyOf":[{"type":"string"},{"prefixItems":[{"type":"number"}]}]}`, `{"type":"array","items":{"type":"number"},"description":"Accepts: string | array"}`},
		{"keyword property names in tuple", `{"type":"array","prefixItems":[{"type":"object","properties":{"prefixItems":{"type":"string"},"items":{"type":"integer"},"type":{"type":"boolean"}}}]}`, `{"type":"array","items":{"type":"object","properties":{"prefixItems":{"type":"string"},"items":{"type":"integer"},"type":{"type":"boolean"}}}}`},
		{"property names", `{"type":"object","properties":{"type":{"type":"string"},"items":{"type":"string"},"rows":{"items":{"type":"object","properties":{"items":{"type":"integer"}}}}}}`, `{"type":"object","properties":{"type":{"type":"string"},"items":{"type":"string"},"rows":{"type":"array","items":{"type":"object","properties":{"items":{"type":"integer"}}}}}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var input map[string]any
			require.NoError(t, json.Unmarshal([]byte(tc.raw), &input))
			result, err := json.Marshal(SanitizeJSONSchema(input))
			require.NoError(t, err)
			require.JSONEq(t, tc.want, string(result))
			after, err := json.Marshal(input)
			require.NoError(t, err)
			require.JSONEq(t, tc.raw, string(after))
		})
	}
}

func TestTupleSchemasAreSanitizedOnlyAtAntigravityBoundary(t *testing.T) {
	const raw = `{"type":"object","properties":{"rows":{"type":"array","prefixItems":[{"type":"integer"}],"items":false},"prefixItems":{"type":"string"}}}`
	request := &llm.Request{
		Model:    "gemini-2.5-flash",
		Messages: []llm.Message{{Role: "user", Content: llm.MessageContent{Content: lo.ToPtr("inspect rows")}}},
		Tools:    []llm.Tool{{Type: "function", Function: llm.Function{Name: "inspect", Parameters: json.RawMessage(raw)}}},
	}
	native, err := gemini.NewOutboundTransformer("https://example.com", "test-key")
	require.NoError(t, err)
	wire, err := native.TransformRequest(t.Context(), request)
	require.NoError(t, err)
	var converted gemini.GenerateContentRequest
	require.NoError(t, json.Unmarshal(wire.Body, &converted))
	require.JSONEq(t, raw, string(converted.Tools[0].FunctionDeclarations[0].ParametersJsonSchema))
	// Both structured output and tool parameters share the protobuf boundary.
	require.NoError(t, json.Unmarshal([]byte(`{"responseJsonSchema":`+raw+`}`), &converted.GenerationConfig))
	adapter := &Transformer{}
	require.NoError(t, adapter.patchGeminiRequest(t.Context(), &converted, request))
	const want = `{"type":"OBJECT","properties":{"rows":{"type":"ARRAY","items":{"type":"INTEGER"}},"prefixItems":{"type":"STRING"}}}`
	require.JSONEq(t, want, string(converted.Tools[0].FunctionDeclarations[0].Parameters))
	require.JSONEq(t, want, string(converted.GenerationConfig.ResponseSchema))
	require.Empty(t, converted.Tools[0].FunctionDeclarations[0].ParametersJsonSchema)
	require.Empty(t, converted.GenerationConfig.ResponseJsonSchema)
	require.Equal(t, raw, string(request.Tools[0].Function.Parameters))
}
