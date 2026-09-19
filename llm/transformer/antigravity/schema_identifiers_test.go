package antigravity

import (
	"encoding/json"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/transformer/gemini"
)

func TestSchemaIdentifiersPreservePropertyNames(t *testing.T) {
	const raw = `{"type":"object","id":"root","$anchor":"root","$vocabulary":{"core":true},"properties":{"id":{"type":"string","id":"Identifier","$dynamicAnchor":"identifier","$dynamicRef":"#identifier"},"$anchor":{"type":"string"},"rows":{"type":"array","items":{"type":"object","$id":"row","id":"row","properties":{"id":{"type":"integer"}},"required":["id"]}}},"required":["id","rows"]}`
	var input map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &input))
	result := SanitizeJSONSchema(input)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.JSONEq(t, `{"type":"object","properties":{"id":{"type":"string"},"$anchor":{"type":"string"},"rows":{"type":"array","items":{"type":"object","properties":{"id":{"type":"integer"}},"required":["id"]}}},"required":["id","rows"]}`, string(encoded))
	unchanged, err := json.Marshal(input)
	require.NoError(t, err)
	require.JSONEq(t, raw, string(unchanged), "the caller's schema is immutable")
}

func TestUnsupportedEvaluationKeywordsAreScopedToAntigravitySchemas(t *testing.T) {
	const raw = `{"type":"object","unevaluatedProperties":false,"contentSchema":{"type":"string"},"properties":{"unevaluatedProperties":{"type":"string"},"rows":{"type":"array","additionalItems":false,"unevaluatedItems":false,"items":{"type":"object","contentSchema":{"type":"string"},"properties":{"contentSchema":{"type":"string"}},"required":["contentSchema"]}}},"required":["unevaluatedProperties","rows"]}`
	request := &llm.Request{
		Model:    "gemini-2.5-flash",
		Messages: []llm.Message{{Role: "user", Content: llm.MessageContent{Content: lo.ToPtr("inspect rows")}}},
		Tools: []llm.Tool{
			{Type: "function", Function: llm.Function{Name: "inspect", Parameters: json.RawMessage(raw)}},
		},
	}
	native, err := gemini.NewOutboundTransformer("https://example.com", "test-key")
	require.NoError(t, err)
	wire, err := native.TransformRequest(t.Context(), request)
	require.NoError(t, err)
	var converted gemini.GenerateContentRequest
	require.NoError(t, json.Unmarshal(wire.Body, &converted))
	require.JSONEq(t, raw, string(converted.Tools[0].FunctionDeclarations[0].ParametersJsonSchema))
	adapter := &Transformer{}
	require.NoError(t, adapter.patchGeminiRequest(t.Context(), &converted, request))
	declaration := converted.Tools[0].FunctionDeclarations[0]
	require.Empty(t, declaration.ParametersJsonSchema)
	require.JSONEq(t, `{"type":"OBJECT","properties":{"unevaluatedProperties":{"type":"STRING"},"rows":{"type":"ARRAY","items":{"type":"OBJECT","properties":{"contentSchema":{"type":"STRING"}},"required":["contentSchema"]}}},"required":["unevaluatedProperties","rows"]}`, string(declaration.Parameters))
	require.Equal(t, raw, string(request.Tools[0].Function.Parameters))
}
