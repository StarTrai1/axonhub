package antigravity

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
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
