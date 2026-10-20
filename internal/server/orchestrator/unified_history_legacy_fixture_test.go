package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/klauspost/compress/zstd"
)

// seedLegacyNativeCompactionSource creates the pre-unified format for upgrade tests.
func seedLegacyNativeCompactionSource(ctx context.Context, a *remoteCompactionAdapter, ref *remoteCompactionReference, state *PersistenceState, source *remoteCompactionSource) error {
	encoded, err := json.Marshal(nativeCompactionSourceCheckpoint{Version: 1, Body: source.body})
	if err != nil {
		return err
	}
	if len(encoded) > nativeCompactionMaxCheckpointBytes {
		return errors.New("native compaction source checkpoint exceeds limit")
	}
	compressor, err := zstd.NewWriter(nil, zstd.WithEncoderConcurrency(1))
	if err != nil {
		return err
	}
	compressed := compressor.EncodeAll(encoded, nil)
	_ = compressor.Close()
	if len(compressed) > nativeCompactionMaxCheckpointBytes {
		return errors.New("compressed native compaction source exceeds limit")
	}
	associatedData, err := nativeCompactionSourceAAD(ref, state)
	if err != nil {
		return err
	}
	aead, err := a.localCompactionCipher(ctx)
	if err != nil {
		return err
	}
	sealed, err := sealNativeCompactionSource(aead, associatedData, compressed)
	if err != nil {
		return fmt.Errorf("seal native compaction source: %w", err)
	}
	return a.systemService.SaveNativeCompactionSource(ctx, state.APIKey.ProjectID, state.APIKey.ID, remoteCompactionCacheKey(ref), sealed)
}
