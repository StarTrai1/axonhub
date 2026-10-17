package api

import (
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/objects"
)

func TestCodexSearchOnlyCrossModelRouting(t *testing.T) {
	models := []string{"gpt-6-astra", "gpt-6.1-sol", "gpt-6-sol", "gpt-6-luna", "gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna"}
	for _, actualModel := range models {
		t.Run(actualModel, func(t *testing.T) {
			var searches, generations atomic.Int32
			upstream := responsesIntegrationServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/alpha/search" {
					generations.Add(1)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				searches.Add(1)
				body, err := readRequestBody(r)
				require.NoError(t, err)
				require.Equal(t, actualModel, gjson.GetBytes(body, "model").String())
				require.Equal(t, "lookup-canary", gjson.GetBytes(body, "query").String())
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"results":[{"title":"synthetic search result"}]}`))
			}))
			client, handlers, project := newResponsesIntegrationFixture(t, upstream.URL, func(create *ent.ChannelCreate) {
				create.SetType(channel.TypeCodex).SetSupportedModels([]string{"synthetic-model", actualModel}).SetDefaultTestModel(actualModel).
					SetSettings(&objects.ChannelSettings{CodexSearchOnly: true, CodexSearchModel: actualModel}).
					SetPolicies(objects.ChannelPolicies{WebSearch: objects.WebSearchPolicyMCPOnly})
			})
			gateway := responsesIntegrationGateway(t, client, handlers, project)
			for _, requestedModel := range models {
				status, body := responsesIntegrationPost(t, gateway, "/v1/alpha/search", fmt.Sprintf(`{"id":"search-test","model":%q,"query":"lookup-canary"}`, requestedModel))
				require.Equal(t, http.StatusOK, status, string(body))
				require.Contains(t, string(body), "synthetic search result")
			}
			status, body := responsesIntegrationPost(t, gateway, "/v1/responses", fmt.Sprintf(`{"model":%q,"input":"generation must be excluded"}`, actualModel))
			require.GreaterOrEqual(t, status, http.StatusBadRequest, string(body))
			conn := responsesIntegrationDial(t, gateway)
			response := responsesIntegrationTurn(t, conn, fmt.Sprintf(`{"type":"response.create","model":%q,"input":"WS generation must be excluded"}`, actualModel))
			require.Contains(t, []string{"error", "response.failed"}, gjson.GetBytes(response, "type").String())
			require.EqualValues(t, len(models), searches.Load())
			require.Zero(t, generations.Load(), "search-only channels must never dispatch a Responses request")
		})
	}
}

func TestCodexSearchOnlyDoesNotFallbackToResponses(t *testing.T) {
	var searches, generations atomic.Int32
	upstream := responsesIntegrationServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/alpha/search" {
			searches.Add(1)
		} else {
			generations.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"message":"Unknown endpoint /alpha/search","type":"invalid_request_error"}}`))
	}))
	client, handlers, project := newResponsesIntegrationFixture(t, upstream.URL, func(create *ent.ChannelCreate) {
		create.SetType(channel.TypeCodex).SetSettings(&objects.ChannelSettings{CodexSearchOnly: true})
	})
	gateway := responsesIntegrationGateway(t, client, handlers, project)
	status, body := responsesIntegrationPost(t, gateway, "/v1/alpha/search", `{"id":"missing-search","model":"gpt-6.1-sol","query":"lookup-canary"}`)
	require.GreaterOrEqual(t, status, http.StatusBadRequest, string(body))
	require.Positive(t, searches.Load())
	require.Zero(t, generations.Load())
}

func TestCodexSearchOnlyFollowsRequestModel(t *testing.T) {
	var calls atomic.Int32
	upstream := responsesIntegrationServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/alpha/search", r.URL.Path)
		body, err := readRequestBody(r)
		require.NoError(t, err)
		require.Equal(t, gjson.GetBytes(body, "expected_model").String(), gjson.GetBytes(body, "model").String())
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	client, handlers, project := newResponsesIntegrationFixture(t, upstream.URL, func(create *ent.ChannelCreate) {
		create.SetType(channel.TypeCodex).SetSupportedModels([]string{"gpt-5.5", "unrelated-model"}).SetDefaultTestModel("gpt-5.5").
			SetSettings(&objects.ChannelSettings{CodexSearchOnly: true})
	})
	gateway := responsesIntegrationGateway(t, client, handlers, project)
	for _, model := range []string{"gpt-5.6-sol", "gpt-6-astra", "gpt-6.1-sol", "gpt-6-luna"} {
		status, body := responsesIntegrationPost(t, gateway, "/v1/alpha/search", fmt.Sprintf(`{"model":%q,"expected_model":%q}`, model, model))
		require.Equal(t, http.StatusOK, status, string(body))
	}
	status, body := responsesIntegrationPost(t, gateway, "/v1/alpha/search", `{"model":"gpt-5.5"}`)
	require.GreaterOrEqual(t, status, http.StatusBadRequest, string(body))
	require.EqualValues(t, 4, calls.Load())
}

func TestCodexNativeSearchDoesNotFallbackToResponses(t *testing.T) {
	var searches, generations atomic.Int32
	upstream := responsesIntegrationServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/alpha/search" {
			searches.Add(1)
		} else {
			generations.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"message":"Unknown endpoint /alpha/search","type":"invalid_request_error"}}`))
	}))
	client, handlers, project := newResponsesIntegrationFixture(t, upstream.URL, func(create *ent.ChannelCreate) {
		create.SetType(channel.TypeCodex).SetSupportedModels([]string{"synthetic-model", "gpt-6.1-sol"}).
			SetPolicies(objects.ChannelPolicies{WebSearch: objects.WebSearchPolicy("auto")})
	})
	gateway := responsesIntegrationGateway(t, client, handlers, project)
	status, body := responsesIntegrationPost(t, gateway, "/v1/alpha/search", `{"model":"gpt-6.1-sol"}`)
	require.GreaterOrEqual(t, status, http.StatusBadRequest, string(body))
	require.Positive(t, searches.Load())
	require.Zero(t, generations.Load())
}
