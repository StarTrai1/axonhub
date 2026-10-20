package orchestrator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

const historyBlockBytes = 64 << 10

// The provider body may be a compiled summary view. Keep its archival source
// independent so later previous_response_id deltas never promote that view to
// the authoritative history. Only the main request installs this value.
type unifiedHistoryOriginKey struct{}

func withUnifiedHistoryOrigin(ctx context.Context, body []byte) context.Context {
	return context.WithValue(ctx, unifiedHistoryOriginKey{}, append([]byte(nil), body...))
}

func (a *remoteCompactionAdapter) flattenHistorySource(ctx context.Context, source *remoteCompactionSource, state *PersistenceState) (*remoteCompactionSource, error) {
	resolved, err := a.resolveCompactionSourceContinuation(ctx, source, state)
	if err != nil {
		return nil, err
	}
	expanded, complete, err := a.expandUnifiedHistory(ctx, resolved.body, state, make(map[string]bool))
	if missingHistoryPredecessor(err) {
		// The current native window is complete even when an older opaque root
		// cannot be expanded. Preserve that root, never present it as plaintext.
		log.Warn(ctx, "retained native history with an unavailable archival ancestor")
		return resolved, nil
	}
	if err != nil {
		return nil, err
	}
	if !complete {
		return resolved, nil
	}
	return &remoteCompactionSource{body: expanded, headers: resolved.headers}, nil
}

// Keep a bounded number of small-window encoders between requests. Creating a
// compressor for every history item otherwise dominates short-item archival.
var historyEncoders = make(chan *zstd.Encoder, 4)

func compressHistoryRecord(body []byte) ([]byte, error) {
	var encoder *zstd.Encoder
	select {
	case encoder = <-historyEncoders:
	default:
	}
	if encoder == nil {
		var err error
		encoder, err = zstd.NewWriter(nil, zstd.WithEncoderConcurrency(1), zstd.WithEncoderLevel(zstd.SpeedFastest), zstd.WithWindowSize(historyBlockBytes))
		if err != nil {
			return nil, err
		}
	}
	compressed := encoder.EncodeAll(body, nil)
	select {
	case historyEncoders <- encoder:
	default:
		_ = encoder.Close()
	}
	return compressed, nil
}

// Snapshots are immutable versions, never a mutable "latest thread" pointer.
// Identical blocks share storage; their positions and repetitions remain in the
// manifest. Branches therefore cannot silently consume a sibling's history.
type unifiedHistorySnapshot struct {
	Version  int             `json:"version"`
	Envelope json.RawMessage `json:"envelope"`
	Input    [][]string      `json:"input"`
	Output   [][]string      `json:"output,omitempty"`
	Response json.RawMessage `json:"response,omitempty"`
}

func historyDigest(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func (a *remoteCompactionAdapter) unifiedHistoryEnabled() bool {
	return a != nil && a.systemService != nil
}

func historyState(ctx context.Context) (*PersistenceState, error) {
	key, ok := contexts.GetAPIKey(ctx)
	project, found := contexts.GetProjectID(ctx)
	if !ok || !found || key == nil || key.ID <= 0 || key.ProjectID != project {
		return nil, errors.New("unified history owner is unavailable")
	}
	return &PersistenceState{APIKey: key}, nil
}

func (a *remoteCompactionAdapter) sealHistoryRecord(ctx context.Context, key string, body []byte) (string, error) {
	state, err := historyState(ctx)
	if err != nil {
		return "", err
	}
	aead, err := a.localCompactionCipher(ctx)
	if err != nil {
		return "", err
	}
	aad := fmt.Appendf(nil, "axonhub/unified-history/v1:%d:%d:%s", state.APIKey.ProjectID, state.APIKey.ID, key)
	compressed, err := compressHistoryRecord(body)
	if err != nil {
		return "", err
	}
	return sealNativeCompactionSource(aead, aad, compressed)
}

func (a *remoteCompactionAdapter) openHistoryRecord(ctx context.Context, key, sealed string, limit int) ([]byte, error) {
	state, err := historyState(ctx)
	if err != nil {
		return nil, err
	}
	aead, err := a.localCompactionCipher(ctx)
	if err != nil {
		return nil, err
	}
	aad := fmt.Appendf(nil, "axonhub/unified-history/v1:%d:%d:%s", state.APIKey.ProjectID, state.APIKey.ID, key)
	compressed, err := openNativeCompactionSource(aead, aad, sealed)
	if err != nil {
		return nil, err
	}
	z, err := zstd.NewReader(nil, zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxMemory(uint64(max(limit, historyBlockBytes))))
	if err != nil {
		return nil, err
	}
	defer z.Close()
	body, err := z.DecodeAll(compressed, nil)
	if err != nil || len(body) > limit {
		return nil, errors.New("invalid or oversized unified history record")
	}
	return body, nil
}

func (a *remoteCompactionAdapter) saveHistorySnapshot(ctx context.Context, alias string, body, response []byte) error {
	if len(body)+len(response) > nativeCompactionMaxSourceBytes {
		return errors.New("unified history snapshot exceeds 64 MiB")
	}
	envelope, input, err := decodeResponsesInput(body)
	if err != nil {
		return err
	}
	if gjson.GetBytes(body, "previous_response_id").String() != "" {
		return errors.New("cannot archive an unresolved history delta")
	}
	delete(envelope, "input")
	header, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	snapshot := unifiedHistorySnapshot{Version: 1, Envelope: header}
	blocks := make(map[string][]byte)
	pack := func(items []json.RawMessage) [][]string {
		result := make([][]string, 0, len(items))
		for _, item := range items {
			var ids []string
			for offset := 0; offset < len(item); offset += historyBlockBytes {
				part := item[offset:min(offset+historyBlockBytes, len(item))]
				key := "block:" + historyDigest(part)
				blocks[key] = part
				ids = append(ids, key)
			}
			result = append(result, ids)
		}
		return result
	}
	snapshot.Input = pack(input)
	if len(response) > 0 {
		var value map[string]json.RawMessage
		if err := json.Unmarshal(response, &value); err != nil {
			return err
		}
		var output []json.RawMessage
		if err := json.Unmarshal(value["output"], &output); err != nil {
			return err
		}
		snapshot.Output = pack(output)
		delete(value, "output")
		snapshot.Response, err = json.Marshal(value)
		if err != nil {
			return err
		}
	}
	manifest, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	if len(manifest) > 4<<20 || len(blocks) > 65530 {
		return errors.New("unified history manifest exceeds limit")
	}
	root := "snapshot:" + historyDigest(manifest)
	blocks[root] = manifest
	blocks[alias] = []byte(root)
	keys := make([]string, 0, len(blocks))
	for key := range blocks {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	existing, err := a.systemService.LoadUnifiedHistory(ctx, keys)
	if err != nil {
		return err
	}
	entries := make(map[string]string, len(blocks))
	for _, key := range keys {
		if sealed := existing[key]; sealed != "" {
			if key == alias {
				prior, err := a.openHistoryRecord(ctx, key, sealed, 128)
				if err != nil {
					return err
				}
				if string(prior) != root {
					return errors.New("history identifier already belongs to a different immutable version")
				}
			}
			entries[key] = sealed
			continue
		}
		sealed, err := a.sealHistoryRecord(ctx, key, blocks[key])
		if err != nil {
			return err
		}
		entries[key] = sealed
	}
	// Verify the published alias inside the same transaction. A competing writer
	// may have won after our initial lookup; rolling back also removes this
	// losing publication's otherwise unreachable blocks.
	return a.systemService.RunInTransaction(ctx, func(txCtx context.Context) error {
		if err := a.systemService.SaveUnifiedHistory(txCtx, entries); err != nil {
			return err
		}
		published, err := a.systemService.LoadUnifiedHistory(txCtx, []string{alias})
		if err != nil {
			return err
		}
		prior, err := a.openHistoryRecord(txCtx, alias, published[alias], 128)
		if err != nil {
			return err
		}
		if string(prior) != root {
			return errors.New("history identifier already belongs to a different immutable version")
		}
		return nil
	})
}

func (a *remoteCompactionAdapter) loadHistorySnapshot(ctx context.Context, alias string) ([]byte, []byte, bool, error) {
	entries, err := a.systemService.LoadUnifiedHistory(ctx, []string{alias})
	if err != nil {
		return nil, nil, false, err
	}
	if entries[alias] == "" {
		return nil, nil, false, nil
	}
	rootBytes, err := a.openHistoryRecord(ctx, alias, entries[alias], 128)
	if err != nil {
		return nil, nil, false, err
	}
	root := string(rootBytes)
	entries, err = a.systemService.LoadUnifiedHistory(ctx, []string{root})
	if err != nil {
		return nil, nil, false, err
	}
	if entries[root] == "" {
		return nil, nil, false, errors.New("history snapshot is missing or expired")
	}
	manifest, err := a.openHistoryRecord(ctx, root, entries[root], 4<<20)
	if err != nil {
		return nil, nil, false, err
	}
	if root != "snapshot:"+historyDigest(manifest) {
		return nil, nil, false, errors.New("history manifest digest mismatch")
	}
	var snapshot unifiedHistorySnapshot
	if err := json.Unmarshal(manifest, &snapshot); err != nil || snapshot.Version != 1 {
		return nil, nil, false, errors.New("unsupported history manifest")
	}
	keys := make([]string, 0)
	for _, items := range [][][]string{snapshot.Input, snapshot.Output} {
		for _, item := range items {
			keys = append(keys, item...)
		}
	}
	slices.Sort(keys)
	keys = slices.Compact(keys)
	entries, err = a.systemService.LoadUnifiedHistory(ctx, keys)
	if err != nil {
		return nil, nil, false, err
	}
	total := len(snapshot.Envelope) + len(snapshot.Response)
	decoded := make(map[string][]byte, len(keys))
	for _, key := range keys {
		if entries[key] == "" {
			return nil, nil, false, errors.New("history block is missing or expired")
		}
		part, err := a.openHistoryRecord(ctx, key, entries[key], historyBlockBytes)
		if err != nil {
			return nil, nil, false, err
		}
		if key != "block:"+historyDigest(part) {
			return nil, nil, false, errors.New("history block digest mismatch")
		}
		decoded[key] = part
	}
	unpack := func(refs [][]string) ([]json.RawMessage, error) {
		items := make([]json.RawMessage, 0, len(refs))
		for _, ids := range refs {
			var item []byte
			for _, id := range ids {
				total += len(decoded[id])
				if total > nativeCompactionMaxSourceBytes {
					return nil, errors.New("expanded history exceeds 64 MiB")
				}
				item = append(item, decoded[id]...)
			}
			if !json.Valid(item) {
				return nil, errors.New("invalid archived history item")
			}
			items = append(items, item)
		}
		return items, nil
	}
	input, err := unpack(snapshot.Input)
	if err != nil {
		return nil, nil, false, err
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(snapshot.Envelope, &envelope); err != nil {
		return nil, nil, false, err
	}
	if err := setResponseEnvelopeInput(envelope, input); err != nil {
		return nil, nil, false, err
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		return nil, nil, false, err
	}
	var response []byte
	if len(snapshot.Response) > 0 {
		output, err := unpack(snapshot.Output)
		if err != nil {
			return nil, nil, false, err
		}
		var value map[string]json.RawMessage
		if err := json.Unmarshal(snapshot.Response, &value); err != nil {
			return nil, nil, false, err
		}
		value["output"], err = json.Marshal(output)
		if err != nil {
			return nil, nil, false, err
		}
		response, err = json.Marshal(value)
		if err != nil {
			return nil, nil, false, err
		}
	}
	return body, response, true, nil
}

func historyResponseAlias(id string) string { return "response:" + historyDigest([]byte(id)) }

func historyChannelID(state *PersistenceState) int {
	if state != nil && state.CurrentCandidate != nil && state.CurrentCandidate.Channel != nil {
		return state.CurrentCandidate.Channel.ID
	}
	return 0
}

func historyWireAlias(id string, channelID int) string {
	return "wire:" + historyDigest(fmt.Appendf(nil, "%d:%s", channelID, id))
}

func historyCheckpointAlias(ref *remoteCompactionReference) string {
	if isLocalCompactionReference(ref) {
		return "checkpoint:" + historyDigest([]byte(ref.ID))
	}
	return "checkpoint:" + remoteCompactionCacheKey(ref)
}

func (a *remoteCompactionAdapter) archiveHistoryResponse(ctx context.Context, body, response []byte) error {
	if !a.unifiedHistoryEnabled() {
		return nil
	}
	if gjson.GetBytes(response, "status").String() != "completed" || gjson.GetBytes(response, "error").Type != gjson.Null {
		return nil
	}
	id := gjson.GetBytes(response, "id").String()
	if id == "" {
		return nil
	}
	state, err := historyState(ctx)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	hasCheckpoint := false
	for _, item := range gjson.GetBytes(response, "output").Array() {
		if item.Get("type").String() == remoteCompactionItemType || item.Get("type").String() == legacyRemoteCompactionSummaryType {
			hasCheckpoint = true
		}
	}
	// A local compaction executor first emits assistant text internally. Only
	// its final checkpoint is an authoritative output for downstream deltas.
	if gjson.GetBytes(body, `input.#(type=="compaction_trigger")`).Exists() && !hasCheckpoint {
		return nil
	}
	// Persist the accepted provider window independently of the raw archive.
	// A same-channel reconnect must never expand private ancestor checkpoints.
	channelID := 0
	if attempt := shared.GetResponsesAttempt(ctx); attempt != nil {
		channelID = attempt.Origin().ChannelID
 if err := a.rememberNativeHistoryOrigins(ctx, body, response, attempt.Origin()); err != nil { return err }
	}
	wireState := *state
	if channelID != 0 {
		wireState.CurrentCandidate = &ChannelModelsCandidate{Channel: &biz.Channel{Channel: &ent.Channel{ID: channelID}}}
	}
	wire, wireErr := a.resolveRetainedContinuation(ctx, &remoteCompactionSource{body: body}, &wireState, true)
	if wireErr == nil {
 if hasCheckpoint {
 envelope, input, err := decodeResponsesInput(wire.body)
 if err != nil { return err }
 if len(input) > 0 && rawInputItemType(input[len(input)-1]) == remoteCompactionTriggerType {
 if err := setResponseEnvelopeInput(envelope, input[:len(input)-1]); err != nil { return err }
 wire.body, err = json.Marshal(envelope)
 if err != nil { return err }
 }
 }
		if err := a.saveHistorySnapshot(ctx, historyWireAlias(id, channelID), wire.body, response); err != nil {
			return err
		}
 if attempt := shared.GetResponsesAttempt(ctx); attempt != nil {
 if err := a.saveHistoryWindow(ctx, id, attempt.Origin()); err != nil { return err }
 }
	} else if !missingHistoryPredecessor(wireErr) {
		return wireErr
	}
	if original, ok := ctx.Value(unifiedHistoryOriginKey{}).([]byte); ok && len(original) > 0 {
		body = original
	}
	source, err := a.flattenHistorySource(ctx, &remoteCompactionSource{body: body}, state)
	if err != nil {
		return err
	}
	if hasCheckpoint {
		envelope, input, err := decodeResponsesInput(source.body)
		if err != nil {
			return err
		}
		if len(input) > 0 && rawInputItemType(input[len(input)-1]) == remoteCompactionTriggerType {
			if err := setResponseEnvelopeInput(envelope, input[:len(input)-1]); err != nil {
				return err
			}
			source.body, err = json.Marshal(envelope)
			if err != nil {
				return err
			}
		}
	}
	return a.saveHistorySnapshot(ctx, historyResponseAlias(id), source.body, response)
}
