package antigravity

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSanitizeStringConstIntersectsEnum(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		want string
	}{
		{"overlap", `{"const":"a","enum":["a","b"]}`, `["a"]`},
		{"disjoint", `{"const":"a","enum":["b"]}`, `[]`},
		{"empty", `{"const":"a","enum":[]}`, `[]`},
		{"mixed", `{"const":"1","enum":[1,"1","1"]}`, `["1"]`},
		{"constant only", `{"const":"a"}`, `["a"]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var property map[string]any
			require.NoError(t, json.Unmarshal([]byte(tc.raw), &property))
			schema := map[string]any{"type": "object", "properties": map[string]any{"mode": property}}
			result := SanitizeJSONSchema(schema)
			properties := result["properties"].(map[string]any)
			mode := properties["mode"].(map[string]any)
			encoded, err := json.Marshal(mode["enum"])
			require.NoError(t, err)
			require.JSONEq(t, tc.want, string(encoded))
			require.NotContains(t, mode, "const")
			require.Contains(t, property, "const", "caller schema must remain unchanged")
		})
	}
}
