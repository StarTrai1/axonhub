package orchestrator

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

func TestNativeCompactionMigrationStorageAndIsolation(t *testing.T) {
	ctx, client, owner, adapter := rejectedCompactionStorage(t)
	key := responsesCompactionRecoveryKey{apiKeyID: owner.ID, projectID: owner.ProjectID, cacheKey: "source-checkpoint-hash", threadID: "migration-thread"}
	key.scope.provider.channelID = 41
	key.scope.provider.model = "gpt-6-astra"
	key.scope.provider.url = "https://example.invalid/v1/responses"
	key.scope.provider.credential[0] = 7
	item := json.RawMessage(`{"type":"compaction","id":"cmp_target","encrypted_content":"opaque-target"}`)
	require.NoError(t, adapter.saveNativeMigration(ctx, key, nativeCompactionMigration{Item: item}))
	restarted := newRemoteCompactionAdapter(adapter.requestService, nil, biz.NewSystemService(biz.SystemServiceParams{Ent: client}))
	value, err := restarted.loadNativeMigration(ctx, key)
	require.NoError(t, err)
	require.JSONEq(t, string(item), string(value.Item))
	sealed, err := adapter.systemService.LoadNativeCompactionMigration(ctx, key.digest())
	require.NoError(t, err)
	for _, mutate := range []func(*responsesCompactionRecoveryKey){
		func(k *responsesCompactionRecoveryKey) { k.apiKeyID++ },
		func(k *responsesCompactionRecoveryKey) { k.projectID++ },
		func(k *responsesCompactionRecoveryKey) { k.cacheKey = "different ciphertext" },
		func(k *responsesCompactionRecoveryKey) { k.threadID = "different thread" },
		func(k *responsesCompactionRecoveryKey) { k.scope.provider.channelID++ },
		func(k *responsesCompactionRecoveryKey) { k.scope.provider.credential[0]++ },
		func(k *responsesCompactionRecoveryKey) { k.scope.provider.model = "another model" },
		func(k *responsesCompactionRecoveryKey) { k.scope.provider.url += "/other" },
		func(k *responsesCompactionRecoveryKey) { k.scope.windowID = "other window" },
	} {
		other := key
		mutate(&other)
		value, err := restarted.loadNativeMigration(ctx, other)
		require.NoError(t, err)
		require.Empty(t, value.Item)
		// Copying a sealed alias into another scope cannot bypass authentication.
		require.NoError(t, adapter.systemService.SaveNativeCompactionMigration(ctx, other.digest(), sealed))
		restarted.nativeMigrations.Remove(other)
		_, err = restarted.loadNativeMigration(ctx, other)
		require.Error(t, err)
	}
	require.NoError(t, adapter.saveNativeMigration(ctx, key, nativeCompactionMigration{RejectedUntil: time.Now().Add(time.Hour)}))
	adapter = newRemoteCompactionAdapter(adapter.requestService, nil, adapter.systemService)
	value, err = adapter.loadNativeMigration(ctx, key)
	require.NoError(t, err)
	require.Empty(t, value.Item)
	require.True(t, time.Now().Before(value.RejectedUntil))
}

func TestNativeCompactionMigrationReplacesOnlyCheckpoint(t *testing.T) {
	body := []byte(responsesCompactionHistoryFixture)
	ref, _, _, err := parseRemoteCompactionRequest(body)
	require.NoError(t, err)
	item := json.RawMessage(`{"type":"compaction","id":"cmp_destination","encrypted_content":"new-native-ciphertext"}`)
	got, err := replaceNativeCompactionMigration(body, ref, item)
	require.NoError(t, err)
	for i, prior := range gjson.GetBytes(body, "input").Array() {
		if i == ref.Index {
			continue
		}
		require.JSONEq(t, prior.Raw, gjson.GetBytes(got, "input").Array()[i].Raw)
	}
	for _, field := range []string{"reasoning", "prompt_cache_key", "client_metadata", "model", "store"} {
		require.JSONEq(t, gjson.GetBytes(body, field).Raw, gjson.GetBytes(got, field).Raw)
	}
	for _, response := range []string{
		`{"status":"incomplete","output":[{"type":"compaction","id":"cmp_target","encrypted_content":"cipher"}]}`,
		`{"status":"completed","output":[{"type":"message","content":[]}]}`,
		`{"status":"completed","output":[{"type":"compaction","id":"cmp_one","encrypted_content":"one"},{"type":"compaction","id":"cmp_two","encrypted_content":"two"}]}`,
	} {
		_, err := extractNativeCompactionMigration([]byte(response))
		require.Error(t, err)
	}
}

func TestNativeCompactionMigrationDestinationIdentity(t *testing.T) {
	source := &remoteCompactionSource{body: []byte(`{"model":"gpt-6-astra","instructions":"original instructions","prompt_cache_key":"old-cache","client_metadata":{"thread_id":"old-thread","x-codex-window-id":"old-window","x-codex-turn-metadata":"{\"request_kind\":\"compaction\",\"thread_id\":\"old-thread\",\"window_id\":\"old-window\",\"compaction\":{\"implementation\":\"responses_compaction_v2\"}}"},"input":[{"role":"user","content":"complete source"},{"type":"compaction_trigger"}]}`)}
	destination := &httpclient.Request{Body: []byte(`{"prompt_cache_key":"target-cache","client_metadata":{"thread_id":"target-thread","x-codex-window-id":"target-window","x-codex-turn-metadata":"{\"session_id\":\"target-session\",\"context_window_id\":\"target-context\",\"request_kind\":\"turn\"}"}}`)}
	bound, err := bindNativeMigrationDestination(source, destination)
	require.NoError(t, err)
	metadata := shared.ReadCodexRequestMetadata(bound.headers, bound.body)
	require.Equal(t, "target-thread", metadata.ThreadID)
	require.Equal(t, "target-window", metadata.WindowID)
	require.Equal(t, "target-session", metadata.SessionID)
	require.Equal(t, "compaction", metadata.RequestKind)
	require.Equal(t, "target-context", gjson.Get(metadata.RawTurnMetadata, "context_window_id").String())
	require.Equal(t, "responses_compaction_v2", gjson.Get(metadata.RawTurnMetadata, "compaction.implementation").String())
	require.Equal(t, "target-cache", gjson.GetBytes(bound.body, "prompt_cache_key").String())
	require.JSONEq(t, gjson.GetBytes(source.body, "input").Raw, gjson.GetBytes(bound.body, "input").Raw)
	require.Equal(t, "original instructions", gjson.GetBytes(bound.body, "instructions").String())
	require.Equal(t, "old-cache", gjson.GetBytes(source.body, "prompt_cache_key").String())
}
