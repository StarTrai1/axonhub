package orchestrator

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/oauth"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/transformer/openai/codex"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

func TestResponsesAgentHistoryWebSocketCompactionSwitch(t *testing.T) {
	for _, raw := range []bool{true, false} {
		for _, scenario := range []string{"sealed local to native", "native to local", "foreign native to native"} {
			name := scenario+map[bool]string{true:"/raw", false:"/converted"}[raw]
			t.Run(name, func(t *testing.T) {
				ctx, _, apiKey, adapter := rejectedCompactionStorage(t)
				ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
				defer cancel()
				req := agentRecoveryRequest(t, llm.APIFormatOpenAIResponse)
				ref, _, _, err := parseRemoteCompactionRequest(req.Body)
				require.NoError(t, err)
				owner := &PersistenceState{APIKey:apiKey}
				const summary = "Authenticated summary survives strategy and transport changes."
				policy := objects.RemoteCompactionPolicyNative
				if scenario == "sealed local to native" {
					ref, err = newLocalCompactionReference()
					require.NoError(t, err)
					cipher, cipherErr := adapter.localCompactionCipher(ctx)
					require.NoError(t, cipherErr)
					aad, aadErr := localCompactionAssociatedData(ref, owner)
					require.NoError(t, aadErr)
					ref.EncryptedContent, err = sealLocalCompactionSummary(cipher, aad, summary)
					require.NoError(t, err)
					req.Body, err = sjson.SetBytes(req.Body, "input.1.id", ref.ID)
					require.NoError(t, err)
					req.Body, err = sjson.SetBytes(req.Body, "input.1.encrypted_content", ref.EncryptedContent)
					require.NoError(t, err)
				} else {
					require.NoError(t, adapter.retainCompactionSummary(ctx, remoteCompactionCacheKey(ref), ref, owner, summary))
					if scenario == "native to local" { policy = objects.RemoteCompactionPolicyLocalBridge }
				}
				// Empty caches and retained authenticated storage model a restarted
				// gateway; request logs are not needed for either bridge direction.
				adapter = newRemoteCompactionAdapter(nil, nil, adapter.systemService)
				original := append([]byte(nil), req.Body...)
				frames := make(chan []byte, 8)
				var attempts atomic.Int32
				upgrader := websocket.Upgrader{}
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					conn, upgradeErr := upgrader.Upgrade(w,r,nil)
					if upgradeErr != nil { t.Errorf("upgrade: %v",upgradeErr); return }
					defer conn.Close()
					_ = conn.SetReadDeadline(time.Now().Add(10*time.Second))
					_, frame, readErr := conn.ReadMessage()
					if readErr != nil { t.Errorf("read: %v",readErr); return }
					frames <- frame
					attempt := attempts.Add(1)
					message := ""
					if attempt == 1 { message = rejectedReasoningMessage }
					if attempt == 2 && scenario == "foreign native to native" { message = rejectedNativeCompactionMessage }
					if message != "" {
						if writeErr := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"error","status":400,"error":{"type":"invalid_request_error","code":"invalid_encrypted_content","param":"input","message":"`+message+`"}}`)); writeErr != nil { t.Errorf("error frame: %v",writeErr) }
						return
					}
					for _, event := range rejectedReasoningCompactionEvents() {
						if writeErr := conn.WriteMessage(websocket.TextMessage,event.Data); writeErr != nil { t.Errorf("completion: %v",writeErr); return }
					}
				}))
				t.Cleanup(server.Close)
				provider, err := codex.NewOutboundTransformer(codex.Params{BaseURL:server.URL+"/backend-api/codex#", Transport:responses.TransportWebSocket, TokenProvider:oauth.NewStaticTokenProvider(&oauth.OAuthCredentials{AccessToken:"synthetic-oauth-token"})})
				require.NoError(t, err)
				t.Cleanup(provider.Stop)
				executor := httpclient.NewHttpClientWithProxy(&httpclient.ProxyConfig{Type:httpclient.ProxyTypeDisabled})
				_, result, err := runRejectedReasoningPipeline(t, ctx, req, executor, "unused-fixture-token", raw, 2,
					func(state *PersistenceState, _ *PersistentOutboundTransformer) pipeline.Middleware {
						state.APIKey = apiKey
						channel := state.ChannelModelsCandidates[0].Channel
						channel.Outbound = provider
						channel.Policies.RemoteCompaction = policy
						return &remoteCompactionMiddleware{inbound:&PersistentInboundTransformer{state:state},adapter:adapter,executor:executor}
					},
					func(_ *PersistenceState, outbound *PersistentOutboundTransformer) pipeline.Middleware { return recoverRejectedRemoteCompaction(outbound,adapter,executor) },
				)
				require.NoError(t, err)
				drainRejectedReasoningPipeline(t,result)
				want := int32(2)
				if scenario == "foreign native to native" { want = 3 }
				require.Equal(t,want,attempts.Load())
				var last []byte
				for i:=int32(0);i<want;i++ {
					select {
					case last = <-frames:
					case <-ctx.Done(): t.Fatal("missing captured WebSocket frame")
					}
					require.Contains(t,string(last),preservedEncryptedAgentMessage)
					require.Contains(t,string(last),preservedPlainAgentMessage)
					require.NotContains(t,string(last),localCompactionSealedReferencePrefix)
					if scenario != "foreign native to native" { require.Contains(t,string(last),summary) }
				}
				require.Contains(t,string(last),summary)
				require.Empty(t,encryptedResponsesReasoningHashes(last))
				require.Equal(t,original,req.Body)
				require.Equal(t,"response.create",gjson.GetBytes(last,"type").String())
			})
		}
	}
}
