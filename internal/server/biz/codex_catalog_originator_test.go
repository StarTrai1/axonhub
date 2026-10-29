package biz

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/llm/transformer/openai/codex"
)

func TestCodexCatalog_ConcurrentOriginatorsRemainIsolated(t *testing.T) {
	// Hold the first catalog in flight while a different originator asks for
	// the same source/version. Joining that flight would return its tool catalog.
	entered := make(chan string, 2)
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		originator := r.Header.Get("Originator")
		entered <- originator
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"models": []any{map[string]any{"slug": "gpt-6.1-terra", "originator_extension": originator}}})
	}))
	defer upstream.Close()
	db := enttest.NewEntClient(t, "sqlite3", "file:originators?mode=memory&_fk=0")
	defer db.Close()
	ctx := authz.WithTestBypass(ent.NewContext(t.Context(), db))
	source := db.Channel.Create().SetName("catalog").SetType(channel.TypeCodex).SetStatus(channel.StatusDisabled).SetBaseURL(upstream.URL).SetDefaultTestModel("test").SetSupportedModels([]string{"test"}).SetCredentials(objects.ChannelCredentials{APIKey: "synthetic"}).SaveX(ctx)
	svc := NewChannelServiceForTest(db)
	defer svc.Stop()
	defer close(release)
	type result struct {
		originator string
		catalog    *CodexCatalog
		err        error
	}
	results := make(chan result, 2)
	for _, originator := range []string{"codex_work_web", "chatgpt_cca"} {
		go func() {
			defer func() {
				if p := recover(); p != nil {
					t.Errorf("catalog worker panic: %v", p)
				}
			}()
			catalog, err := svc.FetchCodexCatalogForOriginator(ctx, source.ID, "0.165.0", originator)
			results <- result{originator, catalog, err}
		}()
		select {
		case got := <-entered:
			require.Equal(t, originator, got)
		case <-time.After(5 * time.Second):
			t.Fatal("originator incorrectly joined another catalog request")
		}
	}
	// Send two permits without closing the cleanup-owned channel.
	for range 2 {
		release <- struct{}{}
	}
	for range 2 {
		got := <-results
		require.NoError(t, got.err)
		raw, err := got.catalog.Intersect([]string{"gpt-6.1-terra"})
		require.NoError(t, err)
		require.Contains(t, string(raw), got.originator)
	}
}

func TestCodexCatalog_AbsentOriginatorKeepsDefault(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, codex.AxonHubOriginator, r.Header.Get("Originator"))
		_, _ = w.Write([]byte(`{"models":[]}`))
	}))
	defer upstream.Close()
	db := enttest.NewEntClient(t, "sqlite3", "file:originator-default?mode=memory&_fk=0")
	defer db.Close()
	ctx := authz.WithTestBypass(ent.NewContext(t.Context(), db))
	source := db.Channel.Create().SetName("catalog").SetType(channel.TypeCodex).SetBaseURL(upstream.URL).SetDefaultTestModel("test").SetSupportedModels([]string{"test"}).SetCredentials(objects.ChannelCredentials{APIKey: "synthetic"}).SaveX(ctx)
	svc := NewChannelServiceForTest(db)
	defer svc.Stop()
	_, err := svc.FetchCodexCatalog(ctx, source.ID, "0.165.0")
	require.NoError(t, err)
}
