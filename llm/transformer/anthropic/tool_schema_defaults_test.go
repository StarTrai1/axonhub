package anthropic

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInboundFunctionToolDefaultsOnlyAbsentSchemas(test *testing.T) {
	test.Parallel()
	for _, scenario := range []struct {
		name   string
		schema json.RawMessage
		want   string
	}{
		{name: "omitted", want: `{"type":"object","properties":{}}`},
		{name: "null", schema: json.RawMessage(" null "), want: `{"type":"object","properties":{}}`},
		{name: "explicit object", schema: json.RawMessage(`{"type":"object"}`), want: `{"type":"object"}`},
		{
			name:   "preserve dialect and union",
			schema: json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","$id":"urn:test:tool","type":"object","properties":{"value":{"anyOf":[{"type":"string"},{"type":"number"}]}}}`),
			want:   `{"$schema":"https://json-schema.org/draft/2020-12/schema","$id":"urn:test:tool","type":"object","properties":{"value":{"anyOf":[{"type":"string"},{"type":"number"}]}}}`,
		},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			test.Parallel()
			tool := Tool{Name: "lookup", InputSchema: scenario.schema}
			converted, ok := convertToolToLLM(tool)
			require.True(test, ok)
			require.JSONEq(test, scenario.want, string(converted.Function.Parameters))
			require.Equal(test, scenario.schema, tool.InputSchema)
		})
	}
}
