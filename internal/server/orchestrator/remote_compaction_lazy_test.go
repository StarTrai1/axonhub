package orchestrator

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/oauth"
	"github.com/looplj/axonhub/llm/streams"
	"github.com/looplj/axonhub/llm/transformer/openai/codex"
)

func TestRemoteCompactionLazyAncestorRecovery(t *testing.T) {
	for _, reject := range []bool{false, true} {
		t.Run(fmt.Sprintf("reject-ancestor=%t", reject), func(t *testing.T) {
			ctx, _, owner, adapter := rejectedCompactionStorage(t)
			provider, err := codex.NewOutboundTransformer(codex.Params{
				BaseURL:       "https://example.invalid/v1",
				TokenProvider: oauth.NewStaticTokenProvider(&oauth.OAuthCredentials{AccessToken: "synthetic-lazy-key"}),
			})
			require.NoError(t, err)
			candidate := &ChannelModelsCandidate{
				Channel: &biz.Channel{Channel: &ent.Channel{ID: 93001, Type: channel.TypeCodex, Settings: &objects.ChannelSettings{PassThroughBody: lo.ToPtr(true)}}, Outbound: provider},
				Models:  []biz.ChannelModelEntry{{RequestModel: "gpt-6-astra", ActualModel: "gpt-6-astra", Source: "direct"}}, APIFormat: string(llm.APIFormatOpenAIResponse),
			}
			state := &PersistenceState{APIKey: owner, ChannelModelsCandidates: []*ChannelModelsCandidate{candidate}}
			latest := &remoteCompactionReference{ID: "cmp_latest", EncryptedContent: "native-latest"}
			ancestor := &remoteCompactionReference{ID: "cmp_ancestor", EncryptedContent: "native-ancestor"}
			first := []byte(`{"model":"gpt-6-astra","client_metadata":{"thread_id":"lazy-thread"},"input":[{"role":"user","content":"first source detail"},{"type":"compaction_trigger"}]}`)
			second := []byte(`{"model":"gpt-6-astra","client_metadata":{"thread_id":"lazy-thread"},"input":[{"role":"user","content":"retained detail"},{"type":"compaction","id":"cmp_ancestor","encrypted_content":"native-ancestor"},{"role":"user","content":"new source detail"},{"type":"compaction_trigger"}]}`)
			require.NoError(t, seedLegacyNativeCompactionSource(ctx, adapter, ancestor, state, &remoteCompactionSource{body: first}))
			require.NoError(t, seedLegacyNativeCompactionSource(ctx, adapter, latest, state, &remoteCompactionSource{body: second}))
			executor := &lazyCompactionExecutor{reject: reject}
			for range 2 {
				summary, err := adapter.summaryForCompaction(ctx, remoteCompactionCacheKey(latest), latest, "lazy-thread", "gpt-6-astra", state, executor)
				require.NoError(t, err)
				require.Equal(t, "verified retained source summary", summary)
				// Cold restart must reuse the sealed result, not generate again.
				adapter = newRemoteCompactionAdapter(adapter.requestService, nil, adapter.systemService)
			}
			require.Len(t, executor.bodies, 1, "one generation must survive a cold adapter restart")
			for _, detail := range []string{"first source detail", "retained detail", "new source detail"} {
				require.Contains(t, string(executor.bodies[0]), detail)
			}
			require.NotContains(t, string(executor.bodies[0]), "native-ancestor")
			require.NotContains(t, string(executor.bodies[0]), "verified retained source summary")
		})
	}
}

type lazyCompactionExecutor struct {
	rejectedCompactionBridgeExecutor

	reject bool
	bodies [][]byte
}

func (e *lazyCompactionExecutor) DoStream(ctx context.Context, req *httpclient.Request) (streams.Stream[*httpclient.StreamEvent], error) {
	e.bodies = append(e.bodies, append([]byte(nil), req.Body...))
	if e.reject && gjson.GetBytes(req.Body, `input.#(type=="compaction")`).Exists() {
		return nil, &httpclient.Error{StatusCode: http.StatusBadRequest, Body: []byte(`{"error":{"type":"invalid_request_error","message":"The encrypted content for item cmp_ancestor could not be verified. Reason: Encrypted content could not be decrypted or parsed."}}`)}
	}
	return e.rejectedCompactionBridgeExecutor.DoStream(ctx, req)
}
