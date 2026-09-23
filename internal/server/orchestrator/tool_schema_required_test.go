package orchestrator

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestRepairToolSchemasPreservesRequiredInstanceData(t *testing.T) {
	for _, raw := range []string{
		`{"type":"function","parameters":SCHEMA}`,
		`{"type":"function","function":{"name":"f","parameters":SCHEMA}}`,
		`{"name":"f","input_schema":SCHEMA}`,
	} {
		schema := `{"type":"object","required":null,"properties":{"required":{"type":"string","default":{"required":null}},"cfg":{"contentSchema":{"required":null}}},"allOf":[{"required":null}],"const":{"required":null}}`
		var tool map[string]any
		require.NoError(t, json.Unmarshal([]byte(strings.ReplaceAll(raw, "SCHEMA", schema)), &tool))
		require.True(t, repairToolSchemaList([]any{tool}))
		body, err := json.Marshal(tool)
		require.NoError(t, err)
		prefix := "parameters"
		if gjson.GetBytes(body, "function").Exists() {
			prefix = "function.parameters"
		} else if gjson.GetBytes(body, "input_schema").Exists() {
			prefix = "input_schema"
		}
		require.False(t, gjson.GetBytes(body, prefix+".required").Exists())
		require.False(t, gjson.GetBytes(body, prefix+".allOf.0.required").Exists())
		require.False(t, gjson.GetBytes(body, prefix+".properties.cfg.contentSchema.required").Exists())
		require.True(t, gjson.GetBytes(body, prefix+".properties.required.default.required").Exists())
		require.True(t, gjson.GetBytes(body, prefix+".const.required").Exists())
	}
}
