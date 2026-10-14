package biz

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/llm/oauth"
)

func (svc *ChannelService) refreshOAuthToken(ctx context.Context, ch *ent.Channel, refreshed *oauth.OAuthCredentials) (*ent.Channel, error) {
	snapshot := *ch
	err := svc.refreshOAuthTokenWithClient(ctx, svc.entFromContext(ctx), &snapshot, refreshed)
	return &snapshot, err
}

func (svc *ChannelService) refreshOAuthTokenWithClient(ctx context.Context, db *ent.Client, ch *ent.Channel, refreshed *oauth.OAuthCredentials) error {
	if refreshed == nil {
		return nil
	}

	updated := ch.Credentials

	if ch.Type == channel.TypeAntigravity {
		projectID, err := extractProjectIDFromAntigravityCreds(ch.Credentials.APIKey)
		if err != nil {
			log.Warn(ctx, "failed to extract project ID from antigravity credentials",
				log.Cause(err),
				log.String("channel", ch.Name))
			return fmt.Errorf("failed to extract project ID from antigravity credentials: %w", err)
		}
		updated.APIKey = fmt.Sprintf("%s|%s", refreshed.RefreshToken, projectID)
	} else {
		credJSON, err := refreshed.ToJSON()
		if err != nil {
			return fmt.Errorf("failed to serialize refreshed credentials: %w", err)
		}
		// NOTE：必须是使用 APIKey 字段，不能使用 API Keys 字段
		updated.APIKey = credJSON
	}

	updated.OAuth = refreshed

	for {
		saved, err := db.Channel.UpdateOneID(ch.ID).Where(channel.UpdatedAt(ch.UpdatedAt)).SetCredentials(updated).Save(ctx)
		if ent.IsNotFound(err) {
			current, reloadErr := db.Channel.Get(ctx, ch.ID)
			if reloadErr != nil {
				return fmt.Errorf("failed to reconcile channel credentials after concurrent update: %w", reloadErr)
			}
			sameEndpoint := current.Type == ch.Type && current.BaseURL == ch.BaseURL
			unchanged := sameEndpoint && channelCredentialsEqual(current.Credentials, ch.Credentials)
			ch.Credentials = current.Credentials
			ch.UpdatedAt = current.UpdatedAt
			if unchanged {
				// A metadata-only update must not discard a rotated refresh token.
				continue
			}

			var winner *oauth.OAuthCredentials
			if sameEndpoint && current.Credentials.IsOAuth() {
				winner, reloadErr = current.Credentials.ResolveOAuthCredentials()
				if reloadErr != nil {
					return &oauth.CredentialConflictError{}
				}
			}
			return &oauth.CredentialConflictError{Credentials: winner}
		}
		if err != nil {
			return err
		}

		ch.Credentials = saved.Credentials
		ch.UpdatedAt = saved.UpdatedAt

		return nil
	}
}

func channelCredentialsEqual(a, b objects.ChannelCredentials) bool {
	return a.APIKey == b.APIKey &&
		oauthCredentialsEqual(a.OAuth, b.OAuth) &&
		slices.Equal(a.APIKeys, b.APIKeys) &&
		a.ManagementAPIKey == b.ManagementAPIKey &&
		azureCredentialsEqual(a.Azure, b.Azure) &&
		gcpCredentialsEqual(a.GCP, b.GCP)
}

func oauthCredentialsEqual(a, b *objects.OAuthCredentials) bool {
	if a == nil || b == nil {
		return a == b
	}

	return a.ClientID == b.ClientID &&
		a.AccessToken == b.AccessToken &&
		a.RefreshToken == b.RefreshToken &&
		a.IDToken == b.IDToken &&
		a.ExpiresAt.Equal(b.ExpiresAt) &&
		a.TokenType == b.TokenType &&
		slices.Equal(a.Scopes, b.Scopes)
}

func azureCredentialsEqual(a, b *objects.AzureCredential) bool {
	if a == nil || b == nil {
		return a == b
	}

	return a.APIVersion == b.APIVersion
}

func gcpCredentialsEqual(a, b *objects.GCPCredential) bool {
	if a == nil || b == nil {
		return a == b
	}

	return a.Region == b.Region && a.ProjectID == b.ProjectID && a.JSONData == b.JSONData
}

// GetModelEntries returns all models this channel can handle, RequestModel -> Entry
// This unifies:
// - SupportedModels (direct models)
// - ExtraModelPrefix (prefixed models)
// - AutoTrimedModelPrefixes (auto-trimmed models)
// - ModelMappings (mapped models)
// The result is cached for performance.
//
// WARNING: The returned map is internal cached state.
// DO NOT modify the returned map or its ChannelModelEntry values.
// Modifications will not persist and may cause data inconsistency.
//
//nolint:gosec // G117: serialized credentials are compared only in memory; never logged, persisted or returned.
func sameOAuthRefreshCredentials(current, snapshot *ent.Channel) bool {
	currentJSON, currentErr := json.Marshal(current.Credentials)
	snapshotJSON, snapshotErr := json.Marshal(snapshot.Credentials)
	return currentErr == nil && snapshotErr == nil && bytes.Equal(currentJSON, snapshotJSON)
}
