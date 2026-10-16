package antigravity

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSanitizeDiscriminatorKeepsNamedProperty(t *testing.T) {
	schema := map[string]any{"type": "object", "discriminator": map[string]any{"propertyName": "kind"}, "properties": map[string]any{"discriminator": map[string]any{"type": "string"}, "nested": map[string]any{"type": "object", "discriminator": map[string]any{"propertyName": "kind"}, "properties": map[string]any{"kind": map[string]any{"type": "string"}}}}}
	cleaned := SanitizeJSONSchema(schema)
	require.NotContains(t, cleaned, "discriminator")
	properties := cleaned["properties"].(map[string]any)
	require.Contains(t, properties, "discriminator")
	require.NotContains(t, properties["nested"].(map[string]any), "discriminator")
}
