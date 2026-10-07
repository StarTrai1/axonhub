package biz

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/llm/oauth"
)

func TestOAuthRefreshPreservesConcurrentCredentials(t *testing.T) {
	for _, when := range []string{"before callback", "during save"} {
		t.Run(when, func(t *testing.T) {
			svc, client := setupTestChannelService(t)
			defer client.Close()
			ctx := authz.WithTestBypass(ent.NewContext(t.Context(), client))
			ch := createTestOAuthChannel(t, client, ctx, "refresh-race")
			callback := svc.onTokenRefreshed(ch)
			replacement := objects.ChannelCredentials{OAuth: &oauth.OAuthCredentials{AccessToken: "new-login", RefreshToken: "new-refresh"}}
			if when == "before callback" {
				_, err := client.Channel.UpdateOneID(ch.ID).SetCredentials(replacement).Save(ctx)
				require.NoError(t, err)
			} else {
				armed := true
				client.Channel.Use(func(next ent.Mutator) ent.Mutator {
					return ent.MutateFunc(func(hookCtx context.Context, mutation ent.Mutation) (ent.Value, error) {
						if armed {
							armed = false
							_, err := client.Channel.UpdateOneID(ch.ID).SetCredentials(replacement).Save(hookCtx)
							if err != nil {
								return nil, err
							}
						}
						return next.Mutate(hookCtx, mutation)
					})
				})
			}
			err := callback(ctx, &oauth.OAuthCredentials{AccessToken: "obsolete-refresh", RefreshToken: "obsolete-next"})
			require.Error(t, err)
			stored, err := client.Channel.Get(ctx, ch.ID)
			require.NoError(t, err)
			require.Equal(t, "new-login", stored.Credentials.OAuth.AccessToken)
			require.Equal(t, "new-refresh", stored.Credentials.OAuth.RefreshToken)
		})
	}
}

func TestOAuthRefreshAllowsSequentialRefreshAndUnrelatedEdits(t *testing.T) {
	svc, client := setupTestChannelService(t)
	defer client.Close()
	ctx := authz.WithTestBypass(ent.NewContext(t.Context(), client))
	ch := createTestOAuthChannel(t, client, ctx, "refresh-sequence")
	original := ch.Credentials.OAuth.AccessToken
	callback := svc.onTokenRefreshed(ch)
	expires := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	disabled := []objects.DisabledAPIKey{{Key: objects.OAuthCredentialRef, ErrorCode: 429, ExpiresAt: &expires}}
	_, err := client.Channel.UpdateOneID(ch.ID).SetName("renamed").SetDisabledAPIKeys(disabled).Save(ctx)
	require.NoError(t, err)
	for _, token := range []string{"first-refresh", "second-refresh"} {
		require.NoError(t, callback(ctx, &oauth.OAuthCredentials{AccessToken: token, RefreshToken: "refresh", ExpiresAt: time.Now().Add(time.Hour)}))
	}
	stored, err := client.Channel.Get(ctx, ch.ID)
	require.NoError(t, err)
	require.Equal(t, "renamed", stored.Name)
	require.Equal(t, disabled, stored.DisabledAPIKeys, "refresh cannot reset quota cooldown")
	require.Equal(t, "second-refresh", stored.Credentials.OAuth.AccessToken)
	require.Equal(t, original, ch.Credentials.OAuth.AccessToken, "cached channel snapshot stays immutable")
}

func TestLateFailureCannotDisableUpdatedOAuthChannel(t *testing.T) {
	svc, client := setupTestChannelService(t)
	defer client.Close()
	ctx := authz.WithTestBypass(ent.NewContext(t.Context(), client))
	ch := createTestOAuthChannel(t, client, ctx, "late-failure")
	ch, err := client.Channel.UpdateOneID(ch.ID).SetPolicies(objects.ChannelPolicies{APIKeyAutoDisableRules: []objects.APIKeyAutoDisableRule{{StatusCodes: []int{401}, Times: 1, Action: objects.APIKeyAutoDisableActionPermanent}}}).Save(ctx)
	require.NoError(t, err)
	svc.SetEnabledChannelsForTest([]*Channel{buildChannel(ch, nil)})
	old := &PerformanceRecord{ChannelID: ch.ID, ChannelRevision: ch.UpdatedAt, APIKey: objects.OAuthCredentialRef, ResponseStatusCode: 401}
	updated, err := client.Channel.UpdateOneID(ch.ID).SetCredentials(objects.ChannelCredentials{OAuth: &oauth.OAuthCredentials{AccessToken: "new-login", RefreshToken: "refresh"}}).Save(ctx)
	require.NoError(t, err)
	// Cache deliberately stays stale: the database revision must win.
	matched, acted := svc.checkAndHandleChannelAPIKeyRules(ctx, old)
	require.False(t, matched)
	require.False(t, acted)
	require.Empty(t, svc.apiKeyErrorCounts)
	// Also cover an update after rule evaluation but before the disable write.
	disableCtx := context.WithValue(ctx, autoDisableRevisionKey{}, old.ChannelRevision)
	require.NoError(t, svc.DisableAPIKey(disableCtx, ch.ID, objects.OAuthCredentialRef, 401, "old failure"))
	stored, err := client.Channel.Get(ctx, ch.ID)
	require.NoError(t, err)
	require.Equal(t, channel.StatusEnabled, stored.Status)
	require.Empty(t, stored.DisabledAPIKeys)

	svc.SetEnabledChannelsForTest([]*Channel{buildChannel(updated, nil)})
	current := *old
	current.ChannelRevision = updated.UpdatedAt
	matched, acted = svc.checkAndHandleChannelAPIKeyRules(ctx, &current)
	require.True(t, matched)
	require.True(t, acted)
	stored, err = client.Channel.Get(ctx, ch.ID)
	require.NoError(t, err)
	require.Equal(t, channel.StatusDisabled, stored.Status)
	require.Len(t, stored.DisabledAPIKeys, 1)
}
