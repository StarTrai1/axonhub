package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/ent/system"
	"github.com/looplj/axonhub/internal/server/biz"
)

// This synthetic envelope was independently generated with Python AESGCM.
// It exercises import interoperability, including native Windows file URIs.
const agentRecoveryCLIFixture = `{"version":1,"project_id":1,"api_key_id":1,"records":[{"digest":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc","sealed":"axonhub-agent-v1.AAECAwQFBgcICQoLTcwC7HEh_7Pm3GmzetkhF6_VoY7I1-mm34c_vU00v2XLXaCCmmn46FqdtQU"}]}`

func TestAgentRecoveryCLI(t *testing.T) {
	database := filepath.Join(t.TempDir(), "history with spaces.db")
	client := enttest.NewEntClient(t, "sqlite3", "file:"+filepath.ToSlash(database)+"?_fk=0")
	t.Cleanup(func() { client.Close() })
	ctx := authz.WithTestBypass(ent.NewContext(t.Context(), client))
	service := biz.NewSystemService(biz.SystemServiceParams{Ent: client})
	require.NoError(t, service.SetSecretKey(ctx, "test-agent-message-installation-secret"))
	owner, err := client.APIKey.Create().SetKey("synthetic-import-owner").SetName("test").SetProjectID(1).Save(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, owner.ID)
	bundle := filepath.Join(t.TempDir(), "recovery.json")
	require.NoError(t, os.WriteFile(bundle, []byte(agentRecoveryCLIFixture), 0o600))
	args := []string{"--database", database, "--file", bundle}
	require.NoError(t, runAgentRecovery(args))
	count, err := client.System.Query().Where(system.KeyHasPrefix("agent_message_recovery_v1:")).Count(ctx)
	require.NoError(t, err)
	require.Zero(t, count, "the default command must not import anything")
	require.NoError(t, runAgentRecovery(append(args, "--apply")))
	require.NoError(t, runAgentRecovery(append(args, "--apply")))
	count, err = client.System.Query().Where(system.KeyHasPrefix("agent_message_recovery_v1:")).Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	require.NoError(t, runAgentRecovery(append(args, "--remove")))
	count, err = client.System.Query().Where(system.KeyHasPrefix("agent_message_recovery_v1:")).Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count, "rollback also requires --apply")
	require.NoError(t, runAgentRecovery(append(args, "--remove", "--apply")))
	count, err = client.System.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count, "only the installation secret remains")
	missing := filepath.Join(t.TempDir(), "must-not-create.db")
	require.Error(t, runAgentRecovery([]string{"--database", missing, "--file", bundle}))
	_, err = os.Stat(missing)
	require.True(t, os.IsNotExist(err))
}
