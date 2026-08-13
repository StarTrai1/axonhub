package oauth

import (
	"context"
	"time"

	"golang.org/x/sync/singleflight"
)

const oauthRefreshTimeout = 30 * time.Second

// doSharedRefresh keeps the shared refresh independent from any one caller while
// still allowing each caller to stop waiting as soon as its own context ends.
func doSharedRefresh(
	ctx context.Context,
	group *singleflight.Group,
	key string,
	refresh func(context.Context) (any, error),
) (any, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	resultCh := group.DoChan(key, func() (any, error) {
		refreshCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), oauthRefreshTimeout)
		defer cancel()

		return refresh(refreshCtx)
	})

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-resultCh:
		return result.Val, result.Err
	}
}
