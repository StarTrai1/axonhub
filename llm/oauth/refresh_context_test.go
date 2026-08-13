package oauth

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sync/singleflight"
)

func TestDoSharedRefreshCallerCancellationDoesNotCancelRefresh(t *testing.T) {
	t.Parallel()

	var group singleflight.Group
	started := make(chan struct{})
	release := make(chan struct{})
	completed := make(chan struct{})
	refreshContext := make(chan context.Context, 1)

	callerCtx, cancel := context.WithCancel(context.Background())
	firstResult := make(chan error, 1)
	go func() {
		_, err := doSharedRefresh(callerCtx, &group, "refresh", func(ctx context.Context) (any, error) {
			defer close(completed)
			refreshContext <- ctx
			close(started)
			<-release
			return "fresh-token", nil
		})
		firstResult <- err
	}()

	<-started
	cancel()
	require.ErrorIs(t, <-firstResult, context.Canceled)

	detachedCtx := <-refreshContext
	require.NoError(t, detachedCtx.Err())
	deadline, ok := detachedCtx.Deadline()
	require.True(t, ok)
	require.WithinDuration(t, time.Now().Add(oauthRefreshTimeout), deadline, time.Second)

	close(release)
	<-completed
}
