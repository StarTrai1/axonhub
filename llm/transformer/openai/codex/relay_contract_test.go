package codex

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

func TestRelayResponsesContractLiftsLiteToolsWithoutChangingHistory(t *testing.T) {
	body := []byte(`{"model":"gpt-6.1-sol","input":[{"type":"additional_tools","role":"developer","tools":[{"type":"namespace","name":"functions","tools":[{"type":"function","name":"read","parameters":{"type":"object","properties":{"offset":{"minimum":9007199254740993}}}}]},{"type":"namespace","name":"clock","tools":[{"type":"function","name":"sleep"}]}]},{"type":"message","role":"developer","content":"keep this instruction"},{"type":"function_call","namespace":"functions","name":"read","call_id":"call_1","arguments":"{\"offset\":9007199254740993}"},{"type":"function_call_output","call_id":"call_1","output":"original result"},{"type":"compaction","id":"cmp_native","encrypted_content":"opaque-state"}],"reasoning":{"effort":"high","context":"all_turns"},"parallel_tool_calls":false}`)
	original := string(body)
	request := &httpclient.Request{Body: body, JSONBody: body, Headers: http.Header{responses.ResponsesLiteHeader: {"true"}}}
	relay := &OutboundTransformer{}
	result := relay.normalizeResponsesContract(request)
	require.Equal(t, original, string(request.Body))
	require.Equal(t, result.Body, result.JSONBody)
	require.Equal(t, "read", gjson.GetBytes(result.Body, "tools.0.name").String())
	require.Equal(t, "clock", gjson.GetBytes(result.Body, "tools.1.name").String())
	require.Equal(t, "9007199254740993", gjson.GetBytes(result.Body, "tools.0.parameters.properties.offset.minimum").Raw)
	require.Equal(t, "keep this instruction", gjson.GetBytes(result.Body, "input.0.content").String())
	require.False(t, gjson.GetBytes(result.Body, "input.1.namespace").Exists())
	require.Equal(t, "original result", gjson.GetBytes(result.Body, "input.2.output").String())
	require.Equal(t, "opaque-state", gjson.GetBytes(result.Body, "input.3.encrypted_content").String())
	require.False(t, gjson.GetBytes(result.Body, "reasoning.context").Exists())
	require.Equal(t, "high", gjson.GetBytes(result.Body, "reasoning.effort").String())
	require.Equal(t, gjson.False, gjson.GetBytes(result.Body, "parallel_tool_calls").Type)
	require.Equal(t, result.Body, relay.normalizeResponsesContract(result).Body)
	official := &OutboundTransformer{official: true}
	require.Same(t, request, official.normalizeResponsesContract(request))
}

func TestRelayResponsesContractUserOnlyProbe(t *testing.T) {
	request := &httpclient.Request{Body: []byte(`{"model":"future-codex","input":[{"role":"user","content":"hello"}],"tool_choice":"auto"}`)}
	result := (&OutboundTransformer{}).normalizeResponsesContract(request)
	require.Equal(t, gjson.String, gjson.GetBytes(result.Body, "instructions").Type)
	require.Equal(t, "[]", gjson.GetBytes(result.Body, "tools").Raw)
	require.True(t, gjson.GetBytes(result.Body, "parallel_tool_calls").Bool())
	require.False(t, gjson.GetBytes(result.Body, "tool_choice").Exists())
}

func TestRelayResponsesContractKeepsOtherEndpoints(t *testing.T) {
	for _, kind := range []llm.RequestType{llm.RequestTypeAlphaSearch, llm.RequestTypeCompact} {
		request := &httpclient.Request{RequestType: string(kind), Body: []byte(`{"model":"gpt-6-astra","input":[]}`)}
		require.Same(t, request, (&OutboundTransformer{}).normalizeResponsesContract(request))
	}
}

func TestRelayToolDeclarationsMergeNamespaceUpdates(t *testing.T) {
	body := []byte(`{"tools":[{"type":"namespace","name":"clock","tools":[{"type":"function","name":"read"}]}],"input":[{"type":"additional_tools","tools":[{"type":"namespace","name":"clock","tools":[{"type":"function","name":"sleep"}]}]},{"type":"message","role":"user","content":"continue"}]}`)
	result := liftRelayToolDeclarations(body)
	require.Equal(t, "read", gjson.GetBytes(result, "tools.0.tools.0.name").String())
	require.Equal(t, "sleep", gjson.GetBytes(result, "tools.0.tools.1.name").String())
	require.Equal(t, "continue", gjson.GetBytes(result, "input.0.content").String())
	malformed := []byte(`{"input":[{"type":"additional_tools","tools":["invalid"]}]}`)
	require.Equal(t, malformed, liftRelayToolDeclarations(malformed))
}

func TestThirdPartyModelCatalogDetectsPublicList(t *testing.T) {
	models, err := ParseThirdPartyModelCatalog([]byte(`{"success":true,"data":[{"id":"gpt-6.1-sol","object":"model"},{"id":"custom-model"},{"id":"gpt-6.1-sol"}]}`))
	require.NoError(t, err)
	require.Equal(t, []string{"gpt-6.1-sol", "custom-model"}, models)
	for _, body := range []string{`{"data":null}`, `{"data":[]}`, `{"data":[{}]}`, `{"data":[{"id":12}]}`, `{"data":{},"models":[{"slug":"do-not-mask-invalid-data"}]}`} {
		_, err := ParseThirdPartyModelCatalog([]byte(body))
		require.Error(t, err)
	}
}
