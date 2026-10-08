package orchestrator

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/ent/promptprotectionrule"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
)

func TestSystemOneChannelTestPromptProtection(t *testing.T) {
	for _, tt := range []struct {
		name   string
		action objects.PromptProtectionAction
		scope  objects.PromptProtectionScope
		state  string
	}{
		{"mask system", objects.PromptProtectionActionMask, objects.PromptProtectionScopeSystem, "masked\n\nsecret"},
		{"mask user", objects.PromptProtectionActionMask, objects.PromptProtectionScopeUser, "secret\n\nmasked"},
		{"reject system", objects.PromptProtectionActionReject, objects.PromptProtectionScopeSystem, ""},
		{"reject user", objects.PromptProtectionActionReject, objects.PromptProtectionScopeUser, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			// Given an enabled role-scoped rule and a native System One channel.
			ctx, client := setupTest(t)
			ctx = contexts.WithProjectID(ctx, createTestProject(t, ctx, client).ID)
			_, err := client.PromptProtectionRule.Create().SetName(tt.name).SetPattern("secret").
				SetStatus(promptprotectionrule.StatusEnabled).SetSettings(&objects.PromptProtectionSettings{
				Action: tt.action, Replacement: "masked", Scopes: []objects.PromptProtectionScope{tt.scope},
			}).Save(ctx)
			require.NoError(t, err)
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var body struct {
					State string `json:"state"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.State != tt.state {
					http.Error(w, "unprotected test state", http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"model":"test-model","answers":{"connection":{"type":"noul","noul":0.95}}}`))
			}))
			t.Cleanup(server.Close)
			ch, err := client.Channel.Create().SetType(channel.TypeTypesafe).SetName(tt.name).
				SetBaseURL(server.URL + "/v1").SetCredentials(objects.ChannelCredentials{APIKeys: []string{"test-key"}}).
				SetSupportedModels([]string{"test-model"}).SetDefaultTestModel("test-model").Save(ctx)
			require.NoError(t, err)
			channelService, requestService, systemService, usageLogService := setupTestServices(t, client)
			require.NoError(t, systemService.SetChannelSetting(ctx, biz.SystemChannelSettings{TestSystemPrompt: "secret", TestUserPrompt: "secret"}))
			require.NoError(t, systemService.SetRetryPolicy(ctx, &biz.RetryPolicy{}))
			protection := biz.NewPromptProtectionRuleService(biz.PromptProtectionRuleServiceParams{Ent: client})
			t.Cleanup(protection.Stop)
			processor := NewTestChannelOrchestrator(channelService, requestService, systemService, usageLogService, protection, httpclient.NewHttpClient())
			id := objects.GUID{Type: "Channel", ID: ch.ID}

			// When each public test entrypoint builds and sends its native request.
			result, channelErr := processor.TestChannel(ctx, id, nil, nil, nil)
			keyResult, keyErr := processor.TestSingleAPIKey(ctx, id, "test-key", nil, nil)
			batch, batchErr := processor.TestChannelAPIKeys(ctx, id, nil, nil)

			// Then masks retain role scope, while rejection prevents upstream delivery.
			require.NoError(t, keyErr)
			require.NoError(t, batchErr)
			if tt.action == objects.PromptProtectionActionReject {
				require.ErrorIs(t, channelErr, biz.ErrPromptProtectionRejected)
				require.False(t, keyResult.Success)
				require.Equal(t, 1, batch.FailedCount)
				require.Zero(t, calls.Load())
			} else {
				require.NoError(t, channelErr)
				require.True(t, result.Success)
				require.True(t, keyResult.Success)
				require.Equal(t, 1, batch.SuccessCount)
				require.EqualValues(t, 3, calls.Load())
			}
		})
	}
}

func TestDecisionsChannelTestProtectsUserExactlyOnce(t *testing.T) {
	ctx, client := setupTest(t)
	ctx = contexts.WithProjectID(ctx, createTestProject(t, ctx, client).ID)
	_, err := client.PromptProtectionRule.Create().SetName("single replacement").SetPattern("secret").
		SetStatus(promptprotectionrule.StatusEnabled).SetSettings(&objects.PromptProtectionSettings{
		Action: objects.PromptProtectionActionMask, Replacement: "secret_masked", Scopes: []objects.PromptProtectionScope{objects.PromptProtectionScopeUser},
	}).Save(ctx)
	require.NoError(t, err)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Input string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Input != "secret_masked" {
			http.Error(w, "test prompt was not protected exactly once", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"test-model","answers":[{"type":"predicate","name":"connection","probability":1}]}`))
	}))
	t.Cleanup(server.Close)
	ch, err := client.Channel.Create().SetType(channel.TypeOpenai).SetName("native decisions protection").
		SetBaseURL(server.URL + "/v1").SetCredentials(objects.ChannelCredentials{APIKeys: []string{"test-key"}}).
		SetSupportedModels([]string{"test-model"}).SetDefaultTestModel("test-model").
		SetEndpoints([]objects.ChannelEndpoint{{APIFormat: llm.APIFormatOpenAIDecisions.String()}}).Save(ctx)
	require.NoError(t, err)
	channelService, requestService, systemService, usageLogService := setupTestServices(t, client)
	require.NoError(t, systemService.SetChannelSetting(ctx, biz.SystemChannelSettings{TestSystemPrompt: "system", TestUserPrompt: "secret"}))
	require.NoError(t, systemService.SetRetryPolicy(ctx, &biz.RetryPolicy{}))
	protection := biz.NewPromptProtectionRuleService(biz.PromptProtectionRuleServiceParams{Ent: client})
	t.Cleanup(protection.Stop)
	processor := NewTestChannelOrchestrator(channelService, requestService, systemService, usageLogService, protection, httpclient.NewHttpClient())
	id := objects.GUID{Type: "Channel", ID: ch.ID}
	result, err := processor.TestChannel(ctx, id, nil, nil, nil)
	require.NoError(t, err)
	require.True(t, result.Success)
	keyResult, err := processor.TestSingleAPIKey(ctx, id, "test-key", nil, nil)
	require.NoError(t, err)
	require.True(t, keyResult.Success)
	batch, err := processor.TestChannelAPIKeys(ctx, id, nil, nil)
	require.NoError(t, err)
	require.Equal(t, 1, batch.SuccessCount)
}
