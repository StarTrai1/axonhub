package codex

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm/httpclient"
)

func TestCodexNumericToolBounds(t *testing.T) {
	for _, tc := range []struct {
		number  string
		removed bool
	}{
		{"9223372036854775807", false},
		{"9223372036854775808", true},
		{"-9223372036854775808", false},
		{"-9223372036854775809", true},
		{"9.223372036854775807e18", false},
		{"9.223372036854775808e18", true},
		{"9223372036854775808.5", false},
		{"0e999999999999999999999", false},
		{"1e999999999999999999999", true},
		{"1e-999999999999999999999", false},
	} {
		t.Run(tc.number, func(t *testing.T) {
			require.Equal(t, tc.removed, isNumericBoundOutOfInt64(json.Number(tc.number)))
		})
	}
	schema := `{"type":"object","properties":{"maximum":{"type":"integer","maximum":9223372036854775808,"minimum":-9223372036854775808,"default":{"maximum":9223372036854775808}},"example":{"anyOf":[{"type":"integer","exclusiveMaximum":1e1000000000},{"type":"string"}]}},"examples":[{"maximum":9223372036854775808}]}`
	for _, envelope := range []string{
		`{"tools":[{"type":"function","name":"f","parameters":%s}]}`,
		`{"tools":[{"type":"namespace","name":"n","tools":[{"type":"function","name":"f","parameters":%s}]}]}`,
		`{"input":[{"type":"additional_tools","tools":[{"type":"function","name":"f","parameters":%s}]}]}`,
		`{"input":[{"type":"tool_search_output","tools":[{"type":"function","name":"f","parameters":%s}]}]}`,
	} {
		request := &httpclient.Request{Body: []byte(fmt.Sprintf(envelope, schema))}
		before := string(request.Body)
		result := (&OutboundTransformer{}).FinalizeTransportRequest(request)
		require.Equal(t, before, string(request.Body), "transport preparation must not mutate the source body")
		require.NotContains(t, string(result.Body), `"exclusiveMaximum"`)
		require.Contains(t, string(result.Body), `"minimum":-9223372036854775808`)
		require.Contains(t, string(result.Body), `"default":{"maximum":9223372036854775808}`)
		require.Contains(t, string(result.Body), `"examples":[{"maximum":9223372036854775808}]`)
	}
	request := &httpclient.Request{Body: []byte(`{"input":[{"type":"function_call","arguments":"{\"maximum\":9223372036854775808}"}],"metadata":{"maximum":9223372036854775808}}`)}
	require.Same(t, request, normalizeNumericToolBounds(request))
	require.Equal(t, "9223372036854775808", gjson.GetBytes(request.Body, "metadata.maximum").Raw)
}
