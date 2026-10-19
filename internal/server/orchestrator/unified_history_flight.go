package orchestrator

import (
	"context"
	"errors"

	"golang.org/x/sync/singleflight"

	"github.com/looplj/axonhub/internal/log"
)

// A canceled follower can leave immediately. The owner also drains its own
// canceled generator so storage/accounting cannot outlive the recovery scope.
func waitForUnifiedHistoryFlight(ctx context.Context, group *singleflight.Group, key string, generate func() (any, error)) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	started := make(chan struct{})
	done := make(chan struct{})
	result := group.DoChan(key, func() (value any, err error) {
		close(started)
		defer close(done)
		defer func() {
			if recovered := recover(); recovered != nil {
				log.Error(ctx, "unified history generation panicked", log.Any("panic", recovered))
				err = errors.New("unified history generation failed")
			}
		}()
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return generate()
	})
	select {
	case completed := <-result:
		return completed.Val, completed.Err
	case <-ctx.Done():
		select {
		case <-started:
			<-done
		default:
			// Either another caller owns the flight, or our callback has not
			// started and will see cancellation before touching any resources.
		}
		return nil, ctx.Err()
	}
}
