package biz

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/objects"
)

func TestChannelReloadRetainsUnchangedPools(t *testing.T) {
	svc, client := setupTestChannelService(t)
	defer client.Close()
	ctx := authz.WithTestBypass(ent.NewContext(context.Background(), client))
	first := createTestChannelWithAPIKeys(t, client, ctx, "first", []string{"synthetic-first"})
	second := createTestChannelWithAPIKeys(t, client, ctx, "second", []string{"synthetic-second"})
	current, _, changed, err := svc.reloadEnabledChannels(ctx, nil, time.Time{})
	require.NoError(t, err)
	require.True(t, changed)
	find := func(channels []*Channel, id int) *Channel {
		for _, ch := range channels {
			if ch.ID == id {
				return ch
			}
		}
		return nil
	}
	oldFirst, oldSecond := find(current, first.ID), find(current, second.ID)
	require.NotNil(t, oldFirst)
	require.NotNil(t, oldSecond)
	defer stopChannelOutbounds(oldFirst)
	defer stopChannelOutbounds(oldSecond)
	_, err = client.Channel.UpdateOneID(second.ID).SetCredentials(objects.ChannelCredentials{APIKey: "new-credential"}).Save(ctx)
	require.NoError(t, err)
	next, _, _, err := svc.reloadEnabledChannels(ctx, current, time.Time{})
	require.NoError(t, err)
	require.Same(t, oldFirst, find(next, first.ID), "sibling transport pools must survive refresh")
	require.NotSame(t, oldSecond, find(next, second.ID), "changed credentials must rebuild")
	require.Equal(t, "new-credential", find(next, second.ID).Credentials.APIKey)
	defer stopChannelOutbounds(find(next, second.ID))
	_, err = client.Channel.UpdateOneID(second.ID).SetStatus(channel.StatusDisabled).Save(ctx)
	require.NoError(t, err)
	remaining, _, _, err := svc.reloadEnabledChannels(ctx, next, time.Time{})
	require.NoError(t, err)
	require.Equal(t, []*Channel{oldFirst}, remaining)
}

func TestChannelReloadDoesNotReuseChangedIdentityAtSameTimestamp(t *testing.T) {
	entity := &ent.Channel{ID: 1, UpdatedAt: time.Now(), Credentials: objects.ChannelCredentials{APIKey: "first"}}
	old := &Channel{Channel: entity}
	next := *entity
	require.True(t, canReuseChannel(old, &next))
	next.Credentials.APIKey = "replacement"
	require.False(t, canReuseChannel(old, &next))
	next = *entity
	next.Settings = &objects.ChannelSettings{HTTPProtocol: "http1"}
	require.False(t, canReuseChannel(old, &next))
}

func TestChannelSwapCleansOnlyRetiredPools(t *testing.T) {
	var starts, stops atomic.Int32
	retained := &Channel{startTokenProvider: func() { starts.Add(1) }, stopTokenProvider: func() { stops.Add(1) }}
	removedDone := make(chan struct{})
	removed := &Channel{stopTokenProvider: func() { close(removedDone) }}
	added := &Channel{startTokenProvider: func() { starts.Add(1) }}
	svc := &ChannelService{}
	svc.onEnabledChannelsSwap([]*Channel{retained, removed}, []*Channel{retained, added})
	select {
	case <-removedDone:
	case <-time.After(5 * time.Second):
		t.Fatal("retired channel was not cleaned")
	}
	require.EqualValues(t, 1, starts.Load(), "retained token provider must not restart")
	require.Zero(t, stops.Load(), "retained token provider must remain active")
	require.EqualValues(t, 1, svc.GetCacheVersion())
}
