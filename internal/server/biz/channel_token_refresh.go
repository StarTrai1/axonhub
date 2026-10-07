package biz

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/llm/oauth"
)

func (svc *ChannelService) refreshOAuthToken(ctx context.Context, ch *ent.Channel, refreshed *oauth.OAuthCredentials) (*ent.Channel, error) {
	if refreshed == nil {
		return ch, nil
	}

	current, err := svc.entFromContext(ctx).Channel.Get(ctx, ch.ID)
	if err != nil {
		return nil, fmt.Errorf("load channel before persisting OAuth refresh: %w", err)
	}
	// A refresh can outlive the provider instance that initiated it. Reject
	// stale credentials or endpoint identities before attempting the CAS.
	if current.Type != ch.Type || current.BaseURL != ch.BaseURL || !sameOAuthRefreshCredentials(current, ch) {
		return nil, errors.New("channel credentials changed while OAuth refresh was in flight")
	}
	updated := current.Credentials

	if ch.Type == channel.TypeAntigravity {
		projectID, err := extractProjectIDFromAntigravityCreds(ch.Credentials.APIKey)
		if err != nil {
			log.Warn(ctx, "failed to extract project ID from antigravity credentials",
				log.Cause(err),
				log.String("channel", ch.Name))
			return nil, fmt.Errorf("failed to extract project ID from antigravity credentials: %w", err)
		}
		updated.APIKey = fmt.Sprintf("%s|%s", refreshed.RefreshToken, projectID)
	} else {
		credJSON, err := refreshed.ToJSON()
		if err != nil {
			return nil, fmt.Errorf("failed to serialize refreshed credentials: %w", err)
		}
		// NOTE：必须是使用 APIKey 字段，不能使用 API Keys 字段
		updated.APIKey = credJSON
	}

	updated.OAuth = refreshed

	saved, err := svc.entFromContext(ctx).Channel.UpdateOneID(ch.ID).
		Where(channel.UpdatedAtEQ(current.UpdatedAt)).
		SetCredentials(updated).Save(ctx)
	if ent.IsNotFound(err) {
		return nil, errors.New("channel changed while persisting OAuth refresh")
	}
	return saved, err
}

//nolint:gosec // G117: serialized credentials are compared only in memory; never logged, persisted or returned.
func sameOAuthRefreshCredentials(current, snapshot *ent.Channel) bool {
	currentJSON, currentErr := json.Marshal(current.Credentials)
	snapshotJSON, snapshotErr := json.Marshal(snapshot.Credentials)
	return currentErr == nil && snapshotErr == nil && bytes.Equal(currentJSON, snapshotJSON)
}
