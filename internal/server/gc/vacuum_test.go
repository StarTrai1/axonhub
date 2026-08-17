package gc

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	_ "github.com/jackc/pgx/v5/stdlib"

	entsql "entgo.io/ent/dialect/sql"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/enttest"
)

func TestWorker_runVacuum_Disabled(t *testing.T) {
	t.Parallel()

	w := &Worker{Config: Config{VacuumEnabled: false}}
	require.NoError(t, w.runVacuum(context.Background()))
}

func TestWorker_runVacuum_SQLite(t *testing.T) {
	t.Parallel()

	// Distinct DSN per subtest so the in-memory DBs don't collide.
	for _, full := range []bool{false, true} {
		name := "vacuum"
		if full {
			name = "vacuum_full_ignored_on_sqlite"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dsn := "file:vac_" + name + "?mode=memory&_fk=1"
			client := enttest.NewEntClient(t, "sqlite3", dsn)
			t.Cleanup(func() { _ = client.Close() })

			w := &Worker{Ent: client, Config: Config{VacuumEnabled: true, VacuumFull: full}}
			require.NoError(t, w.runVacuum(context.Background()))
			require.NoError(t, w.truncateSQLiteWAL(context.Background()))
		})
	}
}

func TestWorker_truncateSQLiteWAL(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "checkpoint.db")
	client := enttest.NewEntClient(t, "sqlite3", "file:"+dbPath+"?_fk=1")
	t.Cleanup(func() { _ = client.Close() })

	sqlDriver, ok := client.Driver().(*entsql.Driver)
	require.True(t, ok)
	_, err := sqlDriver.DB().ExecContext(context.Background(), "PRAGMA journal_mode=WAL")
	require.NoError(t, err)
	_, err = sqlDriver.DB().ExecContext(context.Background(), "PRAGMA wal_autocheckpoint=0")
	require.NoError(t, err)

	ctx := authz.WithTestBypass(ent.NewContext(context.Background(), client))
	_, err = client.Project.Create().SetName("wal-checkpoint").Save(ctx)
	require.NoError(t, err)

	before, err := os.Stat(dbPath + "-wal")
	require.NoError(t, err)
	require.Positive(t, before.Size())

	w := &Worker{Ent: client}
	require.NoError(t, w.truncateSQLiteWAL(context.Background()))

	after, err := os.Stat(dbPath + "-wal")
	require.NoError(t, err)
	require.Zero(t, after.Size())
}

// TestWorker_runVacuum_Postgres exercises the real pgx code path that previously
// failed with "mismatched param and argument count". Gated on AXONHUB_TEST_PG_DSN
// because the project has no in-process Postgres harness.
func TestWorker_runVacuum_Postgres(t *testing.T) {
	dsn := os.Getenv("AXONHUB_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("AXONHUB_TEST_PG_DSN not set; skipping real-Postgres VACUUM check")
	}

	for _, full := range []bool{false, true} {
		name := "vacuum"
		if full {
			name = "vacuum_full"
		}
		t.Run(name, func(t *testing.T) {
			client := newPostgresEntClient(t, dsn)
			t.Cleanup(func() { _ = client.Close() })

			w := &Worker{Ent: client, Config: Config{VacuumEnabled: true, VacuumFull: full}}
			require.NoError(t, w.runVacuum(context.Background()))
		})
	}
}

func newPostgresEntClient(t *testing.T, dsn string) *ent.Client {
	t.Helper()

	sqlDB, err := sql.Open("pgx", dsn)
	require.NoError(t, err)

	require.NoError(t, sqlDB.PingContext(context.Background()))

	return ent.NewClient(ent.Driver(entsql.OpenDB("postgres", sqlDB)))
}
