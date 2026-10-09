package orchestrator

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/ent/system"
	"github.com/looplj/axonhub/internal/server/biz"
)

func TestResponsesAgentRecoveryImportIsAtomic(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:agent-import-atomic?mode=memory&_fk=0")
	t.Cleanup(func() { client.Close() })
	ctx := authz.WithTestBypass(ent.NewContext(t.Context(), client))
	service := biz.NewSystemService(biz.SystemServiceParams{Ent: client})
	require.NoError(t, service.SetSecretKey(ctx, "test-agent-message-installation-secret"))
	_, err := client.APIKey.Create().SetKey("synthetic-owner").SetName("test").SetProjectID(1).Save(ctx)
	require.NoError(t, err)
	aead, err := newResponsesAgentMessageCipher("test-agent-message-installation-secret")
	require.NoError(t, err)
	bundle := AgentMessageRecoveryBundle{Version: 1, ProjectID: 1, APIKeyID: 1}
	for _, digit := range []string{"a", "b"} {
		digest := strings.Repeat(digit, 64)
		aad, err := responsesAgentMessageAAD(agentMessageTestState(), digest)
		require.NoError(t, err)
		sealed, err := sealResponsesAgentMessage(aead, aad, "synthetic original "+digit)
		require.NoError(t, err)
		bundle.Records = append(bundle.Records, AgentMessageRecoveryRecord{Digest: digest, Sealed: sealed})
	}
	conflict := fmt.Sprintf("agent_message_recovery_v1:1:1:%s", bundle.Records[1].Digest)
	_, err = client.System.Create().SetKey(conflict).SetValue("another operator decision").Save(ctx)
	require.NoError(t, err)
	_, err = ImportAgentMessageRecoveries(ctx, client, bundle, true, false)
	require.ErrorContains(t, err, "conflicts")
	count, err := client.System.Query().Where(system.KeyHasPrefix("agent_message_recovery_v1:")).Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count, "no first entry may survive a conflicting second entry")
	before, err := client.System.Query().Where(system.KeyEQ(conflict)).Only(ctx)
	require.NoError(t, err)
	require.Equal(t, "another operator decision", before.Value)
	_, err = ImportAgentMessageRecoveries(ctx, client, bundle, true, true)
	require.ErrorContains(t, err, "conflicts", "rollback must not remove another operator's data")
	duplicate := bundle
	duplicate.Records = []AgentMessageRecoveryRecord{bundle.Records[0], bundle.Records[0]}
	_, err = ImportAgentMessageRecoveries(ctx, client, duplicate, true, false)
	require.ErrorContains(t, err, "duplicate")
	wrongOwner := bundle
	wrongOwner.APIKeyID = 2
	_, err = ImportAgentMessageRecoveries(ctx, client, wrongOwner, true, false)
	require.Error(t, err)
}
