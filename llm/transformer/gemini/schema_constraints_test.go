package gemini

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer/anthropic"
	"github.com/looplj/axonhub/llm/transformer/openai"
)

func TestGeminiSchemaConstraintsPreserveLiteralData(t *testing.T) {
	raw := json.RawMessage(`{"type":"OBJECT","minProperties":"0","maxProperties":"9223372036854775807","properties":{"minItems":{"type":"STRING","minLength":"1","maxLength":"20","default":{"type":"STRING","minLength":"7"},"enum":["1","2"]},"batch":{"type":"ARRAY","minItems":"2","maxItems":5,"items":{"type":"STRING","minLength":"3"}}},"$defs":{"row":{"type":"OBJECT","maxProperties":"4"}},"anyOf":[{"minProperties":"1"}],"examples":[{"minItems":"9"}],"x-vendor":{"type":"STRING","maxLength":"6"}}`)
	converted := normalizeGeminiSchema(raw)
	require.Equal(t, "object", gjson.GetBytes(converted, "type").String())
	for path, value := range map[string]string{
		"minProperties": "0", "maxProperties": "9223372036854775807",
		"properties.minItems.minLength": "1", "properties.minItems.maxLength": "20",
		"properties.batch.minItems": "2", "properties.batch.maxItems": "5",
		"properties.batch.items.minLength": "3", "$defs.row.maxProperties": "4", "anyOf.0.minProperties": "1",
	} {
		actual := gjson.GetBytes(converted, path)
		require.Equal(t, gjson.Number, actual.Type, path)
		require.Equal(t, value, actual.Raw, path)
	}
	for _, path := range []string{"properties.minItems.default", "properties.minItems.enum", "examples", "x-vendor"} {
		require.JSONEq(t, gjson.GetBytes(raw, path).Raw, gjson.GetBytes(converted, path).Raw, path)
	}
	for _, value := range []string{`"-1"`, `"01"`, `"+1"`, `"1.5"`, `"1e2"`, `"9223372036854775808"`, `null`, `true`, `3`} {
		input := json.RawMessage(`{"minItems":` + value + `}`)
		require.Equal(t, input, normalizeGeminiSchema(input))
	}
	for _, input := range []string{`true`, `false`, `null`, `[]`, `{"type":`} {
		require.Equal(t, json.RawMessage(input), normalizeGeminiSchema(json.RawMessage(input)))
	}
}

func TestGeminiProtobufConstraintsReachOtherProtocols(t *testing.T) {
	const schema = `{"type":"OBJECT","properties":{"batch":{"type":"ARRAY","minItems":"2","items":{"type":"STRING","maxLength":"40"}}}}`
	body := []byte(`{"contents":[{"role":"user","parts":[{"text":"inspect"}]}],"tools":[{"functionDeclarations":[{"name":"inspect","parameters":` + schema + `}]}],"generationConfig":{"responseMimeType":"application/json","responseSchema":` + schema + `}}`)
	request, err := NewInboundTransformer().TransformRequest(t.Context(), &httpclient.Request{
		Path: "/v1beta/models/gemini-3-pro-preview:generateContent",
		Headers: http.Header{"Content-Type": []string{"application/json"}}, Body: body,
	})
	require.NoError(t, err)
	for _, raw := range []json.RawMessage{request.Tools[0].Function.Parameters, json.RawMessage(gjson.GetBytes(request.ResponseFormat.JSONSchema, "schema").Raw)} {
		require.Equal(t, "object", gjson.GetBytes(raw, "type").String())
		require.Equal(t, gjson.Number, gjson.GetBytes(raw, "properties.batch.minItems").Type)
		require.Equal(t, gjson.Number, gjson.GetBytes(raw, "properties.batch.items.maxLength").Type)
	}
	chat, err := openai.NewOutboundTransformer("https://example.com/v1", "fixture")
	require.NoError(t, err)
	wire, err := chat.TransformRequest(t.Context(), request)
	require.NoError(t, err)
	require.Equal(t, gjson.Number, gjson.GetBytes(wire.Body, "tools.0.function.parameters.properties.batch.minItems").Type)
	require.Equal(t, gjson.Number, gjson.GetBytes(wire.Body, "response_format.json_schema.schema.properties.batch.minItems").Type)
	claude, err := anthropic.NewOutboundTransformer("https://example.com", "fixture")
	require.NoError(t, err)
	wire, err = claude.TransformRequest(t.Context(), request)
	require.NoError(t, err)
	require.Equal(t, gjson.Number, gjson.GetBytes(wire.Body, "tools.0.input_schema.properties.batch.items.maxLength").Type)
}
