package biz

import (
	"context"
	"crypto/sha256"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/ent/hook"
	"github.com/looplj/axonhub/internal/ent/system"
)

func TestResponsesSessionBindingSurvivesRestartAndLateRejections(t *testing.T) {
	dsn := "file:" + filepath.ToSlash(filepath.Join(t.TempDir(), "bindings.db")) + "?_fk=1"
	client := enttest.NewEntClient(t, "sqlite3", dsn)
	service := NewSystemService(SystemServiceParams{Ent: client})
	scope := sha256.Sum256([]byte("owner-channel-credential-model-thread"))
	alias, err := service.LoadResponsesSessionBinding(t.Context(), scope)
	require.NoError(t, err)
	require.Empty(t, alias)
	first, err := service.RenewResponsesSessionBinding(t.Context(), scope, "")
	require.NoError(t, err)
	require.NotEmpty(t, first)
	late, err := service.RenewResponsesSessionBinding(t.Context(), scope, "")
	require.NoError(t, err)
	require.Equal(t, first, late)
	require.NoError(t, client.Close())

	client = enttest.NewEntClient(t, "sqlite3", dsn)
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	service = NewSystemService(SystemServiceParams{Ent: client})
	restored, err := service.LoadResponsesSessionBinding(t.Context(), scope)
	require.NoError(t, err)
	require.Equal(t, first, restored)
	second, err := service.RenewResponsesSessionBinding(t.Context(), scope, first)
	require.NoError(t, err)
	require.NotEqual(t, first, second)
	for _, stale := range []string{"", first} {
		got, err := service.RenewResponsesSessionBinding(t.Context(), scope, stale)
		require.NoError(t, err)
		require.Equal(t, second, got)
	}
	other := sha256.Sum256([]byte("another-owner"))
	missing, err := service.LoadResponsesSessionBinding(t.Context(), other)
	require.NoError(t, err)
	require.Empty(t, missing)
	_, err = service.RenewResponsesSessionBinding(t.Context(), other, second)
	require.Error(t, err)
	ctx := authz.WithTestBypass(t.Context())
	record, err := client.System.Query().Only(ctx)
	require.NoError(t, err)
	require.NotContains(t, record.Key+record.Value, "owner-channel-credential-model-thread")
	require.NoError(t, client.System.Update().Where(system.IDEQ(record.ID)).SetValue(`{"version":2,"session":"bad"}`).Exec(ctx))
	_, err = service.LoadResponsesSessionBinding(t.Context(), scope)
	require.Error(t, err)
	_, err = service.RenewResponsesSessionBinding(t.Context(), scope, second)
	require.Error(t, err)
}

func TestResponsesSessionBindingConcurrentRejectionsConverge(t *testing.T) {
	for _, operation := range []ent.Op{ent.OpCreate, ent.OpUpdate} {
		t.Run(operation.String(), func(t *testing.T) {
			service, client := newResponsesMetadataTestService(t)
			other := NewSystemService(SystemServiceParams{Ent: client})
			scope := sha256.Sum256([]byte(t.Name()))
			expected := ""
			if operation == ent.OpUpdate {
				var err error
				expected, err = service.RenewResponsesSessionBinding(t.Context(), scope, "")
				require.NoError(t, err)
			}
			var injected atomic.Bool
			var winner string
			client.System.Use(func(next ent.Mutator) ent.Mutator {
				return hook.SystemFunc(func(ctx context.Context, mutation *ent.SystemMutation) (ent.Value, error) {
					if mutation.Op() == operation && injected.CompareAndSwap(false, true) {
						var err error
						winner, err = other.RenewResponsesSessionBinding(ctx, scope, expected)
						if err != nil {
							return nil, err
						}
					}
					return next.Mutate(ctx, mutation)
				})
			})
			alias, err := service.RenewResponsesSessionBinding(t.Context(), scope, expected)
			require.NoError(t, err)
			require.True(t, injected.Load())
			require.NotEmpty(t, winner)
			require.Equal(t, winner, alias)
			restored, err := other.LoadResponsesSessionBinding(t.Context(), scope)
			require.NoError(t, err)
			require.Equal(t, winner, restored)
		})
	}
}
