package decisions

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/auth"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer"
)

const nativeRequest = `{"model":"decision-alias","input":"An invoice was duplicated.","questions":[{"type":"predicate","instructions":"Is this about billing?"},{"type":"choice","name":"team","instructions":"Select a team.","choices":[{"value":"billing","description":"Payments"}]},{"type":"score","name":"severity","instructions":"Rate severity.","levels":[{"label":"Low","description":"Minor"},{"label":"High","description":"Blocking"}]}],"extension":{"integer":9007199254740993}}`

func TestDecisionsNativeRoundTrip(t *testing.T) {
	ctx := context.Background()
	inbound := NewInboundTransformer()
	req, err := inbound.TransformRequest(ctx, &httpclient.Request{Body: []byte(nativeRequest)})
	require.NoError(t, err)
	require.Equal(t, llm.RequestTypeDecisions, req.RequestType)
	require.Equal(t, llm.APIFormatOpenAIDecisions, req.APIFormat)
	req.Model = "gpt-6-luna"
	outbound, err := NewOutboundTransformer(Config{APIKeyProvider: auth.NewStaticKeyProvider("fixture")})
	require.NoError(t, err)
	wire, err := outbound.TransformRequest(ctx, req)
	require.NoError(t, err)
	require.Equal(t, "https://api.openai.com/v1/decisions", wire.URL)
	require.Equal(t, "fixture", wire.Auth.APIKey)
	require.Equal(t, "gpt-6-luna", gjson.GetBytes(wire.Body, "model").String())
	require.Equal(t, "9007199254740993", gjson.GetBytes(wire.Body, "extension.integer").Raw)
	require.Equal(t, gjson.Get(nativeRequest, "questions").Raw, gjson.GetBytes(wire.Body, "questions").Raw)
	require.Equal(t, nativeRequest, string(req.Decisions))
	require.Equal(t, nativeRequest, string(req.RawRequest.Body))

	body := []byte(`{"model":"gpt-6-luna","answers":[{"type":"predicate","name":null,"probability":0.98},{"type":"choice","name":"team","choice":"billing","confidence":0.9,"probabilities":[{"value":"billing","probability":1}]},{"type":"refusal","name":"severity","reason":"fixture"}],"usage":{"input_tokens":42,"input_tokens_details":{"cached_tokens":10,"cache_write_tokens":5},"output_tokens":0,"total_tokens":42},"extra":9007199254740993}`)
	result, err := outbound.TransformResponse(ctx, &httpclient.Response{StatusCode: 200, Body: body})
	require.NoError(t, err)
	require.Equal(t, int64(42), result.Usage.PromptTokens)
	require.Equal(t, int64(10), result.Usage.PromptTokensDetails.CachedTokens)
	result.Model = "decision-alias"
	result.Usage.Cost = lo.ToPtr(0.0000042)
	response, err := inbound.TransformResponse(ctx, result)
	require.NoError(t, err)
	require.Equal(t, "decision-alias", gjson.GetBytes(response.Body, "model").String())
	require.Equal(t, "9007199254740993", gjson.GetBytes(response.Body, "extra").Raw)
	require.Equal(t, gjson.GetBytes(body, "answers").Raw, gjson.GetBytes(response.Body, "answers").Raw)
	require.InDelta(t, 0.0000042, gjson.GetBytes(response.Body, "usage.cost").Float(), 1e-12)
	require.Equal(t, body, []byte(result.Decisions))
}

func TestDecisionsRequestValidation(t *testing.T) {
	valid := []string{nativeRequest, strings.Replace(nativeRequest, `"input":"An invoice was duplicated."`, `"input":[{"role":"user","content":[{"type":"input_text","text":"Inspect"},{"type":"input_image","image_url":"data:image/png;base64,AAAA"}]}]`, 1)}
	for _, body := range valid {
		_, err := NewInboundTransformer().TransformRequest(context.Background(), &httpclient.Request{Body: []byte(body)})
		require.NoError(t, err)
	}
	invalid := []string{
		`null`, `[]`, `{}`, nativeRequest + `{}`,
		strings.Replace(nativeRequest, `"model":`, `"Model":`, 1),
		strings.Replace(nativeRequest, `"model":`, `"model":"other","model":`, 1),
		strings.Replace(nativeRequest, `"input":`, `"input":"a","Input":`, 1),
		strings.Replace(nativeRequest, `"questions":`, `"Questions":`, 1),
		strings.Replace(nativeRequest, `"extension":`, `"stream":true,"Stream":false,"extension":`, 1),
		strings.Replace(nativeRequest, `"extension":`, `"stream":true,"extension":`, 1),
		strings.Replace(nativeRequest, `"questions":[`, `"questions":{`, 1),
		strings.Replace(nativeRequest, `"predicate"`, `"noul"`, 1),
		strings.Replace(valid[1], `"user"`, `"assistant"`, 1),
		strings.Replace(valid[1], `data:image/png;base64,AAAA`, `https://example.invalid/photo.png`, 1),
		strings.Replace(valid[1], `"image_url":`, `"file_id":"file-x","image_url":`, 1),
		strings.Replace(valid[1], `"input_image"`, `"input_file"`, 1),
	}
	for i, body := range invalid {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			_, err := NewInboundTransformer().TransformRequest(context.Background(), &httpclient.Request{Body: []byte(body)})
			require.ErrorIs(t, err, transformer.ErrInvalidRequest)
		})
	}
}

func TestDecisionsImageLimit(t *testing.T) {
	image := `{"type":"input_image","image_url":"data:image/png;base64,AAAA"}`
	for _, count := range []int{128, 129} {
		body := fmt.Sprintf(`{"model":"gpt-6-luna","input":[{"role":"user","content":[%s]}],"questions":[{"type":"predicate","instructions":"Is there damage?"}]}`, strings.TrimSuffix(strings.Repeat(image+",", count), ","))
		_, err := validateRequest([]byte(body))
		if count == 128 {
			require.NoError(t, err)
		} else {
			require.ErrorIs(t, err, transformer.ErrInvalidRequest)
		}
	}
}

func TestDecisionsEndpointAndResponseValidation(t *testing.T) {
	for _, tc := range []struct{ base, path, want string }{
		{"https://example.invalid/", "", "https://example.invalid/v1/decisions"},
		{"https://example.invalid/v1/", "", "https://example.invalid/v1/decisions"},
		{"https://example.invalid/gateway/", "/native/decide", "https://example.invalid/gateway/native/decide"},
	} {
		outbound, err := NewOutboundTransformer(Config{BaseURL: tc.base, EndpointPath: tc.path, APIKeyProvider: auth.NewStaticKeyProvider("fixture")})
		require.NoError(t, err)
		request, err := outbound.TransformRequest(context.Background(), &llm.Request{Model: "gpt-6-luna", RequestType: llm.RequestTypeDecisions, Decisions: json.RawMessage(nativeRequest)})
		require.NoError(t, err)
		require.Equal(t, tc.want, request.URL)
		_, err = outbound.TransformRequest(context.Background(), &llm.Request{Model: "gpt-6-luna", RequestType: llm.RequestTypeChat})
		require.ErrorIs(t, err, transformer.ErrInvalidRequest)
		for _, body := range []string{`{}`, `{"answers":[]}`, `{"answers":[null]}`, `{"answers":[{}]}`, `{"answers":{}}`, `{"answers":[{"type":"predicate"}],"usage":{"input_tokens":-1}}`} {
			_, err = outbound.TransformResponse(context.Background(), &httpclient.Response{StatusCode: 200, Body: []byte(body)})
			require.Error(t, err)
		}
		_, err = outbound.TransformResponse(context.Background(), &httpclient.Response{StatusCode: http.StatusTooManyRequests, Body: []byte(`{"error":{"message":"retry later","type":"rate_limit_error"}}`)})
		require.Error(t, err)
		_, err = outbound.TransformStream(context.Background(), request, nil)
		require.Error(t, err)
	}
}
