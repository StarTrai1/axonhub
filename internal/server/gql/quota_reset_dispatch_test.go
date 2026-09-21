package gql

import (
	"context"
	"testing"

	"github.com/99designs/gqlgen/client"
	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/objects"
)

// This resolver replaces the entire reset business path. These tests cannot
// reach an HTTP client, load credentials, or consume a real reset credit.
type quotaResetDispatchRecorder struct {
	ResolverRoot
	MutationResolver

	channelID objects.GUID
	creditID  *string
	calls     int
}

func (r *quotaResetDispatchRecorder) Mutation() MutationResolver { return r }

func (r *quotaResetDispatchRecorder) ResetChannelQuotaNow(_ context.Context, channelID objects.GUID, creditID *string) (bool, error) {
	r.channelID = channelID
	r.creditID = creditID
	r.calls++
	return true, nil
}

func TestQuotaResetGraphQLDispatchPreservesSelectedCredit(t *testing.T) {
	tests := []struct {
		name      string
		selection string
		creditID  *string
	}{
		{name: "selected credit", selection: `creditID: "credit-selected"`, creditID: lo.ToPtr("credit-selected")},
		{name: "explicit null", selection: "creditID: null"},
		{name: "omitted optional argument"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := &quotaResetDispatchRecorder{}
			server := handler.NewDefaultServer(NewExecutableSchema(Config{Resolvers: recorder}))
			graphqlClient := client.New(server)
			var response struct{ ResetChannelQuotaNow bool }
			err := graphqlClient.Post(`mutation {
				resetChannelQuotaNow(channelID: "gid://axonhub/Channel/39" `+tt.selection+`)
			}`, &response)
			require.NoError(t, err)
			require.True(t, response.ResetChannelQuotaNow)
			require.Equal(t, 1, recorder.calls)
			require.Equal(t, objects.GUID{Type: "Channel", ID: 39}, recorder.channelID)
			require.Equal(t, tt.creditID, recorder.creditID)
		})
	}
}

func TestQuotaResetGraphQLDispatchAcceptsFrontendVariables(t *testing.T) {
	recorder := &quotaResetDispatchRecorder{}
	server := handler.NewDefaultServer(NewExecutableSchema(Config{Resolvers: recorder}))
	graphqlClient := client.New(server)
	var response struct{ ResetChannelQuotaNow bool }
	err := graphqlClient.Post(`mutation ResetChannelQuotaNow($channelID: ID!, $creditID: String) {
		resetChannelQuotaNow(channelID: $channelID, creditID: $creditID)
	}`, &response, client.Var("channelID", "gid://axonhub/Channel/73"), client.Var("creditID", "credit-specific"))
	require.NoError(t, err)
	require.Equal(t, 1, recorder.calls)
	require.Equal(t, objects.GUID{Type: "Channel", ID: 73}, recorder.channelID)
	require.Equal(t, lo.ToPtr("credit-specific"), recorder.creditID)
}
