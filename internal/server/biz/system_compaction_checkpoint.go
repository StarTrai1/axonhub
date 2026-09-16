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

// Keep encrypted summaries outside request logs, whose retention is independent
// of a conversation's lifetime. Native references use these after bridging;
// new locally generated references carry their own sealed summaries.
func compactionCheckpointSystemKey(projectID, apiKeyID int, cacheKey string) (string, error) {
	digest, err := hex.DecodeString(cacheKey)
	if projectID <= 0 || apiKeyID <= 0 || err != nil || len(digest) != 32 {
		return "", errors.New("invalid local compaction checkpoint identity")
	}
	return fmt.Sprintf("local_compaction_checkpoint_v1:%d:%d:%s", projectID, apiKeyID, cacheKey), nil
}

// Native source checkpoints contain encrypted, compressed context, separate
// from the request/response logs and from local summary checkpoints.
func (s *SystemService) LoadNativeCompactionSource(ctx context.Context, projectID, apiKeyID int, cacheKey string) (string, error) {
	key, err := compactionCheckpointSystemKey(projectID, apiKeyID, cacheKey)
	if err != nil {
		return "", err
	}
	ctx = authz.WithSystemBypass(ctx, "load-native-compaction-source")
	// Bypass the ordinary system cache: source contexts can be several MiB.
	record, err := s.entFromContext(ctx).System.Query().Where(system.KeyEQ("native_source:" + key)).Only(ctx)
	if ent.IsNotFound(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return record.Value, nil
}

func (s *SystemService) SaveNativeCompactionSource(ctx context.Context, projectID, apiKeyID int, cacheKey, sealedSource string) error {
	key, err := compactionCheckpointSystemKey(projectID, apiKeyID, cacheKey)
	if err != nil {
		return err
	}
	if sealedSource == "" {
		return errors.New("native compaction source is empty")
	}
	ctx = authz.WithSystemBypass(ctx, "save-native-compaction-source")
	return s.setSystemValue(ctx, "native_source:"+key, sealedSource)
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
