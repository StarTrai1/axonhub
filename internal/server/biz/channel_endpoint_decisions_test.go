package biz

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/transformer/openai/decisions"
)

func TestDecisionsChannelEndpointTransport(t *testing.T) {
	for _, tc := range []struct {
		kind             channel.Type
		base, path, want string
	}{
		{channel.TypeOpenai, "https://api.openai.com/v1", "", "https://api.openai.com/v1/decisions"},
		{channel.TypeOpenaiResponses, "wss://api.openai.com/v1", "", "https://api.openai.com/v1/decisions"},
		{channel.TypeZenmux, "https://example.invalid/gateway", "/decide", "https://example.invalid/gateway/decide"},
	} {
		t.Run(string(tc.kind), func(t *testing.T) {
			client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
			defer client.Close()
			ctx := authz.WithTestBypass(context.Background())
			create := client.Channel.Create().SetType(tc.kind).SetName("Decisions").SetBaseURL(tc.base).
				SetCredentials(objects.ChannelCredentials{APIKey: "fixture"}).SetSupportedModels([]string{"gpt-6-luna"}).SetDefaultTestModel("gpt-6-luna")
			if tc.path != "" {
				create.SetEndpoints([]objects.ChannelEndpoint{{APIFormat: llm.APIFormatOpenAIDecisions.String(), Path: tc.path}})
			}
			ch, err := create.Save(ctx)
			require.NoError(t, err)
			svc := NewChannelServiceForTest(client)
			built, err := svc.buildChannelWithOutbounds(ch)
			require.NoError(t, err)
			out, err := BuildOutboundByAPIFormat(built, llm.APIFormatOpenAIDecisions.String())
			require.NoError(t, err)
			require.IsType(t, &decisions.OutboundTransformer{}, out)
			wire, err := out.TransformRequest(ctx, &llm.Request{Model: "gpt-6-luna", RequestType: llm.RequestTypeDecisions, Decisions: []byte(`{"model":"alias","input":"hello","questions":[{"type":"predicate","instructions":"Is this a greeting?"}]}`)})
			require.NoError(t, err)
			require.Equal(t, tc.want, wire.URL)
			require.Equal(t, llm.APIFormatOpenAIDecisions.String(), wire.APIFormat)
		})
	}
	require.Error(t, ValidateEndpoints([]objects.ChannelEndpoint{{APIFormat: llm.APIFormatOpenAIDecisions.String(), Transport: objects.ChannelEndpointTransportWebSocket}}))
}

func TestValidateEndpoints_AllowsManualDecisionsEndpoint(t *testing.T) {
	// Given a manually configured Decisions endpoint
	endpoints := []objects.ChannelEndpoint{{APIFormat: llm.APIFormatOpenAIDecisions.String()}}

	// When endpoint configuration is validated
	err := ValidateEndpoints(endpoints)

	// Then the opt-in format is accepted without becoming a default
	require.NoError(t, err)
}
