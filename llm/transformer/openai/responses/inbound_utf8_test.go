package responses

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer"
)

func TestResponsesRejectInvalidUTF8BeforeTransportConversion(t *testing.T) {
	for _, inbound := range []*InboundTransformer{NewInboundTransformer(), NewWebSocketInboundTransformer()} {
		t.Run(string(inbound.APIFormat()), func(t *testing.T) {
			body := append([]byte(`{"type":"response.create","model":"test","input":"`), 0xff)
			body = append(body, []byte(`"}`)...)
			request, err := inbound.TransformRequest(t.Context(), &httpclient.Request{Body: body})
			require.Nil(t, request)
			require.ErrorIs(t, err, transformer.ErrInvalidRequest)
			require.Contains(t, err.Error(), "UTF-8")

			request, err = inbound.TransformRequest(t.Context(), &httpclient.Request{
				Body: []byte(`{"type":"response.create","model":"test","input":"中文 🎈"}`),
			})
			require.NoError(t, err)
			require.Equal(t, "中文 🎈", *request.Messages[0].Content.Content)
		})
	}
}
