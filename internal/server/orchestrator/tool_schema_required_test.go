package orchestrator

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
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

func TestNormalizeNULPatternRetainsConstraints(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{`^[^\0]*$`, `^[^\x00]*$`},
		{`\\0`, `\\0`},
		{`\01`, `\01`},
		{`[0-9]+`, `[0-9]+`},
	} {
		require.Equal(t, tc.want, normalizeNULPattern(tc.input))
	}
}

func TestRepairToolSchemaMiddlewarePreservesLargeNumbers(t *testing.T) {
	body := []byte(`{"seed":9007199254740993,"tools":[{"name":"f","input_schema":{"type":"object","required":null,"properties":{"path":{"type":"string","pattern":"^[^\\0]*$"}}}}]}`)
	request := &httpclient.Request{APIFormat: string(llm.APIFormatAnthropicMessage), Body: body}
	repaired, err := repairInvalidOpenAIToolSchemas().OnOutboundRawRequest(t.Context(), request)
	require.NoError(t, err)
	require.Equal(t, "9007199254740993", gjson.GetBytes(repaired.Body, "seed").Raw)
	require.False(t, gjson.GetBytes(repaired.Body, "tools.0.input_schema.required").Exists())
	require.Equal(t, `^[^\x00]*$`, gjson.GetBytes(repaired.Body, "tools.0.input_schema.properties.path.pattern").String())
}
