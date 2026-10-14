package antigravity

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNativeSchemaUnionAlternatives(t *testing.T) {
	for _, tc := range []struct{ name, input, want string }{
		{"nested choices", `{"type":"array","items":{"anyOf":[{"type":"integer"},{"type":"boolean"}]}}`, `{"type":"array","items":{"anyOf":[{"type":"integer"},{"type":"boolean"}]}}`},
		{"nullable choices", `{"anyOf":[{"type":"string"},{"type":"number"},{"type":"null"}]}`, `{"anyOf":[{"type":"string"},{"type":"number"}],"nullable":true}`},
		{"native oneOf", `{"oneOf":[{"type":"integer"},{"type":"boolean"}]}`, `{"anyOf":[{"type":"integer"},{"type":"boolean"}],"description":"Exactly one alternative must match"}`},
		{"required alternatives", `{"type":"object","properties":{"a":{"type":"string"},"b":{"type":"integer"}},"anyOf":[{"required":["a"]},{"required":["b"]}]}`, `{"type":"object","properties":{"a":{"type":"string"},"b":{"type":"integer"}},"anyOf":[{"type":"object","properties":{"a":{"type":"string"}},"required":["a"]},{"type":"object","properties":{"b":{"type":"integer"}},"required":["b"]}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var schema map[string]any
			require.NoError(t, json.Unmarshal([]byte(tc.input), &schema))
			result, err := json.Marshal(SanitizeJSONSchema(schema))
			require.NoError(t, err)
			require.JSONEq(t, tc.want, string(result))
			unchanged, err := json.Marshal(schema)
			require.NoError(t, err)
			require.JSONEq(t, tc.input, string(unchanged))
		})
	}
}
