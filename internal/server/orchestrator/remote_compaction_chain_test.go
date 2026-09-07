package orchestrator

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/internal/ent"
)

func TestStoredCompactionSourceRestoresEarlierGeneration(t *testing.T) {
	adapter := newRemoteCompactionAdapter(nil, nil, nil)
	ref := &remoteCompactionReference{ID: "cmp_earlier", EncryptedContent: "opaque-earlier"}
	state := &PersistenceState{APIKey: &ent.APIKey{ID: 1, ProjectID: 1}}
	adapter.summaries.SetDefault(remoteCompactionOwnerCacheKey(state, remoteCompactionCacheKey(ref)), "earlier history and decisions")
	body := []byte(`{"model":"gpt-6-astra","client_metadata":{"thread_id":"thread-1"},"input":[{"type":"message","role":"user","content":"keep user history"},{"type":"compaction","id":"cmp_earlier","encrypted_content":"opaque-earlier"},{"type":"reasoning","id":"rs_native","encrypted_content":"keep-reasoning"},{"type":"function_call","id":"fc_native","call_id":"call_native","name":"exec","arguments":"{}"},{"type":"function_call_output","call_id":"call_native","output":"keep output"},{"type":"compaction_trigger"}]}`)
	source := &remoteCompactionSource{body: body, headers: http.Header{"Thread-Id": []string{"thread-1"}}}
	adapted, err := adapter.adaptStoredCompactionSource(context.Background(), source, state, nil, map[string]struct{}{"current-generation": {}})
	require.NoError(t, err)
	require.NotSame(t, source, adapted)
	require.Equal(t, body, source.body)
	require.Equal(t, source.headers, adapted.headers)
	require.Empty(t, gjson.GetBytes(adapted.body, `input.#(type=="compaction")`).Raw)
	require.Equal(t, "keep user history", gjson.GetBytes(adapted.body, "input.0.content").String())
	require.Contains(t, gjson.GetBytes(adapted.body, "input.1.content.0.text").String(), "earlier history and decisions")
	require.Equal(t, "keep-reasoning", gjson.GetBytes(adapted.body, "input.2.encrypted_content").String())
	require.Equal(t, "fc_native", gjson.GetBytes(adapted.body, "input.3.id").String())
	require.Equal(t, "call_native", gjson.GetBytes(adapted.body, "input.4.call_id").String())
	require.Equal(t, "compaction_trigger", gjson.GetBytes(adapted.body, "input.5.type").String())
	generated, err := buildLocalCompactionRequest(adapted.body)
	require.NoError(t, err)
	require.Empty(t, gjson.GetBytes(generated, `input.#(type=="compaction")`).Raw)
	require.Empty(t, gjson.GetBytes(generated, `input.#(type=="compaction_trigger")`).Raw)
	require.Equal(t, localCompactionPrompt, gjson.GetBytes(generated, "input.5.content.0.text").String())
}

func TestStoredCompactionSourceRejectsCyclesAndDepthLimit(t *testing.T) {
	adapter := newRemoteCompactionAdapter(nil, nil, nil)
	ref := &remoteCompactionReference{ID: "cmp_cycle", EncryptedContent: "opaque"}
	cacheKey := remoteCompactionCacheKey(ref)
	_, err := adapter.summaryForCompactionChain(context.Background(), cacheKey, ref, "thread", "gpt-6-astra", nil, nil, map[string]struct{}{cacheKey: {}})
	require.ErrorContains(t, err, "cycle")
	ancestors := make(map[string]struct{}, remoteCompactionMaxSourceDepth)
	for index := range remoteCompactionMaxSourceDepth {
		ancestors[fmt.Sprintf("ancestor-%d", index)] = struct{}{}
	}
	_, err = adapter.summaryForCompactionChain(context.Background(), cacheKey, ref, "thread", "gpt-6-astra", nil, nil, ancestors)
	require.ErrorContains(t, err, "exceeds")
}

func TestStoredCompactionSourceFailsClosedWithoutEarlierHistory(t *testing.T) {
	adapter := newRemoteCompactionAdapter(nil, nil, nil)
	body := []byte(`{"model":"gpt-6-astra","client_metadata":{"thread_id":"thread"},"input":[{"type":"compaction","id":"cmp_missing","encrypted_content":"must-not-be-dropped"},{"type":"compaction_trigger"}]}`)
	source := &remoteCompactionSource{body: body}
	adapted, err := adapter.adaptStoredCompactionSource(context.Background(), source, nil, nil, make(map[string]struct{}))
	require.ErrorContains(t, err, "request history is unavailable")
	require.Nil(t, adapted)
	require.Equal(t, body, source.body)
}
