package biz

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
)

// Legacy local references did not carry their summaries. Keep their encrypted
// migration checkpoints outside request logs, whose retention is independent of
// a conversation's lifetime. New references carry their own sealed summaries.
func compactionCheckpointSystemKey(projectID, apiKeyID int, cacheKey string) (string, error) {
	digest, err := hex.DecodeString(cacheKey)
	if projectID <= 0 || apiKeyID <= 0 || err != nil || len(digest) != 32 {
		return "", errors.New("invalid local compaction checkpoint identity")
	}
	return fmt.Sprintf("local_compaction_checkpoint_v1:%d:%d:%s", projectID, apiKeyID, cacheKey), nil
}

func (s *SystemService) LoadLocalCompactionCheckpoint(ctx context.Context, projectID, apiKeyID int, cacheKey string) (string, error) {
	key, err := compactionCheckpointSystemKey(projectID, apiKeyID, cacheKey)
	if err != nil {
		return "", err
	}
	ctx = authz.WithSystemBypass(ctx, "load-local-compaction-checkpoint")
	value, err := s.getSystemValue(ctx, key)
	if ent.IsNotFound(err) {
		return "", nil
	}
	return value, err
}

func (s *SystemService) SaveLocalCompactionCheckpoint(ctx context.Context, projectID, apiKeyID int, cacheKey, sealedSummary string) error {
	key, err := compactionCheckpointSystemKey(projectID, apiKeyID, cacheKey)
	if err != nil {
		return err
	}
	if sealedSummary == "" {
		return errors.New("local compaction checkpoint is empty")
	}
	ctx = authz.WithSystemBypass(ctx, "save-local-compaction-checkpoint")
	return s.setSystemValue(ctx, key, sealedSummary)
}
