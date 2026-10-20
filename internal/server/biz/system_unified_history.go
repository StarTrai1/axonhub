package biz

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/system"
)

const unifiedHistoryPrefix = "conversation_history_v1:"

func unifiedHistoryOwner(ctx context.Context) (string, error) {
	key, ok := contexts.GetAPIKey(ctx)
	project, found := contexts.GetProjectID(ctx)
	if !ok || !found || key == nil || key.ID <= 0 || project <= 0 || key.ProjectID != project {
		return "", errors.New("unified history requires an authenticated API key and project")
	}
	return fmt.Sprintf("%s%d:%d:", unifiedHistoryPrefix, project, key.ID), nil
}

func validHistoryKey(key string) bool {
	parts := strings.Split(key, ":")
	if len(parts) != 2 || !slices.Contains([]string{"block", "snapshot", "response", "checkpoint", "summary", "summaryref", "view"}, parts[0]) {
		return false
	}
	digest, err := hex.DecodeString(parts[1])
	return err == nil && len(digest) == 32
}

// LoadUnifiedHistory never enters the unbounded system-settings cache. Every
// query includes the authenticated owner prefix even for content hashes.
func (s *SystemService) LoadUnifiedHistory(ctx context.Context, keys []string) (map[string]string, error) {
	prefix, err := unifiedHistoryOwner(ctx)
	if err != nil {
		return nil, err
	}
	if len(keys) > 65536 {
		return nil, errors.New("too many history records")
	}
	ctx = authz.WithSystemBypass(ctx, "load-owned-unified-history")
	result := make(map[string]string, len(keys))
	for offset := 0; offset < len(keys); offset += 128 {
		batch := keys[offset:min(offset+128, len(keys))]
		full := make([]string, len(batch))
		for i, key := range batch {
			if !validHistoryKey(key) {
				return nil, errors.New("invalid history record identity")
			}
			full[i] = prefix + key
		}
		q := s.entFromContext(ctx).System.Query().Where(system.KeyIn(full...))
		rows, err := q.All(ctx)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			result[strings.TrimPrefix(row.Key, prefix)] = row.Value
		}
	}
	return result, nil
}

// SaveUnifiedHistory atomically publishes a manifest with all its encrypted
// blocks. Existing immutable values are retained, not re-encrypted or rewritten.
// Batches avoid SQL parameter limits and content hashes deduplicate only inside
// the authenticated owner. Reusing immutable records performs no SQL update.
func (s *SystemService) SaveUnifiedHistory(ctx context.Context, entries map[string]string) error {
	prefix, err := unifiedHistoryOwner(ctx)
	if err != nil {
		return err
	}
	if len(entries) == 0 || len(entries) > 65536 {
		return errors.New("invalid history batch size")
	}
	keys := make([]string, 0, len(entries))
	for key, value := range entries {
		if !validHistoryKey(key) || value == "" || len(value) > 8<<20 {
			return errors.New("invalid history record")
		}
		keys = append(keys, key)
	}
	slices.Sort(keys)
	ctx = authz.WithSystemBypass(ctx, "save-owned-unified-history")
	return s.RunInTransaction(ctx, func(txCtx context.Context) error {
		client := s.entFromContext(txCtx)
		for offset := 0; offset < len(keys); offset += 128 {
			batch := keys[offset:min(offset+128, len(keys))]
			full := make([]string, len(batch))
			for i, key := range batch {
				full[i] = prefix + key
			}
			existing, err := client.System.Query().Where(system.KeyIn(full...)).Select(system.FieldKey).Strings(txCtx)
			if err != nil {
				return err
			}
			found := make(map[string]bool, len(existing))
			for _, key := range existing {
				found[key] = true
			}
			creates := make([]*ent.SystemCreate, 0, len(batch))
			for _, key := range batch {
				if !found[prefix+key] {
					creates = append(creates, client.System.Create().SetKey(prefix+key).SetValue(entries[key]))
				}
			}
			for pos := 0; pos < len(creates); pos += 32 {
				if err := client.System.CreateBulk(creates[pos:min(pos+32, len(creates))]...).OnConflictColumns(system.FieldKey).DoNothing().Exec(txCtx); err != nil {
					return err
				}
			}
		}
		return nil
	})
}
