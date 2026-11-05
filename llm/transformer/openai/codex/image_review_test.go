package codex

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/oauth"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/transformer"
	"github.com/looplj/axonhub/llm/transformer/openai"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

func TestCodexGenericImagesReturnsConfigurationError(t *testing.T) {
	// Given
	out, err := NewOutboundTransformer(Params{TokenProvider: failingTokenGetter{}})
	require.NoError(t, err)
	// When
	_, err = out.TransformRequest(t.Context(), &llm.Request{RequestType: llm.RequestTypeImage, Image: &llm.ImageRequest{Prompt: "draw"}})
	// Then
	require.ErrorIs(t, err, transformer.ErrInvalidRequest)
	require.Contains(t, err.Error(), "endpoint")
}

func TestCodexNonstreamingTokenFailureSendsZeroHTTP(t *testing.T) {
	// Given: a real HTTP executor and a failing token getter for nonstreaming Images.
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(http.StatusOK) }))
	defer server.Close()
	out, err := NewImageOutboundTransformer(ImageParams{TokenProvider: failingTokenGetter{}, BaseURL: server.URL})
	require.NoError(t, err)
	pipe := pipeline.NewFactory(httpclient.NewHttpClient()).Pipeline(openai.NewImageGenerationInboundTransformer(), out)
	// When
	_, err = pipe.Process(t.Context(), &httpclient.Request{Method: http.MethodPost, Headers: http.Header{"Content-Type": {"application/json"}}, Body: []byte(`{"model":"gpt-image-2","prompt":"draw","stream":false}`)})
	// Then
	require.ErrorContains(t, err, "refresh failed")
	require.Zero(t, calls.Load())
}

func TestCodexGenericExecutorRejectsImagesBeforeHTTP(t *testing.T) {
	// Given
	inner := &mockCodexExecutor{}
	executor := (&OutboundTransformer{}).CustomizeExecutor(inner)
	// When
	_, err := executor.Do(t.Context(), &httpclient.Request{RequestType: llm.RequestTypeImage.String()})
	// Then
	require.ErrorIs(t, err, transformer.ErrInvalidRequest)
	require.Zero(t, inner.doCalls.Load())
	require.Zero(t, inner.doStreamCalls.Load())
}

func TestCodexNativeResponsesImageToolPreserved(t *testing.T) {
	// Given
	body := []byte(`{"model":"gpt-5.5","input":"synthetic","tools":[{"type":"image_generation","quality":"low","size":"1024x1024","output_format":"png"}],"tool_choice":{"type":"image_generation"}}`)
	req, err := responses.NewInboundTransformer().TransformRequest(t.Context(), &httpclient.Request{Headers: http.Header{"Content-Type": {"application/json"}}, Body: body})
	require.NoError(t, err)
	out, err := NewOutboundTransformer(Params{BaseURL: "https://relay.example/v1", TokenProvider: oauth.NewStaticTokenProvider(&oauth.OAuthCredentials{AccessToken: "test-token"})})
	require.NoError(t, err)
	// When
	encoded, err := out.TransformRequest(t.Context(), req)
	// Then
	require.NoError(t, err)
	require.JSONEq(t, gjson.GetBytes(body, "tools").Raw, gjson.GetBytes(encoded.Body, "tools").Raw)
	require.JSONEq(t, gjson.GetBytes(body, "tool_choice").Raw, gjson.GetBytes(encoded.Body, "tool_choice").Raw)
	require.Equal(t, "https://relay.example/v1/responses", encoded.URL)
}

func TestCodexImageGIFCapabilityRejectionPreservesDetail(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-ID", "synthetic-gif-rejection")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"detail":"GIF output is not enabled for this request."}`))
	}))
	defer server.Close()
	out, err := NewImageOutboundTransformer(ImageParams{BaseURL: server.URL, TokenProvider: oauth.NewStaticTokenProvider(&oauth.OAuthCredentials{AccessToken: "synthetic"})})
	require.NoError(t, err)
	in := openai.NewImageGenerationInboundTransformer()
	pipe := pipeline.NewFactory(httpclient.NewHttpClient()).Pipeline(in, out)
	_, err = pipe.Process(t.Context(), &httpclient.Request{Method: http.MethodPost, Headers: http.Header{"Content-Type": {"application/json"}}, Body: []byte(`{"model":"gpt-image-2.6-sunburst","prompt":"a bouncing ball","output_format":"gif","n":1}`)})
	require.Error(t, err)
	response := in.TransformError(t.Context(), err)
	require.Equal(t, http.StatusBadRequest, response.StatusCode)
	require.Equal(t, "GIF output is not enabled for this request.", gjson.GetBytes(response.Body, "error.message").String())
	require.Equal(t, "image_output_format_not_enabled", gjson.GetBytes(response.Body, "error.code").String())
	require.Equal(t, "output_format", gjson.GetBytes(response.Body, "error.param").String())
	require.Equal(t, "synthetic-gif-rejection", response.Headers.Get("X-Request-ID"))
	require.Equal(t, int32(1), calls.Load())
}

func TestCodexImageErrorsKeepStandardEnvelopeAndIgnoreStructuredDetail(t *testing.T) {
	out, err := NewImageOutboundTransformer(ImageParams{TokenProvider: failingTokenGetter{}})
	require.NoError(t, err)
	for _, tc := range []struct{ body, message string }{
		{`{"error":{"message":"original error","code":"original"},"detail":"other"}`, "original error"},
		{`{"detail":[{"input":"private request"}]}`, "Bad Request"},
		{`{"detail":""}`, "Bad Request"},
	} {
		err := out.TransformError(t.Context(), &httpclient.Error{StatusCode: http.StatusBadRequest, Body: []byte(tc.body)})
		require.Equal(t, tc.message, err.Detail.Message)
	}
}
