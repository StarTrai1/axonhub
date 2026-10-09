package biz

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/system"
)

// Recovery entries are explicitly imported by the operator after recovering
// the original message. Normal requests only read them; they never ask another
// provider to infer missing text or enable a disabled source channel.
func (s *SystemService) LoadAgentMessageRecovery(ctx context.Context, projectID, apiKeyID int, digest string) (string, error) {
	decoded, err := hex.DecodeString(digest)
	if projectID <= 0 || apiKeyID <= 0 || err != nil || len(decoded) != 32 {
		return "", errors.New("invalid agent message recovery identity")
	}
	key := fmt.Sprintf("agent_message_recovery_v1:%d:%d:%s", projectID, apiKeyID, digest)
	ctx = authz.WithSystemBypass(ctx, "load-agent-message-recovery")
	// Do not cache misses: an authorized recovery import must take effect on
	// the next request, without restarting the service or expiring a cache.
	record, err := s.entFromContext(ctx).System.Query().Where(system.KeyEQ(key)).Only(ctx)
	if ent.IsNotFound(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return record.Value, nil
}

func agentTransportSystemKey(digest string) (string, error) {
	decoded, err := hex.DecodeString(digest)
	if err != nil || len(decoded) != 32 {
		return "", errors.New("invalid agent transport scope")
	}
	return "agent_message_transport_v1:" + digest, nil
}

func (s *SystemService) LoadAgentTransportPolicy(ctx context.Context, digest string) (string, error) {
	key, err := agentTransportSystemKey(digest)
	if err != nil {
		return "", err
	}
	ctx = authz.WithSystemBypass(ctx, "load-agent-transport-policy")
	record, err := s.entFromContext(ctx).System.Query().Where(system.KeyEQ(key)).Only(ctx)
	if ent.IsNotFound(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return record.Value, nil
}

func (s *SystemService) SaveAgentTransportPolicy(ctx context.Context, digest, policy string) error {
	key, err := agentTransportSystemKey(digest)
	if err != nil {
		return err
	}
	return s.setSystemValue(authz.WithSystemBypass(ctx, "save-agent-transport-policy"), key, policy)
}
