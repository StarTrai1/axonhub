package antigravity

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
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
