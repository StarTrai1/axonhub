package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/apikey"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/ent/project"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/pkg/xcache"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/internal/server/middleware"
	"github.com/looplj/axonhub/internal/server/orchestrator"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
)

type responsesWebSocketAuthFixture struct {
	ctx      context.Context
	auth     *biz.AuthService
	keys     *biz.APIKeyService
	projects *biz.ProjectService
	key      *ent.APIKey
}

func newResponsesWebSocketAuthFixture(t *testing.T) *responsesWebSocketAuthFixture {
	t.Helper()
	client := enttest.NewEntClient(t, "sqlite3", "file:ws-auth-"+uuid.NewString()+"?mode=memory&cache=shared&_fk=1")
	ctx := authz.WithTestBypass(ent.NewContext(t.Context(), client))
	owner, err := client.User.Create().SetEmail("ws-auth@example.com").SetPassword("test-only").
		SetFirstName("WS").SetLastName("Auth").Save(ctx)
	require.NoError(t, err)
	proj, err := client.Project.Create().SetName("WebSocket auth test").SetStatus(project.StatusActive).Save(ctx)
	require.NoError(t, err)
	key, err := client.APIKey.Create().SetKey("ah-websocket-test-only").SetName("WebSocket auth test").
		SetUser(owner).SetProject(proj).SetType(apikey.TypeServiceAccount).SetStatus(apikey.StatusEnabled).
		SetProfiles(&objects.APIKeyProfiles{ActiveProfile: "original", Profiles: []objects.APIKeyProfile{{Name: "original"}}}).Save(ctx)
	require.NoError(t, err)
	cache := xcache.Config{Mode: xcache.ModeMemory}
	projects := biz.NewProjectService(biz.ProjectServiceParams{Ent: client, CacheConfig: cache})
	keys := biz.NewAPIKeyService(biz.APIKeyServiceParams{Ent: client, CacheConfig: cache, ProjectService: projects, KeyPrefix: "ah"})
	t.Cleanup(keys.Stop)
	return &responsesWebSocketAuthFixture{
		ctx: ctx, auth: &biz.AuthService{APIKeyService: keys, AllowNoAuth: true},
		keys: keys, projects: projects, key: key,
	}
}

func (f *responsesWebSocketAuthFixture) connect(t *testing.T, process responsesWebSocketProcessFunc) *websocket.Conn {
	t.Helper()
	router := gin.New()
	require.NoError(t, router.SetTrustedProxies(nil))
	router.Use(middleware.WithAPIKeyAuth(f.auth))
	router.GET("/v1/responses", func(c *gin.Context) {
		serveResponsesWebSocket(c, 5*time.Second, process, nil)
	})
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	conn := dialResponsesWebSocket(t, server.URL, http.Header{"Authorization": []string{"Bearer " + f.key.Key}})
	t.Cleanup(func() { _ = conn.Close() })
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
	return conn
}

func responsesWebSocketAuthResult(id string) orchestrator.ChatCompletionResult {
	return orchestrator.ChatCompletionResult{ChatCompletionStream: streams.SliceStream([]*httpclient.StreamEvent{
		{Type: "response.completed", Data: []byte(fmt.Sprintf(`{"type":"response.completed","response":{"id":%q,"status":"completed"}}`, "resp_"+id))},
	})}
}

func TestResponsesWebSocketAuthRechecksQueuedRequests(t *testing.T) {
	fixture := newResponsesWebSocketAuthFixture(t)
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	var calls atomic.Int32
	conn := fixture.connect(t, func(ctx context.Context, _ *httpclient.Request) (orchestrator.ChatCompletionResult, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		select {
		case <-release:
		case <-ctx.Done():
			return orchestrator.ChatCompletionResult{}, ctx.Err()
		}
		return responsesWebSocketAuthResult("active"), nil
	})
	create := []byte(`{"type":"response.create","model":"test","input":"hello","stream_id":"same"}`)
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, create))
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("first request did not start")
	}
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, create))
	_, err := fixture.keys.UpdateAPIKeyStatus(fixture.ctx, fixture.key.ID, apikey.StatusDisabled)
	require.NoError(t, err)
	releaseOnce.Do(func() { close(release) })
	_, first, err := conn.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, "response.completed", gjson.GetBytes(first, "type").String())
	_, rejected, err := conn.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, "error", gjson.GetBytes(rejected, "type").String())
	require.Equal(t, int64(http.StatusUnauthorized), gjson.GetBytes(rejected, "status").Int())
	require.Equal(t, "same", gjson.GetBytes(rejected, "stream_id").String())
	require.Equal(t, int32(1), calls.Load(), "revocation must not fall back to no-auth or start queued work")

	// A warm-up also counts as a new authenticated operation.
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"test","generate":false}`)))
	_, warmup, err := conn.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, int64(http.StatusUnauthorized), gjson.GetBytes(warmup, "status").Int())
}

func TestResponsesWebSocketAuthRefreshesProfilesWithoutSharingRequestState(t *testing.T) {
	fixture := newResponsesWebSocketAuthFixture(t)
	started := make(chan context.Context, 2)
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	conn := fixture.connect(t, func(ctx context.Context, request *httpclient.Request) (orchestrator.ChatCompletionResult, error) {
		input := gjson.GetBytes(request.Body, "input").String()
		ctx = contexts.WithChannelAPIKey(ctx, "channel-"+input)
		started <- ctx
		select {
		case <-release:
		case <-ctx.Done():
			return orchestrator.ChatCompletionResult{}, ctx.Err()
		}
		return responsesWebSocketAuthResult(input), nil
	})
	receive := func() context.Context {
		select {
		case ctx := <-started:
			return ctx
		case <-time.After(5 * time.Second):
			t.Fatal("request did not start")
			return nil
		}
	}
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"test","input":"first","stream_id":"first"}`)))
	first := receive()
	_, err := fixture.keys.UpdateAPIKeyProfiles(fixture.ctx, fixture.key.ID, objects.APIKeyProfiles{
		ActiveProfile: "changed", Profiles: []objects.APIKeyProfile{{Name: "changed", ModelIDs: []string{"restricted-model"}}},
	})
	require.NoError(t, err)
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"test","input":"second","stream_id":"second"}`)))
	second := receive()
	firstKey, ok := contexts.GetAPIKey(first)
	require.True(t, ok)
	secondKey, ok := contexts.GetAPIKey(second)
	require.True(t, ok)
	require.Equal(t, "original", firstKey.Profiles.ActiveProfile)
	require.Equal(t, "changed", secondKey.Profiles.ActiveProfile)
	require.NotNil(t, secondKey.GetActiveProfile())
	require.Equal(t, []string{"restricted-model"}, secondKey.GetActiveProfile().ModelIDs)
	firstChannel, _ := contexts.GetChannelAPIKey(first)
	secondChannel, _ := contexts.GetChannelAPIKey(second)
	require.Equal(t, "channel-first", firstChannel)
	require.Equal(t, "channel-second", secondChannel)
	releaseOnce.Do(func() { close(release) })
	for range 2 {
		_, event, err := conn.ReadMessage()
		require.NoError(t, err)
		require.Equal(t, "response.completed", gjson.GetBytes(event, "type").String())
	}
}

func TestResponsesWebSocketAuthRechecksCredentialProjectAndIP(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		change func(*responsesWebSocketAuthFixture) error
		status int
	}{
		{"key rotated", func(f *responsesWebSocketAuthFixture) error {
			_, err := f.keys.RotateAPIKey(f.ctx, f.key.ID)
			return err
		}, http.StatusUnauthorized},
		{"project disabled", func(f *responsesWebSocketAuthFixture) error {
			_, err := f.projects.UpdateProjectStatus(f.ctx, f.key.ProjectID, project.StatusArchived)
			return err
		}, http.StatusUnauthorized},
		{"IP denied", func(f *responsesWebSocketAuthFixture) error {
			_, err := f.keys.UpdateAPIKey(f.ctx, f.key.ID, ent.UpdateAPIKeyInput{AllowedIps: []string{"192.0.2.10"}})
			return err
		}, http.StatusForbidden},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			fixture := newResponsesWebSocketAuthFixture(t)
			var calls atomic.Int32
			conn := fixture.connect(t, func(context.Context, *httpclient.Request) (orchestrator.ChatCompletionResult, error) {
				calls.Add(1)
				return responsesWebSocketAuthResult("initial"), nil
			})
			create := []byte(`{"type":"response.create","model":"test","input":"hello"}`)
			require.NoError(t, conn.WriteMessage(websocket.TextMessage, create))
			_, _, err := conn.ReadMessage()
			require.NoError(t, err)
			require.NoError(t, scenario.change(fixture))
			require.NoError(t, conn.WriteMessage(websocket.TextMessage, create))
			_, rejected, err := conn.ReadMessage()
			require.NoError(t, err)
			require.Equal(t, int64(scenario.status), gjson.GetBytes(rejected, "status").Int())
			require.Equal(t, int32(1), calls.Load())
		})
	}
}
