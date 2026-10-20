package orchestrator

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm/transformer/shared"
)

type unifiedHistoryWindow struct {
	Version int `json:"version"`
	Alias string `json:"alias"`
	Origin shared.ResponsesOrigin `json:"origin"`
}

func historyWindowKey(id string) string {
	return "origin:" + historyDigest([]byte("accepted-response-window-v1:" + id))
}

func historyUnresolvedNativeKey(ref *remoteCompactionReference) string {
	return "native:" + historyDigest([]byte("unresolved-native-source-v1:" + remoteCompactionCacheKey(ref)))
}

// Preserve a valid upstream checkpoint even if an old connection's delta cannot
// yet be materialized. Keep the unresolved link explicitly outside the complete
// archive namespace; it cannot be replayed as if its predecessor were present.
func (a *remoteCompactionAdapter) retainUnresolvedNativeSource(ctx context.Context, ref *remoteCompactionReference, state *PersistenceState, source *remoteCompactionSource) error {
	owner, err := historyState(ctx)
	if err != nil { return err }
	if state == nil || state.APIKey == nil || owner.APIKey.ID != state.APIKey.ID || owner.APIKey.ProjectID != state.APIKey.ProjectID { return errors.New("unresolved native history owner mismatch") }
	key := historyUnresolvedNativeKey(ref)
	sealed, err := a.sealHistoryRecord(ctx, key, source.body)
	if err != nil { return err }
	return a.systemService.SaveUnifiedHistory(ctx, map[string]string{key:sealed})
}

func (a *remoteCompactionAdapter) saveHistoryWindow(ctx context.Context, id string, origin shared.ResponsesOrigin) error {
	if origin.ChannelID <= 0 || origin.Endpoint == "" || origin.Auth == "" || origin.Model == "" { return nil }
	origin.Generation = 0
	value, err := json.Marshal(unifiedHistoryWindow{Version:1, Alias:historyWireAlias(id, origin.ChannelID), Origin:origin})
	if err != nil { return err }
	key := historyWindowKey(id)
	sealed, err := a.sealHistoryRecord(ctx, key, value)
	if err != nil { return err }
	return a.systemService.SaveUnifiedHistory(ctx, map[string]string{key:sealed})
}

func (a *remoteCompactionAdapter) loadHistoryWindow(ctx context.Context, id string) ([]byte, []byte, shared.ResponsesOrigin, bool, error) {
	var window unifiedHistoryWindow
	key := historyWindowKey(id)
	entries, err := a.systemService.LoadUnifiedHistory(ctx, []string{key})
	if err != nil || entries[key] == "" { return nil, nil, window.Origin, false, err }
	value, err := a.openHistoryRecord(ctx, key, entries[key], 8192)
	if err != nil { return nil, nil, window.Origin, false, err }
	if json.Unmarshal(value, &window) != nil || window.Version != 1 || window.Origin.ChannelID <= 0 || window.Origin.Auth == "" ||
		window.Alias != historyWireAlias(id, window.Origin.ChannelID) {
		return nil, nil, window.Origin, false, errors.New("invalid accepted history window")
	}
	body, response, found, err := a.loadHistorySnapshot(ctx, window.Alias)
	if err != nil { return nil, nil, window.Origin, false, err }
	if !found || gjson.GetBytes(response, "id").String() != id || gjson.GetBytes(body, "model").String() != window.Origin.Model {
		return nil, nil, window.Origin, false, errors.New("accepted history window is unavailable or mismatched")
	}
	return body, response, window.Origin, true, nil
}
