package orchestrator

import (
	"context"

	"golang.org/x/sync/singleflight"
)

// A canceled waiter must not stay attached to another request's long compaction.
// The generating call still uses its own parent context and only completed
// checkpoints may be persisted after that parent is canceled.
func waitForCompactionFlight(ctx context.Context, group *singleflight.Group, key string, generate func() (any, error)) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result := group.DoChan(key, generate)
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case completed := <-result:
		return completed.Val, completed.Err
	}
}
