package server

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
)

func TestCodexCLICompatibility(t *testing.T) {
	binary := os.Getenv("PROBE_TEST_CODEX")
	if binary == "" {
		t.Skip("packaged CLI supplied by the hosted compatibility matrix")
	}
	model := os.Getenv("PROBE_TEST_MODEL")
	require.NotEmpty(t, model)
	h := newCodexRouteHarness(t)
	var turns, searches, whoami, catalogs, legacyRequests, resumedRequests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer upstream", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/models"):
			catalogs.Add(1)
			_, _ = fmt.Fprintf(w, `{"models":[{"slug":%q,"display_name":%q,"supported_reasoning_levels":[],"shell_type":"disabled","visibility":"list","supported_in_api":true,"priority":0,"base_instructions":"Use the provided tools.","support_verbosity":true,"truncation_policy":{"mode":"tokens","limit":12000},"experimental_supported_tools":[],"input_modalities":["text","image"],"tool_mode":"direct"}]}`, model, model)
		case strings.HasSuffix(r.URL.Path, "/alpha/search"):
			searches.Add(1)
			body, err := io.ReadAll(r.Body)
			assert.NoError(t, err)
			assert.Equal(t, "gateway-fixture", gjson.GetBytes(body, "commands.search_query.0.q").String())
			_, _ = io.WriteString(w, `{"output":"gateway-search-receipt","results":[]}`)
		case strings.HasSuffix(r.URL.Path, "/responses"):
			body, err := io.ReadAll(r.Body)
			assert.NoError(t, err)
			assert.Equal(t, model, gjson.GetBytes(body, "model").String())
			n := turns.Add(1)
			var item map[string]any
			switch n {
			case 1:
				item = map[string]any{"id": "fc_gateway", "type": "function_call", "status": "completed", "call_id": "call_gateway", "namespace": "web", "name": "run", "arguments": `{"search_query":[{"q":"gateway-fixture"}]}`}
			case 2, 3, 4, 5:
				if n == 2 {
					assert.Contains(t, string(body), "gateway-search-receipt")
				} else if n == 3 {
					assert.Contains(t, string(body), "continue-gateway-fixture")
					assert.Contains(t, string(body), "gateway-answer-2")
				} else if n == 5 {
					assert.Contains(t, string(body), "legacy-seed")
					assert.Contains(t, string(body), "gateway-answer-4")
					assert.Contains(t, string(body), "resume-after-migration")
				}
				item = map[string]any{"id": fmt.Sprintf("msg_gateway_%d", n), "type": "message", "role": "assistant", "status": "completed", "phase": "final_answer", "content": []any{map[string]any{"type": "output_text", "text": fmt.Sprintf("gateway-answer-%d", n), "annotations": []any{}}}}
			default:
				http.Error(w, "unexpected inference", http.StatusBadRequest)
				return
			}
			id := fmt.Sprintf("resp_gateway_%d", n)
			w.Header().Set("Content-Type", "text/event-stream")
			events := []map[string]any{
				{"type": "response.created", "response": map[string]any{"id": id, "object": "response", "model": model, "status": "in_progress", "output": []any{}}},
				{"type": "response.output_item.done", "output_index": 0, "item": item},
				{"type": "response.completed", "response": map[string]any{"id": id, "object": "response", "model": model, "status": "completed", "output": []any{item}, "usage": map[string]int{"input_tokens": 10, "output_tokens": 3, "total_tokens": 13}}},
			}
			for i, e := range events {
				e["sequence_number"] = i
				data, err := json.Marshal(e)
				assert.NoError(t, err)
				_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e["type"], data)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstream.Close)
	h.source.Update().SetBaseURL(upstream.URL).SetSupportedModels([]string{model}).SetDefaultTestModel(model).
		SetEndpoints([]objects.ChannelEndpoint{{APIFormat: llm.APIFormatOpenAIResponse.String(), Transport: objects.ChannelEndpointTransportHTTP}, {APIFormat: llm.APIFormatOpenAISearch.String()}}).SaveX(h.ctx)
	ch, err := h.channels.GetChannel(h.ctx, h.source.ID)
	require.NoError(t, err)
	h.channels.SetEnabledChannelsForTest([]*biz.Channel{ch})
	require.NoError(t, h.system.SetCodexCompatibilitySettings(h.ctx, biz.CodexCompatibilitySettings{Enabled: true, ChannelID: &h.source.ID}))
	gateway := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/codex/v1/user-auth-credential/whoami":
			whoami.Add(1)
		case "/v1/responses":
			legacyRequests.Add(1)
		case "/codex/responses":
			if legacyRequests.Load() > 0 {
				resumedRequests.Add(1)
			}
		}
		h.router.ServeHTTP(w, r)
	}))
	t.Cleanup(gateway.Close)
	base := gateway.URL + "/codex"
	home, cwd := t.TempDir(), t.TempDir()
	certFile := filepath.Join(home, "gateway.pem")
	require.NoError(t, os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: gateway.Certificate().Raw}), 0o600))
	auth, err := json.Marshal(map[string]string{"personal_access_token": h.key.Key})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(home, "auth.json"), auth, 0o600))
	config := fmt.Sprintf("model_provider=\"openai\"\nmodel=%q\nopenai_base_url=%q\nchatgpt_base_url=%q\nweb_search=\"live\"\ncli_auth_credentials_store=\"file\"\n[analytics]\nenabled=false\n[features]\nstandalone_web_search=true\nplugins=false\napps=false\n", model, base, base)
	require.NoError(t, os.WriteFile(filepath.Join(home, "config.toml"), []byte(config), 0o600))
	run := func(args ...string) string {
		ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, binary, args...)
		cmd.Dir = cwd
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "CODEX_HOME=" + home, "CODEX_AUTHAPI_BASE_URL=" + base, "NO_PROXY=127.0.0.1,localhost", "HTTPS_PROXY=http://127.0.0.1:1", "HTTP_PROXY=http://127.0.0.1:1"}
		cmd.Env = append(cmd.Env, "CODEX_CA_CERTIFICATE="+certFile)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
		return string(out)
	}
	first := run("exec", "--skip-git-repo-check", "--json", "search-gateway-fixture")
	require.Contains(t, first, "gateway-answer-2")
	threadID := func(output string) string {
		for line := range strings.SplitSeq(output, "\n") {
			if gjson.Get(line, "type").String() == "thread.started" {
				return gjson.Get(line, "thread_id").String()
			}
		}
		t.Fatal("Codex did not report a thread ID")
		return ""
	}
	require.Contains(t, run("exec", "resume", "--skip-git-repo-check", "--json", threadID(first), "continue-gateway-fixture"), "gateway-answer-3")
	require.Positive(t, whoami.Load())
	require.Positive(t, catalogs.Load())
	require.EqualValues(t, 1, searches.Load())
	require.EqualValues(t, 3, turns.Load())

	legacyConfig := fmt.Sprintf("model=%q\nmodel_provider=\"OpenAI\"\n[model_providers.OpenAI]\nname=\"OpenAI\"\nbase_url=%q\nwire_api=\"responses\"\nrequires_openai_auth=true\n", model, gateway.URL+"/v1")
	require.NoError(t, os.WriteFile(filepath.Join(home, "config.toml"), []byte(legacyConfig), 0o600))
	legacyAuth, err := json.Marshal(map[string]string{"auth_mode": "apikey", "OPENAI_API_KEY": h.key.Key})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(home, "auth.json"), legacyAuth, 0o600))
	seed := run("exec", "--skip-git-repo-check", "--json", "legacy-seed")
	require.Contains(t, seed, "gateway-answer-4")
	require.Positive(t, legacyRequests.Load())
	require.NoError(t, os.WriteFile(filepath.Join(home, "config.toml"), []byte(config), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(home, "auth.json"), auth, 0o600))
	require.Contains(t, run("exec", "resume", "--skip-git-repo-check", "--json", threadID(seed), "resume-after-migration"), "gateway-answer-5")
	require.Positive(t, resumedRequests.Load())
	require.EqualValues(t, 5, turns.Load())
}
