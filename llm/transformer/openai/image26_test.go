package openai_test

import (
	"bytes"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer/openai"
)

func TestImage26ExactModelAndComplexityRoundTrip(t *testing.T) {
	for _, model := range []string{"gpt-image-2.6-sunburst", "gpt-image-2.6-flare"} {
		for _, mode := range []string{"generation", "json-edit", "multipart-edit"} {
			t.Run(model+"/"+mode, func(t *testing.T) {
				body := []byte(fmt.Sprintf(`{"model":%q,"prompt":"synthetic","complexity":"medium","quality":"max"}`, model))
				inbound := openai.NewImageGenerationInboundTransformer()
				if mode == "json-edit" {
					inbound = openai.NewImageEditInboundTransformer()
					body = []byte(fmt.Sprintf(`{"model":%q,"prompt":"synthetic","images":[{"file_id":"file-synthetic"}],"complexity":"medium","quality":"max"}`, model))
				}
				headers := http.Header{"Content-Type": {"application/json"}}
				if mode == "multipart-edit" {
					inbound = openai.NewImageEditInboundTransformer()
					var buffer bytes.Buffer
					writer := multipart.NewWriter(&buffer)
					for name, value := range map[string]string{"model": model, "prompt": "synthetic", "complexity": "medium", "quality": "max"} {
						require.NoError(t, writer.WriteField(name, value))
					}
					part, err := writer.CreatePart(textproto.MIMEHeader{
						"Content-Disposition": {`form-data; name="image"; filename="test.png"`},
						"Content-Type":        {"image/png"},
					})
					require.NoError(t, err)
					_, err = part.Write([]byte("synthetic-image"))
					require.NoError(t, err)
					require.NoError(t, writer.Close())
					headers.Set("Content-Type", writer.FormDataContentType())
					body = buffer.Bytes()
				}
				raw := &httpclient.Request{Method: http.MethodPost, Body: body, Headers: headers}
				request, err := inbound.TransformRequest(t.Context(), raw)
				require.NoError(t, err)
				outbound, err := openai.NewOutboundTransformer("https://images.example.test/v1", "synthetic")
				require.NoError(t, err)
				wire, err := outbound.TransformRequest(t.Context(), request)
				require.NoError(t, err)
				if mode != "multipart-edit" {
					require.Equal(t, model, gjson.GetBytes(wire.Body, "model").String())
					require.Equal(t, "medium", gjson.GetBytes(wire.Body, "complexity").String())
					require.Equal(t, "max", gjson.GetBytes(wire.Body, "quality").String())
				} else {
					_, params, err := mime.ParseMediaType(wire.Headers.Get("Content-Type"))
					require.NoError(t, err)
					reader := multipart.NewReader(bytes.NewReader(wire.Body), params["boundary"])
					fields := map[string]string{}
					for {
						part, err := reader.NextPart()
						if err == io.EOF {
							break
						}
						require.NoError(t, err)
						value, err := io.ReadAll(part)
						require.NoError(t, err)
						fields[part.FormName()] = string(value)
					}
					require.Equal(t, model, fields["model"])
					require.Equal(t, "medium", fields["complexity"])
					require.Equal(t, "max", fields["quality"])
				}
			})
		}
	}
}
