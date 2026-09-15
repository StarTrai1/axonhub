package biz

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/apikey"
	"github.com/looplj/axonhub/internal/ent/project"
	"github.com/looplj/axonhub/internal/pkg/watcher"
	"github.com/looplj/axonhub/internal/pkg/xcache"
	"github.com/looplj/axonhub/internal/pkg/xcache/live"
)

func TestAPIKeyServiceInvalidatesLocalCacheAfterCommit(t *testing.T) {
	for _, mode := range []string{"immediate", "commit", "rollback"} {
		t.Run(mode, func(t *testing.T) {
			service, client := setupTestAPIKeyService(t, xcache.Config{Mode: xcache.ModeMemory})
			t.Cleanup(service.Stop)
			ctx := authz.WithTestBypass(ent.NewContext(t.Context(), client))
			owner, err := client.User.Create().SetEmail("cache-invalidation@example.com").
				SetPassword("test-only").SetFirstName("Cache").SetLastName("Test").Save(ctx)
			require.NoError(t, err)
			proj, err := client.Project.Create().SetName("Cache invalidation").SetStatus(project.StatusActive).Save(ctx)
			require.NoError(t, err)
			key, err := client.APIKey.Create().SetKey("ah-invalidation-test-only").SetName("Cache invalidation").
				SetUser(owner).SetProject(proj).SetType(apikey.TypeServiceAccount).SetStatus(apikey.StatusEnabled).Save(ctx)
			require.NoError(t, err)
			auth := &AuthService{APIKeyService: service}
			original, err := auth.AuthenticateAPIKey(ctx, key.Key)
			require.NoError(t, err)
			cacheKey := buildAPIKeyCacheKey(key.Key)

			// No subscribers receive these events: local correctness must not
			// depend on the watcher goroutine running before the next request.
			service.apiKeyNotifier = watcher.NewMemoryWatcher[live.CacheEvent[string]](watcher.MemoryWatcherOptions{})
			updateCtx := ctx
			var tx *ent.Tx
			if mode != "immediate" {
				tx, err = client.Tx(ctx)
				require.NoError(t, err)
				t.Cleanup(func() { _ = tx.Rollback() })
				updateCtx = ent.NewContext(ent.NewTxContext(ctx, tx), tx.Client())
			}
			_, err = service.UpdateAPIKeyStatus(updateCtx, key.ID, apikey.StatusDisabled)
			require.NoError(t, err)
			if tx != nil {
				cached, ok := service.APIKeyCache.GetCached(cacheKey)
				require.True(t, ok, "an uncommitted mutation must not invalidate the shared cache")
				require.Equal(t, apikey.StatusEnabled, cached.Status)
				if mode == "rollback" {
					require.NoError(t, tx.Rollback())
				} else {
					require.NoError(t, tx.Commit())
				}
			}

			_, cached := service.APIKeyCache.GetCached(cacheKey)
			require.Equal(t, mode == "rollback", cached)
			_, err = auth.AuthenticateAPIKey(ctx, key.Key)
			if mode == "rollback" {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, ErrInvalidAPIKey)
			}
			require.Equal(t, apikey.StatusEnabled, original.Status, "active requests retain their own authorization snapshot")
		})
	}
}
