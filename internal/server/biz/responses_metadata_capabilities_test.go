package biz

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/ent/hook"
	"github.com/looplj/axonhub/internal/ent/system"
)

func newResponsesMetadataTestService(test *testing.T) (*SystemService, *ent.Client) {
	test.Helper()
	client := enttest.NewEntClient(test, "sqlite3", "file:responses_metadata?mode=memory&_fk=1")
	test.Cleanup(func() { require.NoError(test, client.Close()) })
	return NewSystemService(SystemServiceParams{Ent: client}), client
}

func TestSystemServiceResponsesMetadataPersistsAcrossInstances(test *testing.T) {
	service, client := newResponsesMetadataTestService(test)
	scope := sha256.Sum256([]byte("private-channel-endpoint-model-credential"))
	expiresAt := time.Now().Add(time.Hour).UTC()
	require.NoError(test, service.SaveResponsesMetadataRejection(test.Context(), scope, expiresAt))

	restarted := NewSystemService(SystemServiceParams{Ent: client})
	restored, err := restarted.LoadResponsesMetadataRejection(test.Context(), scope)
	require.NoError(test, err)
	require.True(test, expiresAt.Equal(restored))
	missing, err := restarted.LoadResponsesMetadataRejection(test.Context(), sha256.Sum256([]byte("different-scope")))
	require.NoError(test, err)
	require.True(test, missing.IsZero())

	ctx := authz.WithTestBypass(test.Context())
	record, err := client.System.Query().Only(ctx)
	require.NoError(test, err)
	require.Equal(test, responsesMetadataCapabilitiesKey, record.Key)
	require.Contains(test, record.Value, hex.EncodeToString(scope[:]))
	require.NotContains(test, record.Value, "private-channel-endpoint-model-credential")
	require.NoError(test, service.SaveResponsesMetadataRejection(test.Context(), scope, expiresAt.Add(-time.Minute)))
	restored, err = restarted.LoadResponsesMetadataRejection(test.Context(), scope)
	require.NoError(test, err)
	require.True(test, expiresAt.Equal(restored))
}

func TestSystemServiceResponsesMetadataSurvivesDatabaseReopen(test *testing.T) {
	databaseURL := "file:" + filepath.ToSlash(filepath.Join(test.TempDir(), "capabilities.db")) + "?_fk=1"
	firstClient := enttest.NewEntClient(test, "sqlite3", databaseURL)
	test.Cleanup(func() { require.NoError(test, firstClient.Close()) })
	first := NewSystemService(SystemServiceParams{Ent: firstClient})
	scope := sha256.Sum256([]byte("durable-scope"))
	expiresAt := time.Now().Add(time.Hour).UTC()
	require.NoError(test, first.SaveResponsesMetadataRejection(test.Context(), scope, expiresAt))
	require.NoError(test, firstClient.Close())

	reopenedClient := enttest.NewEntClient(test, "sqlite3", databaseURL)
	test.Cleanup(func() { require.NoError(test, reopenedClient.Close()) })
	restarted := NewSystemService(SystemServiceParams{Ent: reopenedClient})
	restored, err := restarted.LoadResponsesMetadataRejection(test.Context(), scope)
	require.NoError(test, err)
	require.True(test, expiresAt.Equal(restored))
}

func TestSystemServiceResponsesMetadataRejectsStaleAndMalformedState(test *testing.T) {
	for _, scenario := range []struct {
		name      string
		value     func(string) string
		wantError bool
	}{
		{name: "expired", value: func(scope string) string {
			return fmt.Sprintf(`{"version":1,"rejections":{%q:%q}}`, scope, time.Now().Add(-time.Minute).Format(time.RFC3339Nano))
		}},
		{name: "excessive_expiry", value: func(scope string) string {
			return fmt.Sprintf(`{"version":1,"rejections":{%q:%q}}`, scope, time.Now().Add(24*time.Hour).Format(time.RFC3339Nano))
		}},
		{name: "wrong_version", value: func(string) string { return `{"version":2,"rejections":{}}` }, wantError: true},
		{name: "missing_map", value: func(string) string { return `{"version":1}` }, wantError: true},
		{name: "invalid_json", value: func(string) string { return `{` }, wantError: true},
		{name: "oversized", value: func(string) string { return strings.Repeat(" ", responsesMetadataCapabilitiesMaxBytes+1) }, wantError: true},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			service, client := newResponsesMetadataTestService(test)
			scope := sha256.Sum256([]byte(scenario.name))
			ctx := authz.WithTestBypass(test.Context())
			require.NoError(test, client.System.Create().SetKey(responsesMetadataCapabilitiesKey).
				SetValue(scenario.value(hex.EncodeToString(scope[:]))).Exec(ctx))
			expiresAt, err := service.LoadResponsesMetadataRejection(test.Context(), scope)
			if scenario.wantError {
				require.Error(test, err)
			} else {
				require.NoError(test, err)
			}
			require.True(test, expiresAt.IsZero())

			freshExpiry := time.Now().Add(time.Hour).UTC()
			require.NoError(test, service.SaveResponsesMetadataRejection(test.Context(), scope, freshExpiry))
			expiresAt, err = service.LoadResponsesMetadataRejection(test.Context(), scope)
			require.NoError(test, err)
			require.True(test, freshExpiry.Equal(expiresAt))
		})
	}
}

func TestSystemServiceResponsesMetadataBoundsPersistedEntries(test *testing.T) {
	service, client := newResponsesMetadataTestService(test)
	ctx := authz.WithTestBypass(test.Context())
	now := time.Now().UTC()
	capabilities := responsesMetadataCapabilities{Version: 1, Rejections: make(map[string]time.Time)}
	var oldestScope [sha256.Size]byte
	for index := range responsesMetadataCapabilitiesLimit {
		scope := sha256.Sum256(fmt.Appendf(nil, "old-scope-%d", index))
		if index == 0 {
			oldestScope = scope
		}
		capabilities.Rejections[hex.EncodeToString(scope[:])] = now.Add(time.Minute + time.Duration(index)*time.Second)
	}
	encoded, err := json.Marshal(capabilities)
	require.NoError(test, err)
	require.NoError(test, client.System.Create().SetKey(responsesMetadataCapabilitiesKey).SetValue(string(encoded)).Exec(ctx))
	require.NoError(test, client.System.Create().SetKey("unrelated-setting").SetValue("preserve").Exec(ctx))

	scope := sha256.Sum256([]byte("new-scope"))
	expiresAt := now.Add(2 * time.Hour)
	require.NoError(test, service.SaveResponsesMetadataRejection(test.Context(), scope, expiresAt))
	record, err := client.System.Query().Where(system.KeyEQ(responsesMetadataCapabilitiesKey)).Only(ctx)
	require.NoError(test, err)
	var persisted responsesMetadataCapabilities
	require.NoError(test, json.Unmarshal([]byte(record.Value), &persisted))
	require.Len(test, persisted.Rejections, responsesMetadataCapabilitiesLimit)
	require.True(test, expiresAt.Equal(persisted.Rejections[hex.EncodeToString(scope[:])]))
	_, retained := persisted.Rejections[hex.EncodeToString(oldestScope[:])]
	require.False(test, retained)
	unrelated, err := client.System.Query().Where(system.KeyEQ("unrelated-setting")).Only(ctx)
	require.NoError(test, err)
	require.Equal(test, "preserve", unrelated.Value)
}

func TestSystemServiceResponsesMetadataPreservesConcurrentLearning(test *testing.T) {
	for _, scenario := range []struct {
		name string
		op   ent.Op
	}{
		{name: "create", op: ent.OpCreate},
		{name: "update", op: ent.OpUpdate},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			service, client := newResponsesMetadataTestService(test)
			other := NewSystemService(SystemServiceParams{Ent: client})
			expiresAt := time.Now().Add(time.Hour).UTC()
			firstScope := sha256.Sum256([]byte("first"))
			secondScope := sha256.Sum256([]byte("second"))
			seedScope := sha256.Sum256([]byte("seed"))
			if scenario.op == ent.OpUpdate {
				require.NoError(test, service.SaveResponsesMetadataRejection(test.Context(), seedScope, expiresAt))
			}
			var injected atomic.Bool
			client.System.Use(func(next ent.Mutator) ent.Mutator {
				return hook.SystemFunc(func(ctx context.Context, mutation *ent.SystemMutation) (ent.Value, error) {
					if mutation.Op() == scenario.op && injected.CompareAndSwap(false, true) {
						if err := other.SaveResponsesMetadataRejection(ctx, secondScope, expiresAt); err != nil {
							return nil, err
						}
					}
					return next.Mutate(ctx, mutation)
				})
			})
			require.NoError(test, service.SaveResponsesMetadataRejection(test.Context(), firstScope, expiresAt))
			require.True(test, injected.Load())
			for _, scope := range [][sha256.Size]byte{firstScope, secondScope} {
				restored, err := other.LoadResponsesMetadataRejection(test.Context(), scope)
				require.NoError(test, err)
				require.True(test, expiresAt.Equal(restored))
			}
			if scenario.op == ent.OpUpdate {
				restored, err := other.LoadResponsesMetadataRejection(test.Context(), seedScope)
				require.NoError(test, err)
				require.True(test, expiresAt.Equal(restored))
			}
		})
	}
}

func TestSystemServiceResponsesMetadataRelearnsAfterSoftDelete(test *testing.T) {
	service, client := newResponsesMetadataTestService(test)
	ctx := authz.WithTestBypass(test.Context())
	oldScope := sha256.Sum256([]byte("forgotten"))
	newScope := sha256.Sum256([]byte("relearned"))
	expiresAt := time.Now().Add(time.Hour).UTC()
	require.NoError(test, service.SaveResponsesMetadataRejection(test.Context(), oldScope, expiresAt))
	_, err := client.System.Delete().Where(system.KeyEQ(responsesMetadataCapabilitiesKey)).Exec(ctx)
	require.NoError(test, err)
	restored, err := service.LoadResponsesMetadataRejection(test.Context(), oldScope)
	require.NoError(test, err)
	require.True(test, restored.IsZero())
	require.NoError(test, service.SaveResponsesMetadataRejection(test.Context(), newScope, expiresAt))
	restored, err = service.LoadResponsesMetadataRejection(test.Context(), newScope)
	require.NoError(test, err)
	require.True(test, expiresAt.Equal(restored))
	restored, err = service.LoadResponsesMetadataRejection(test.Context(), oldScope)
	require.NoError(test, err)
	require.True(test, restored.IsZero())
}

func TestSystemServiceResponsesMetadataHonorsCancellationAndExpiry(test *testing.T) {
	service, _ := newResponsesMetadataTestService(test)
	scope := sha256.Sum256([]byte("scope"))
	ctx, cancel := context.WithCancel(test.Context())
	cancel()
	_, err := service.LoadResponsesMetadataRejection(ctx, scope)
	require.ErrorIs(test, err, context.Canceled)
	require.ErrorIs(test, service.SaveResponsesMetadataRejection(ctx, scope, time.Now().Add(time.Hour)), context.Canceled)
	require.Error(test, service.SaveResponsesMetadataRejection(test.Context(), scope, time.Now().Add(-time.Second)))
	require.Error(test, service.SaveResponsesMetadataRejection(test.Context(), scope, time.Now().Add(24*time.Hour)))
}
