package openai_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm/auth"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer"
	"github.com/looplj/axonhub/llm/transformer/openai"
)

func TestImageMarkupJSONReferencesRoundTrip(t *testing.T) {
	for _, markup := range []string{`{"file_id":"file-overlay"}`, `{"image_url":"https://example.invalid/overlay.png"}`} {
		body := fmt.Sprintf(`{"model":"gpt-image-2.6-sunburst","prompt":"edit marked area","images":[{"file_id":"file-original"}],"markup":%s}`, markup)
		in, err := openai.NewImageEditInboundTransformer().TransformRequest(t.Context(), &httpclient.Request{Headers: http.Header{"Content-Type": {"application/json"}}, Body: []byte(body)})
		require.NoError(t, err)
		out, err := openai.NewOutboundTransformer("https://example.invalid/v1", "synthetic")
		require.NoError(t, err)
		wire, err := out.TransformRequest(t.Context(), in)
		require.NoError(t, err)
		require.JSONEq(t, markup, gjson.GetBytes(wire.Body, "markup").Raw)
		require.Equal(t, "gpt-image-2.6-sunburst", gjson.GetBytes(wire.Body, "model").String())
		require.ErrorIs(t, transformer.ValidateImageBytes(in.Image), transformer.ErrUnsupportedConversion)
	}
}

func TestImageMarkupMultipartAndJSONPreserveOverlay(t *testing.T) {
	png, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVQIHWP4z8DwHwAFgAI/ScLbtAAAAABJRU5ErkJggg==")
	require.NoError(t, err)
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	require.NoError(t, w.WriteField("model", "gpt-image-2.6-flare"))
	require.NoError(t, w.WriteField("prompt", "edit marked area"))
	for _, field := range []string{"image", "markup"} {
		part, err := w.CreatePart(textproto.MIMEHeader{"Content-Type": {"image/png"}, "Content-Disposition": {fmt.Sprintf(`form-data; name="%s"; filename="overlay.png"`, field)}})
		require.NoError(t, err)
		_, err = part.Write(png)
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	in, err := openai.NewImageEditInboundTransformer().TransformRequest(t.Context(), &httpclient.Request{Headers: http.Header{"Content-Type": {w.FormDataContentType()}}, Body: body.Bytes()})
	require.NoError(t, err)
	for _, jsonEdit := range []bool{false, true} {
		out, err := openai.NewOutboundTransformerWithConfig(&openai.Config{PlatformType: openai.PlatformOpenAI, BaseURL: "https://example.invalid/v1", APIKeyProvider: auth.NewStaticKeyProvider("synthetic"), ImageEditJSON: jsonEdit})
		require.NoError(t, err)
		wire, err := out.TransformRequest(t.Context(), in)
		require.NoError(t, err)
		if jsonEdit {
			require.Equal(t, "data:image/png;base64,"+base64.StdEncoding.EncodeToString(png), gjson.GetBytes(wire.Body, "markup.image_url").String())
			continue
		}
		_, params, err := mime.ParseMediaType(wire.Headers.Get("Content-Type"))
		require.NoError(t, err)
		reader := multipart.NewReader(bytes.NewReader(wire.Body), params["boundary"])
		found := false
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			require.NoError(t, err)
			data, err := io.ReadAll(part)
			require.NoError(t, err)
			if part.FormName() == "markup" {
				found = true
				require.Equal(t, png, data)
				require.Equal(t, "image/png", part.Header.Get("Content-Type"))
			}
		}
		require.True(t, found)
	}
}

func TestImageMarkupRejectsAmbiguousEdits(t *testing.T) {
	for _, overlay := range []string{`{}`, `"legacy"`, `{"file_id":"overlay","image_url":"https://example.invalid/a.png"}`} {
		body := `{"prompt":"edit","images":[{"file_id":"original"}],"markup":` + overlay + `}`
		_, err := openai.NewImageEditInboundTransformer().TransformRequest(t.Context(), &httpclient.Request{Headers: http.Header{"Content-Type": {"application/json"}}, Body: []byte(body)})
		require.ErrorIs(t, err, transformer.ErrInvalidRequest)
	}
	for _, other := range []string{"mask", "prompt_rewriter"} {
		body, err := json.Marshal(map[string]any{"prompt": "edit", "images": []map[string]string{{"file_id": "original"}}, "markup": map[string]string{"file_id": "overlay"}, other: map[string]string{"file_id": "conflict"}})
		require.NoError(t, err)
		_, err = openai.NewImageEditInboundTransformer().TransformRequest(t.Context(), &httpclient.Request{Headers: http.Header{"Content-Type": {"application/json"}}, Body: body})
		require.ErrorIs(t, err, transformer.ErrInvalidRequest)
	}
}
