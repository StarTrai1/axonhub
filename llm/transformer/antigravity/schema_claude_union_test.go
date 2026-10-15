package antigravity

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/transformer/gemini"
)

func TestClaudeToolUnionsPreserveBoundaryAndNullable(t *testing.T) {
	const raw = `{"type":"object","properties":{"value":{"anyOf":[{"type":"string"},{"type":"object","properties":{"enabled":{"oneOf":[{"type":"boolean"},{"type":"integer"}]}}},{"type":"null"}]},"anyOf":{"type":"string"}}}`
	for _, model := range []string{"claude-synthetic", "gemini-3.8-flash"} {
		t.Run(model, func(t *testing.T) {
			var request gemini.GenerateContentRequest
			require.NoError(t, json.Unmarshal([]byte(`{"contents":[{"role":"user","parts":[{"text":"inspect"}]},{"role":"model","parts":[{"functionCall":{"name":"inspect","id":"tool_synthetic","args":{"anyOf":[{"const":7}],"value":{"enabled":true}}}}]}],"tools":[{"functionDeclarations":[{"name":"inspect","parametersJsonSchema":`+raw+`}]}],"generationConfig":{"responseJsonSchema":`+raw+`}}`), &request))
			args, _ := json.Marshal(request.Contents[1].Parts[0].FunctionCall.Args)
			adapter := &Transformer{}
			require.NoError(t, adapter.patchGeminiRequest(t.Context(), &request, &llm.Request{Model: model}))
			schema := request.Tools[0].FunctionDeclarations[0].Parameters
			if model == "claude-synthetic" {
				require.False(t, gjson.GetBytes(schema, "properties.value.anyOf").Exists())
				require.Equal(t, "OBJECT", gjson.GetBytes(schema, "properties.value.type").String())
				require.Equal(t, "BOOLEAN", gjson.GetBytes(schema, "properties.value.properties.enabled.type").String())
				require.True(t, gjson.GetBytes(schema, "properties.value.nullable").Bool())
			} else {
				require.Len(t, gjson.GetBytes(schema, "properties.value.anyOf").Array(), 2)
			}
			require.Equal(t, "STRING", gjson.GetBytes(schema, "properties.anyOf.type").String())
			require.Len(t, gjson.GetBytes(request.GenerationConfig.ResponseSchema, "properties.value.anyOf").Array(), 2)
			after, _ := json.Marshal(request.Contents[1].Parts[0].FunctionCall.Args)
			require.JSONEq(t, string(args), string(after))
			first := string(schema)
			require.NoError(t, adapter.patchGeminiRequest(t.Context(), &request, &llm.Request{Model: model}))
			require.JSONEq(t, first, string(request.Tools[0].FunctionDeclarations[0].Parameters))
		})
	}
}

func TestClaudeToolUnionsDoNotMutateSource(t *testing.T) {
	for _, raw := range []string{
		`{"nullable":true,"anyOf":[{"type":"string"},{"type":"array","items":{"type":"number"}}]}`,
		`{"anyOf":[{"type":"string"},{"type":"integer"}],"oneOf":[{"type":"boolean"},{"type":"null"}]}`,
		`{"type":"object","properties":{"a":{"type":"string"},"b":{"type":"integer"}},"anyOf":[{"required":["a"]},{"required":["b"]}]}`,
	} {
		var schema map[string]any
		require.NoError(t, json.Unmarshal([]byte(raw), &schema))
		result := sanitizeClaudeToolSchema(schema)
		encoded, err := json.Marshal(result)
		require.NoError(t, err)
		require.False(t, gjson.GetBytes(encoded, "anyOf").Exists())
		require.False(t, gjson.GetBytes(encoded, "oneOf").Exists())
		require.Equal(t, result, sanitizeClaudeToolSchema(result))
		original, err := json.Marshal(schema)
		require.NoError(t, err)
		require.JSONEq(t, raw, string(original))
	}
}
