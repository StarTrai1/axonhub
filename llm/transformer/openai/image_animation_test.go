package openai

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer"
)

func TestImageAnimationEditRejectsBeforeImageProcessing(t *testing.T) {
	var form bytes.Buffer
	writer := multipart.NewWriter(&form)
	require.NoError(t, writer.WriteField("output_format", "gif"))
	require.NoError(t, writer.Close())
	for _, raw := range []*httpclient.Request{
		{Headers: http.Header{"Content-Type": {"application/json"}}, Body: []byte(`{"output_format":"gif"}`)},
		{Headers: http.Header{"Content-Type": {writer.FormDataContentType()}}, Body: form.Bytes()},
	} {
		_, err := NewImageEditInboundTransformer().TransformRequest(t.Context(), raw)
		require.ErrorIs(t, err, transformer.ErrInvalidRequest)
		require.Contains(t, err.Error(), "images/generations")
	}
}

func TestImageAnimationGenerationRoundTrip(t *testing.T) {
	for _, fields := range []string{"", `,"frame_count":24,"frame_duration_ms":125,"size":"480x640","quality":"max","complexity":"medium"`} {
		raw := &httpclient.Request{Headers: http.Header{"Content-Type": {"application/json"}}, Body: []byte(`{"model":"gpt-image-2.6-sunburst","prompt":"loop","output_format":"gif"` + fields + `}`)}
		req, err := NewImageGenerationInboundTransformer().TransformRequest(t.Context(), raw)
		require.NoError(t, err)
		out, err := NewOutboundTransformer("https://images.example/v1", "synthetic")
		require.NoError(t, err)
		wire, err := out.TransformRequest(t.Context(), req)
		require.NoError(t, err)
		require.JSONEq(t, string(raw.Body), string(wire.Body))
		require.Equal(t, "https://images.example/v1/images/generations", wire.URL)
	}
}

func TestImageAnimationRejectsInvalidCombinations(t *testing.T) {
	for _, tc := range []struct{ fields, message string }{
		{`"output_format":"png","frame_count":9`, "require output_format=gif"},
		{`"output_format":"gif","frame_count":1`, "frame_count"},
		{`"output_format":"gif","frame_duration_ms":19`, "frame_duration_ms"},
		{`"output_format":"gif","output_compression":0`, "output_compression"},
		{`"output_format":"gif","partial_images":1`, "partial_images"},
		{`"output_format":"gif","stream":true`, "streaming"},
	} {
		raw := &httpclient.Request{Headers: http.Header{"Content-Type": {"application/json"}}, Body: []byte(`{"model":"gpt-image-2.6-sunburst","prompt":"loop",` + tc.fields + `}`)}
		in := NewImageGenerationInboundTransformer()
		_, err := in.TransformRequest(t.Context(), raw)
		require.ErrorIs(t, err, transformer.ErrInvalidRequest)
		require.Contains(t, err.Error(), tc.message)
		response := in.TransformError(t.Context(), err)
		require.Equal(t, http.StatusBadRequest, response.StatusCode)
		require.Contains(t, gjson.GetBytes(response.Body, "error.message").String(), tc.message)
	}
}
