package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/ent/providerquotastatus"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/pkg/xcache"
	"github.com/looplj/axonhub/internal/server/biz"
)

func TestProbeQuotaOnlyReturnsPinnedResetTimes(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:probe-quota?mode=memory&_fk=1")
	ctx := authz.WithTestBypass(ent.NewContext(t.Context(), client))
	system := biz.NewSystemService(biz.SystemServiceParams{Ent: client, CacheConfig: xcache.Config{Mode: xcache.ModeMemory}})
	require.NoError(t, system.UpdateProviderQuotaCollectionSettings(ctx, lo.ToPtr(true), []biz.ProviderQuotaCollectionProvider{{Provider: "codex", Enabled: true}}))
	ch, err := client.Channel.Create().SetName("private-key-channel").SetType(channel.TypeCodex).
		SetStatus(channel.StatusEnabled).SetCredentials(objects.ChannelCredentials{APIKey: "never-return-this-secret"}).
		SetSupportedModels([]string{"test"}).SetDefaultTestModel("test").SetTags([]string{"permitted"}).Save(ctx)
	require.NoError(t, err)
	reset := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	_, err = client.ProviderQuotaStatus.Create().SetChannelID(ch.ID).
		SetProviderType(providerquotastatus.ProviderTypeCodex).SetStatus(providerquotastatus.StatusAvailable).
		SetNextCheckAt(time.Now().Add(time.Minute)).SetAccountKey("private-account").
		SetQuotaData(map[string]any{
			"raw_secret": "do-not-return",
			"_limits": []any{
				map[string]any{"type": "token", "window": "7d", "nextResetAt": reset.Format(time.RFC3339), "periodCost": 42},
				map[string]any{"type": "image", "window": "7d", "nextResetAt": reset.Format(time.RFC3339)},
			},
		}).Save(ctx)
	require.NoError(t, err)
	key := &ent.APIKey{
		ID:        4,
		ProjectID: 7,
		Key:       "synthetic-probe-key",
		Profiles:  &objects.APIKeyProfiles{ActiveProfile: "probe", Profiles: []objects.APIKeyProfile{{Name: "probe", ChannelIDs: []int{ch.ID}}}},
		Edges:     ent.APIKeyEdges{Project: &ent.Project{ID: 7}},
	}
	handler := NewProbeQuotaHandlers(ProbeQuotaHandlersParams{Ent: client, SystemService: system})
	call := func(key *ent.APIKey) *httptest.ResponseRecorder {
		t.Helper()
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		// No administrator/system bypass supplied to the real handler.
		c.Request = httptest.NewRequest(http.MethodGet, "/v1/axonhub/quota-windows", nil).
			WithContext(contexts.WithAPIKey(t.Context(), key))
		handler.Get(c)
		return recorder
	}
	response := call(key)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
	var data map[string]any
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &data))
	require.Len(t, data, 4)
	require.Len(t, data["windows"], 1)
	require.NotContains(t, response.Body.String(), "secret")
	require.NotContains(t, response.Body.String(), "private-account")
	require.NotContains(t, response.Body.String(), "periodCost")

	key.Profiles.Profiles[0].ChannelIDs = []int{ch.ID, ch.ID + 1}
	require.Equal(t, http.StatusForbidden, call(key).Code)
	key.Profiles.Profiles[0].ChannelIDs = []int{ch.ID}
	key.Edges.Project.Profiles = &objects.ProjectProfiles{ActiveProfile: "deny", Profiles: []objects.ProjectProfile{{Name: "deny", ChannelIDs: []int{ch.ID + 1}}}}
	require.Equal(t, http.StatusForbidden, call(key).Code)
	key.Edges.Project.Profiles = nil
	key.Profiles.Profiles[0].ChannelTags = []string{"other"}
	require.Equal(t, http.StatusForbidden, call(key).Code)
	key.Profiles.Profiles[0].ChannelTags = nil
	require.NoError(t, system.UpdateProviderQuotaCollectionSettings(ctx, lo.ToPtr(false), nil))
	require.Equal(t, http.StatusConflict, call(key).Code)
	key.Key = biz.NoAuthAPIKeyValue
	require.Equal(t, http.StatusForbidden, call(key).Code)
	require.Equal(t, http.StatusForbidden, call(nil).Code)
}
