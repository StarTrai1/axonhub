package orchestrator

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/schema/schematype"
	"github.com/looplj/axonhub/internal/ent/system"
	"github.com/looplj/axonhub/internal/server/biz"
)

// AgentMessageRecoveryBundle contains installation-encrypted operator recoveries,
// never plaintext or credentials. Digest binds ID, author, recipient and original
// ciphertext. The original request and conversation records are not modified.
type AgentMessageRecoveryBundle struct {
	Version   int                          `json:"version"`
	ProjectID int                          `json:"project_id"`
	APIKeyID  int                          `json:"api_key_id"`
	Records   []AgentMessageRecoveryRecord `json:"records"`
}

type AgentMessageRecoveryRecord struct {
	Digest string `json:"digest"`
	Sealed string `json:"sealed"`
}

// ImportAgentMessageRecoveries is a maintenance operation. It only validates
// when apply is false. Applying or removing exact entries is atomic and refuses
// conflicting records; remove is an explicit rollback using the same bundle.
func ImportAgentMessageRecoveries(ctx context.Context, client *ent.Client, bundle AgentMessageRecoveryBundle, apply, remove bool) (int, error) {
	if bundle.Version != 1 || bundle.ProjectID <= 0 || bundle.APIKeyID <= 0 || len(bundle.Records) == 0 || len(bundle.Records) > 32 {
		return 0, errors.New("invalid agent message recovery bundle")
	}
	ctx = authz.WithSystemBypass(ent.NewContext(ctx, client), "operator-agent-message-recovery-import")
	service := biz.NewSystemService(biz.SystemServiceParams{Ent: client})
	secret, err := service.SecretKey(ctx)
	if err != nil {
		return 0, err
	}
	aead, err := newResponsesAgentMessageCipher(secret)
	if err != nil {
		return 0, err
	}
	state := &PersistenceState{APIKey: &ent.APIKey{ID: bundle.APIKeyID, ProjectID: bundle.ProjectID}}
	keys := make(map[string]string, len(bundle.Records))
	for _, record := range bundle.Records {
		digest, err := hex.DecodeString(record.Digest)
		if err != nil || len(digest) != 32 || hex.EncodeToString(digest) != record.Digest {
			return 0, errors.New("invalid recovery digest")
		}
		aad, err := responsesAgentMessageAAD(state, record.Digest)
		if err != nil {
			return 0, err
		}
		if _, err := openResponsesAgentMessage(aead, aad, record.Sealed); err != nil {
			return 0, err
		}
		key := fmt.Sprintf("agent_message_recovery_v1:%d:%d:%s", bundle.ProjectID, bundle.APIKeyID, record.Digest)
		if _, exists := keys[key]; exists {
			return 0, errors.New("duplicate recovery entry")
		}
		keys[key] = record.Sealed
	}
	changed := 0
	err = service.RunInTransaction(ctx, func(txCtx context.Context) error {
		txClient := ent.FromContext(txCtx)
		owner, err := txClient.APIKey.Get(txCtx, bundle.APIKeyID)
		if err != nil || owner.ProjectID != bundle.ProjectID {
			return errors.New("recovery API key does not belong to the specified project")
		}
		// Include deleted entries so a unique-key collision cannot silently
		// replace an older operator decision or reactivate a removed record.
		txCtx = schematype.SkipSoftDelete(txCtx)
		for key, sealed := range keys {
			current, err := txClient.System.Query().Where(system.KeyEQ(key)).Only(txCtx)
			if err != nil && !ent.IsNotFound(err) {
				return err
			}
			if current != nil && (current.Value != sealed || current.DeletedAt != 0) {
				return errors.New("recovery import conflicts with an existing record")
			}
			if remove && current != nil {
				changed++
				if apply {
					if err := txClient.System.DeleteOne(current).Exec(txCtx); err != nil {
						return err
					}
				}
			} else if !remove && current == nil {
				changed++
				if apply {
					if _, err := txClient.System.Create().SetKey(key).SetValue(sealed).Save(txCtx); err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return changed, nil
}
