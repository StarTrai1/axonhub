package antigravity

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLocalSchemaReferencesRetainNestedToolFields(t *testing.T) {
	const raw = `{"type":"object","$defs":{"record/value":{"type":"object","properties":{"value":{"type":"number"}},"required":["value"]}},"properties":{"first":{"$ref":"#/$defs/record~1value"},"second":{"$ref":"#/$defs/record~1value","description":"second record"}},"required":["first","second"]}`
	var source map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &source))
	result := SanitizeJSONSchema(source)
	properties := result["properties"].(map[string]any)
	for _, name := range []string{"first", "second"} {
		node := properties[name].(map[string]any)
		require.Equal(t, "object", node["type"])
		require.Equal(t, "number", node["properties"].(map[string]any)["value"].(map[string]any)["type"])
		require.Equal(t, []any{"value"}, node["required"])
	}
	require.Equal(t, "second record", properties["second"].(map[string]any)["description"])
	encoded, err := json.Marshal(source)
	require.NoError(t, err)
	require.JSONEq(t, raw, string(encoded), "caller source remains unchanged")
}

func TestLocalSchemaReferenceWalkKeepsLiteralValuesAndTerminatesCycles(t *testing.T) {
	const raw = `{"type":"object","$defs":{"node":{"type":"object","properties":{"next":{"$ref":"#/$defs/node"}}}},"properties":{"$ref":{"type":"string"},"tree":{"$ref":"#/$defs/node"},"literal":{"type":"string","default":{"$ref":"#/$defs/node"}}}}`
	var source map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &source))
	result := convertRefsToHints(inlineKnownSchemaRefs(deepCopy(source)))
	properties := result["properties"].(map[string]any)
	require.Equal(t, map[string]any{"type": "string"}, properties["$ref"])
	require.Equal(t, map[string]any{"$ref": "#/$defs/node"}, properties["literal"].(map[string]any)["default"])
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.Less(t, len(encoded), 4096)
	require.Contains(t, string(encoded), "See: node")
}
