package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sync/singleflight"
)

func TestCompactionFlightCanceledWaiterReturnsWithoutDuplicateGeneration(t *testing.T) {
	var group singleflight.Group
	started := make(chan struct{})
	release := make(chan struct{})
	first := group.DoChan("same-checkpoint", func() (any, error) {
		close(started)
		<-release
		return "completed checkpoint", nil
	})
	<-started
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	value, err := waitForCompactionFlight(ctx, &group, "same-checkpoint", func() (any, error) {
		return "must not generate a duplicate", nil
	})
	close(release)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Nil(t, value)
	completed := <-first
	require.Equal(t, "completed checkpoint", completed.Val)
	require.NoError(t, completed.Err)
}
