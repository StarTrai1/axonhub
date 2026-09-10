package orchestrator

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/ent/request"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
)

func initializeTestCompactionCipher(t *testing.T, generation *localCompactionGeneration) {
	t.Helper()
	var err error
	generation.summaryCipher, err = newLocalCompactionCipher("test-compaction-installation-secret")
	require.NoError(t, err)
	generation.associatedData, err = localCompactionAssociatedData(generation.ref, &PersistenceState{APIKey: &ent.APIKey{ID: 1, ProjectID: 1}})
	require.NoError(t, err)
}

func TestSealedLocalCompactionRestoresWithoutRequestLogs(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:compaction-capsule?mode=memory&_fk=0")
	t.Cleanup(func() { client.Close() })
	ctx := authz.WithTestBypass(ent.NewContext(context.Background(), client))
	systemService := biz.NewSystemService(biz.SystemServiceParams{Ent: client})
	require.NoError(t, systemService.SetSecretKey(ctx, "test-compaction-installation-secret"))
	ref, err := newLocalCompactionReference()
	require.NoError(t, err)
	cacheKey := remoteCompactionCacheKey(ref)
	generation := &localCompactionGeneration{ref: ref}
	initializeTestCompactionCipher(t, generation)
	summary := "Exact retained decisions, tool results, and user constraints."
	require.NoError(t, generation.sealSummary(summary))
	require.Equal(t, cacheKey, remoteCompactionCacheKey(ref))
	require.NotContains(t, ref.EncryptedContent, summary)
	require.True(t, isSealedLocalCompactionReference(ref))
	state := &PersistenceState{APIKey: &ent.APIKey{ID: 1, ProjectID: 1}}

	// A new adapter with no request service or prior memory must restore directly
	// from the client's capsule, even after a model/thread change.
	restarted := newRemoteCompactionAdapter(nil, nil, biz.NewSystemService(biz.SystemServiceParams{Ent: client}))
	got, err := restarted.summaryForCompaction(ctx, cacheKey, ref, "", "gpt-6-astra", state, nil)
	require.NoError(t, err)
	require.Equal(t, summary, got)
	count, err := client.System.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count, "new capsules must not create per-conversation database records")

	// The request's authenticated identity and the item ID are part of the seal.
	for _, other := range []*ent.APIKey{{ID: 2, ProjectID: 1}, {ID: 1, ProjectID: 2}} {
		_, err = restarted.summaryForCompaction(ctx, cacheKey, ref, "other-thread", "gpt-6-astra", &PersistenceState{APIKey: other}, nil)
		var responseErr *llm.ResponseError
		require.ErrorAs(t, err, &responseErr)
		require.Equal(t, http.StatusBadRequest, responseErr.StatusCode)
		require.Equal(t, "invalid_local_compaction_reference", responseErr.Detail.Code)
	}
	tampered := *ref
	tampered.ID += "-changed"
	_, err = restarted.summaryForCompaction(ctx, remoteCompactionCacheKey(&tampered), &tampered, "thread", "gpt-6-astra", state, nil)
	require.Error(t, err)

	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(ref.EncryptedContent, localCompactionSealedReferencePrefix))
	require.NoError(t, err)
	decoded[len(decoded)/2] ^= 0x80
	tampered = *ref
	tampered.EncryptedContent = localCompactionSealedReferencePrefix + base64.RawURLEncoding.EncodeToString(decoded)
	restarted.summaries.SetDefault(remoteCompactionOwnerCacheKey(state, cacheKey), "must not bypass authentication")
	_, err = restarted.summaryForCompaction(ctx, cacheKey, &tampered, "thread", "gpt-6-astra", state, nil)
	require.Error(t, err)

	for _, malformed := range []string{localCompactionSealedReferencePrefix, localCompactionSealedReferencePrefix + "invalid!", localCompactionSealedReferencePrefix + "AA"} {
		tampered.EncryptedContent = malformed
		_, err = restarted.summaryForCompaction(ctx, cacheKey, &tampered, "thread", "gpt-6-astra", state, nil)
		require.Error(t, err)
	}
	require.NoError(t, systemService.SetSecretKey(ctx, "rotated-test-installation-secret"))
	rotated := newRemoteCompactionAdapter(nil, nil, biz.NewSystemService(biz.SystemServiceParams{Ent: client}))
	_, err = rotated.summaryForCompaction(ctx, cacheKey, ref, "thread", "gpt-6-astra", state, nil)
	require.Error(t, err)
}

func legacyCompactionContinuationFixture(t *testing.T) ([]byte, []byte, string) {
	t.Helper()
	current := []byte(`{
		"model":"gpt-6-astra",
		"client_metadata":{"thread_id":"thread-retained","x-codex-turn-metadata":"{\"context_window_id\":\"window-retained\"}"},
		"input":[
			{"type":"additional_tools","tools":[{"type":"function","name":"inspect","parameters":{"type":"object"}}]},
			{"type":"compaction","id":"cmp_axonhub_legacy","encrypted_content":"axonhub-local-v1.legacy"},
			{"type":"reasoning","id":"rs_retained","summary":[],"content":null,"encrypted_content":"unchanged-reasoning"},
			{"type":"function_call","id":"fc_retained","call_id":"call_retained","name":"inspect","arguments":"{}"},
			{"type":"function_call_output","call_id":"call_retained","output":"verified tool result","internal_chat_message_metadata_passthrough":{"turn_id":"new-client"}},
			{"type":"message","role":"user","content":"continue"}
		]
	}`)
	summary := "Recovered exact summary, without regenerating or dropping history."
	prior, err := replaceRemoteCompactionWithLocalSummary(current, summary)
	require.NoError(t, err)
	var envelope map[string]any
	require.NoError(t, json.Unmarshal(prior, &envelope))
	input := envelope["input"].([]any)
	delete(input[2].(map[string]any), "content")
	input[4].(map[string]any)["internal_chat_message_metadata_passthrough"] = map[string]any{"cell_id": "old-client", "turn_id": "old-turn"}
	envelope["input"] = input[:len(input)-1]
	envelope["model"] = "gpt-5.6-sol"
	prior, err = json.Marshal(envelope)
	require.NoError(t, err)
	return current, prior, summary
}

func TestLegacyLocalCompactionRecoversAndRetainsVerifiedContinuation(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:compaction-legacy-migration?mode=memory&_fk=0")
	t.Cleanup(func() { client.Close() })
	ctx := authz.WithTestBypass(ent.NewContext(context.Background(), client))
	_, err := client.DataStorage.Create().SetName("primary").SetDescription("test storage").SetPrimary(true).SetType("database").SetSettings(new(objects.DataStorageSettings)).Save(ctx)
	require.NoError(t, err)
	apiKey := &ent.APIKey{ID: 71, ProjectID: 72}
	ctx = contexts.WithProjectID(contexts.WithAPIKey(ctx, apiKey), apiKey.ProjectID)
	systemService := biz.NewSystemService(biz.SystemServiceParams{Ent: client})
	require.NoError(t, systemService.SetSecretKey(ctx, "test-compaction-installation-secret"))
	current, prior, summary := legacyCompactionContinuationFixture(t)
	_, err = client.Request.Create().
		SetProjectID(apiKey.ProjectID).
		SetAPIKeyID(apiKey.ID).
		SetModelID("gpt-5.6-sol").
		SetFormat(llm.APIFormatOpenAIResponseWebSocket.String()).
		SetStatus(request.StatusCompleted).
		SetStream(true).
		SetRequestHeaders(objects.JSONRawMessage(`{"Thread-Id":["thread-retained"]}`)).
		SetRequestBody(prior).
		SetResponseBody(objects.JSONRawMessage(`{"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Do not mistake this later answer for the summary"}]}]}`)).
		Save(ctx)
	require.NoError(t, err)
	state := &PersistenceState{APIKey: apiKey, RawRequest: &httpclient.Request{Body: current}}
	ref, threadID, model, err := parseRemoteCompactionRequest(current)
	require.NoError(t, err)
	cacheKey := remoteCompactionCacheKey(ref)
	adapter := newRemoteCompactionAdapter(createTestRequestService(t, client), nil, systemService)
	got, err := adapter.summaryForCompaction(ctx, cacheKey, ref, threadID, model, state, nil)
	require.NoError(t, err)
	require.Equal(t, summary, got)
	sealed, err := systemService.LoadLocalCompactionCheckpoint(ctx, apiKey.ProjectID, apiKey.ID, cacheKey)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(sealed, localCompactionSealedReferencePrefix))
	require.NotContains(t, sealed, summary)

	_, err = client.Request.Delete().Exec(ctx)
	require.NoError(t, err)
	restarted := newRemoteCompactionAdapter(createTestRequestService(t, client), nil, biz.NewSystemService(biz.SystemServiceParams{Ent: client}))
	got, err = restarted.summaryForCompaction(ctx, cacheKey, ref, "resumed-thread", "gpt-6-astra", state, nil)
	require.NoError(t, err)
	require.Equal(t, summary, got)
	for _, other := range []*ent.APIKey{{ID: apiKey.ID + 1, ProjectID: apiKey.ProjectID}, {ID: apiKey.ID, ProjectID: apiKey.ProjectID + 1}} {
		_, err = restarted.summaryForCompaction(ctx, cacheKey, ref, threadID, model, &PersistenceState{APIKey: other}, nil)
		require.Error(t, err)
	}
}

func TestRetainedLocalCompactionRejectsDivergentHistory(t *testing.T) {
	current, prior, summary := legacyCompactionContinuationFixture(t)
	ref, _, _, err := parseRemoteCompactionRequest(current)
	require.NoError(t, err)
	require.Equal(t, summary, retainedLocalCompactionSummary(prior, ref))
	for _, change := range [][2]string{
		{"window-retained", "different-window"},
		{"thread-retained", "different-thread"},
		{"call_retained", "different-call"},
		{"verified tool result", "different tool result"},
		{"unchanged-reasoning", "different-reasoning"},
		{`"name":"inspect"`, `"name":"other"`},
	} {
		t.Run(change[1], func(t *testing.T) {
			changed := []byte(strings.ReplaceAll(string(prior), change[0], change[1]))
			require.NotEqual(t, string(prior), string(changed))
			require.Empty(t, retainedLocalCompactionSummary(changed, ref))
		})
	}
	remote := *ref
	remote.EncryptedContent = "native-provider-opaque-state"
	require.Empty(t, retainedLocalCompactionSummary(prior, &remote))

	var envelope map[string]any
	require.NoError(t, json.Unmarshal(prior, &envelope))
	envelope["input"] = envelope["input"].([]any)[:2]
	noContinuation, err := json.Marshal(envelope)
	require.NoError(t, err)
	require.Empty(t, retainedLocalCompactionSummary(noContinuation, ref))
}
