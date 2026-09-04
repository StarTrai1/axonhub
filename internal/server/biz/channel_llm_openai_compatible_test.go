package biz

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/streams"
	"github.com/looplj/axonhub/llm/transformer"
	"github.com/looplj/axonhub/llm/transformer/openai"
	"github.com/looplj/axonhub/llm/transformer/openai/codex"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

func TestOpenAICompatibleChannel_BuildChannelWithOutbounds(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx := authz.WithTestBypass(context.Background())

	entChannel := client.Channel.Create().
		SetName("Vercel Multi Endpoint Channel").
		SetType(channel.TypeVercel).
		SetBaseURL("https://ai-gateway.vercel.sh/v1").
		SetCredentials(objects.ChannelCredentials{APIKey: "test-key"}).
		SetSupportedModels([]string{"gpt-4o-mini"}).
		SetDefaultTestModel("gpt-4o-mini").
		SaveX(ctx)

	channelSvc := NewChannelServiceForTest(client)

	built, err := channelSvc.buildChannelWithOutbounds(entChannel)
	require.NoError(t, err)
	require.NotNil(t, built)
	require.NotNil(t, built.Outbound)
	require.Len(t, built.Outbounds, 7)

	require.Equal(t, llm.APIFormatOpenAIChatCompletion, built.Outbound.APIFormat())

	embeddingOutbound, err := BuildOutboundByAPIFormat(built, llm.APIFormatOpenAIEmbedding.String())
	require.NoError(t, err)
	require.NotNil(t, embeddingOutbound)
	_, ok := embeddingOutbound.(*openai.OutboundTransformer)
	require.True(t, ok)

	moderationOutbound, err := BuildOutboundByAPIFormat(built, llm.APIFormatOpenAIModeration.String())
	require.NoError(t, err)
	require.NotNil(t, moderationOutbound)
	_, ok = moderationOutbound.(*openai.OutboundTransformer)
	require.True(t, ok)

	imageOutbound, err := BuildOutboundByAPIFormat(built, llm.APIFormatOpenAIImageGeneration.String())
	require.NoError(t, err)
	require.NotNil(t, imageOutbound)
	_, ok = imageOutbound.(*openai.OutboundTransformer)
	require.True(t, ok)

	videoOutbound, err := BuildOutboundByAPIFormat(built, llm.APIFormatOpenAIVideo.String())
	require.NoError(t, err)
	require.NotNil(t, videoOutbound)
	_, ok = videoOutbound.(*openai.OutboundTransformer)
	require.True(t, ok)
}

func TestAtlasCloudChannel_BuildChannelWithOutbounds(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx := authz.WithTestBypass(context.Background())

	entChannel := client.Channel.Create().
		SetName("AtlasCloud Channel").
		SetType(channel.TypeAtlascloud).
		SetBaseURL("https://api.atlascloud.ai/v1").
		SetCredentials(objects.ChannelCredentials{APIKey: "test-key"}).
		SetSupportedModels([]string{"deepseek-v3"}).
		SetDefaultTestModel("deepseek-v3").
		SaveX(ctx)

	channelSvc := NewChannelServiceForTest(client)

	built, err := channelSvc.buildChannelWithOutbounds(entChannel)
	require.NoError(t, err)
	require.NotNil(t, built)
	require.NotNil(t, built.Outbound)
	require.Len(t, built.Outbounds, 7)

	require.Equal(t, llm.APIFormatOpenAIChatCompletion, built.Outbound.APIFormat())

	embeddingOutbound, err := BuildOutboundByAPIFormat(built, llm.APIFormatOpenAIEmbedding.String())
	require.NoError(t, err)
	require.NotNil(t, embeddingOutbound)
	_, ok := embeddingOutbound.(*openai.OutboundTransformer)
	require.True(t, ok)

	moderationOutbound, err := BuildOutboundByAPIFormat(built, llm.APIFormatOpenAIModeration.String())
	require.NoError(t, err)
	require.NotNil(t, moderationOutbound)
	_, ok = moderationOutbound.(*openai.OutboundTransformer)
	require.True(t, ok)
}

func TestOpenAIResponsesEndpoint_InheritsWebSocketTransportFromBaseURL(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx := authz.WithTestBypass(context.Background())

	entChannel := client.Channel.Create().
		SetName("Responses WebSocket Channel").
		SetType(channel.TypeOpenaiResponses).
		SetBaseURL("wss://api.openai.com/v1").
		SetCredentials(objects.ChannelCredentials{APIKey: "test-key"}).
		SetSupportedModels([]string{"gpt-5"}).
		SetDefaultTestModel("gpt-5").
		SetEndpoints([]objects.ChannelEndpoint{{
			APIFormat: llm.APIFormatOpenAIResponse.String(),
			Path:      "/custom/responses",
		}}).
		SaveX(ctx)

	channelSvc := NewChannelServiceForTest(client)

	built, err := channelSvc.buildChannelWithOutbounds(entChannel)
	require.NoError(t, err)

	outbound, err := BuildOutboundByAPIFormat(built, llm.APIFormatOpenAIResponse.String())
	require.NoError(t, err)
	custom, ok := outbound.(pipeline.ChannelCustomizedExecutor)
	require.True(t, ok)

	executor := custom.CustomizeExecutor(nil)
	_, ok = executor.(*responses.WebSocketExecutor)
	require.True(t, ok)

	searchOutbound, err := BuildOutboundByAPIFormat(built, llm.APIFormatOpenAISearch.String())
	require.NoError(t, err)
	_, ok = searchOutbound.(*responses.SearchOutboundTransformer)
	require.True(t, ok)
	_, customized := searchOutbound.(pipeline.ChannelCustomizedExecutor)
	require.True(t, customized)

	searchReq, err := searchOutbound.TransformRequest(t.Context(), &llm.Request{
		Model:       "gpt-5.6-sol",
		RequestType: llm.RequestTypeSearch,
		APIFormat:   llm.APIFormatOpenAISearch,
		Search: &llm.SearchRequest{
			Raw: []byte(`{"id":"search-1","model":"gpt-5.6-sol"}`),
		},
	})
	require.NoError(t, err)
	require.Equal(t, "https://api.openai.com/v1/alpha/search", searchReq.URL)

	probe := &searchFallbackProbeExecutor{}
	fallbackResponse, err := searchOutbound.(pipeline.ChannelCustomizedExecutor).CustomizeExecutor(probe).Do(t.Context(), searchReq)
	require.NoError(t, err)
	require.Equal(t, "search fallback", gjson.GetBytes(fallbackResponse.Body, "output").String())
	require.Len(t, probe.requests, 2)
	require.Equal(t, "https://api.openai.com/v1/alpha/search", probe.requests[0].URL)
	require.Equal(t, "https://api.openai.com/v1/custom/responses", probe.requests[1].URL)
	require.Equal(t, "gpt-5.6-sol", gjson.GetBytes(probe.requests[1].Body, "model").String())
	require.True(t, gjson.GetBytes(probe.requests[1].Body, "stream").Bool())
}

func TestOpenAIResponsesCompactEndpoint_RejectsInheritedWebSocketTransport(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:compact_websocket?mode=memory&_fk=0")
	defer client.Close()

	ctx := authz.WithTestBypass(context.Background())
	entChannel := client.Channel.Create().
		SetName("Responses Compact WebSocket Channel").
		SetType(channel.TypeOpenaiResponses).
		SetBaseURL("wss://api.openai.com/v1").
		SetCredentials(objects.ChannelCredentials{APIKey: "test-key"}).
		SetSupportedModels([]string{"gpt-5"}).
		SetDefaultTestModel("gpt-5").
		SetEndpoints([]objects.ChannelEndpoint{{
			APIFormat: llm.APIFormatOpenAIResponseCompact.String(),
		}}).
		SaveX(ctx)

	_, err := NewChannelServiceForTest(client).buildChannelWithOutbounds(entChannel)
	require.ErrorContains(t, err, "websocket transport only supports api_format \"openai/responses\"")
}

func TestCodexOAuthWebSocketEndpointBuildsWithoutAPIKey(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx := authz.WithTestBypass(context.Background())

	entChannel := client.Channel.Create().
		SetName("Codex OAuth WebSocket Channel").
		SetType(channel.TypeCodex).
		SetBaseURL("wss://chatgpt.com/backend-api/codex#").
		SetCredentials(objects.ChannelCredentials{
			OAuth: &objects.OAuthCredentials{
				AccessToken:  "access-token",
				RefreshToken: "refresh-token",
				ExpiresAt:    time.Now().Add(time.Hour),
			},
		}).
		SetSupportedModels([]string{"gpt-5.5"}).
		SetDefaultTestModel("gpt-5.5").
		SetEndpoints([]objects.ChannelEndpoint{{
			APIFormat: llm.APIFormatOpenAIResponse.String(),
			Transport: objects.ChannelEndpointTransportWebSocket,
		}}).
		SaveX(ctx)

	channelSvc := NewChannelServiceForTest(client)

	built, err := channelSvc.buildChannelWithOutbounds(entChannel)
	require.NoError(t, err)

	primary, ok := built.Outbound.(*codex.OutboundTransformer)
	require.True(t, ok)
	require.NotNil(t, primary.TokenProvider())

	outbound, err := BuildOutboundByAPIFormat(built, llm.APIFormatOpenAIResponse.String())
	require.NoError(t, err)
	override, ok := outbound.(*codex.OutboundTransformer)
	require.True(t, ok)
	require.True(t, primary.TokenProvider() == override.TokenProvider())

	custom, ok := outbound.(pipeline.ChannelCustomizedExecutor)
	require.True(t, ok)
	require.NotNil(t, custom.CustomizeExecutor(nil))

	searchOutbound, err := BuildOutboundByAPIFormat(built, llm.APIFormatOpenAISearch.String())
	require.NoError(t, err)
	_, ok = searchOutbound.(*codex.SearchOutboundTransformer)
	require.True(t, ok)
	_, customized := searchOutbound.(pipeline.ChannelCustomizedExecutor)
	require.True(t, customized)

	searchReq, err := searchOutbound.TransformRequest(t.Context(), &llm.Request{
		Model:       "gpt-5.6-sol",
		RequestType: llm.RequestTypeSearch,
		APIFormat:   llm.APIFormatOpenAISearch,
		Search: &llm.SearchRequest{
			Raw: []byte(`{"id":"search-1","model":"gpt-5.6-sol"}`),
		},
	})
	require.NoError(t, err)
	require.Equal(t, "https://chatgpt.com/backend-api/codex/alpha/search", searchReq.URL)
	require.Equal(t, "access-token", searchReq.Auth.APIKey)
}

func TestCodexAPIKeyChannelBuildsStandaloneSearchOutbound(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx := authz.WithTestBypass(context.Background())
	entChannel := client.Channel.Create().
		SetName("Third-Party Codex Channel").
		SetType(channel.TypeCodex).
		SetBaseURL("https://sub.example.test/v1").
		SetCredentials(objects.ChannelCredentials{APIKey: "third-party-key"}).
		SetSupportedModels([]string{"gpt-5.6-sol"}).
		SetDefaultTestModel("gpt-5.6-sol").
		SaveX(ctx)

	built, err := NewChannelServiceForTest(client).buildChannelWithOutbounds(entChannel)
	require.NoError(t, err)

	searchOutbound, err := BuildOutboundByAPIFormat(built, llm.APIFormatOpenAISearch.String())
	require.NoError(t, err)
	_, ok := searchOutbound.(*codex.SearchOutboundTransformer)
	require.True(t, ok)
	_, customized := searchOutbound.(pipeline.ChannelCustomizedExecutor)
	require.True(t, customized)

	searchReq, err := searchOutbound.TransformRequest(t.Context(), &llm.Request{
		Model:       "gpt-5.6-sol",
		RequestType: llm.RequestTypeSearch,
		APIFormat:   llm.APIFormatOpenAISearch,
		Search: &llm.SearchRequest{
			Raw: []byte(`{"id":"search-1","model":"gpt-5.6-sol"}`),
		},
	})
	require.NoError(t, err)
	require.Equal(t, "https://sub.example.test/v1/alpha/search", searchReq.URL)
	require.Equal(t, "third-party-key", searchReq.Auth.APIKey)
}

type searchFallbackProbeExecutor struct {
	requests []*httpclient.Request
}

func (e *searchFallbackProbeExecutor) Do(
	_ context.Context,
	request *httpclient.Request,
) (*httpclient.Response, error) {
	e.requests = append(e.requests, request)
	if len(e.requests) == 1 {
		return nil, &httpclient.Error{
			Method:     http.MethodPost,
			URL:        request.URL,
			StatusCode: http.StatusNotFound,
			Body:       []byte(`{"error":{"message":"Invalid URL (POST /v1/alpha/search)"}}`),
		}
	}

	return &httpclient.Response{
		StatusCode: http.StatusOK,
		Body: []byte(`{
			"id":"resp-1",
			"model":"gpt-5.5",
			"status":"completed",
			"output":[{"type":"message","content":[{"type":"output_text","text":"search fallback"}]}]
		}`),
		Request: request,
	}, nil
}

func (e *searchFallbackProbeExecutor) DoStream(
	_ context.Context,
	request *httpclient.Request,
) (streams.Stream[*httpclient.StreamEvent], error) {
	e.requests = append(e.requests, request)

	return streams.SliceStream([]*httpclient.StreamEvent{
		{Data: []byte(`{"type":"response.created","response":{"id":"resp-1","object":"response","created_at":1700000000,"model":"gpt-5.6-sol","status":"in_progress","output":[]}}`)},
		{Data: []byte(`{"type":"response.output_item.added","output_index":0,"item":{"id":"msg-1","type":"message","status":"in_progress","role":"assistant","content":[]}}`)},
		{Data: []byte(`{"type":"response.content_part.added","item_id":"msg-1","output_index":0,"content_index":0,"part":{"type":"output_text","text":"","annotations":[]}}`)},
		{Data: []byte(`{"type":"response.output_text.done","item_id":"msg-1","output_index":0,"content_index":0,"text":"search fallback"}`)},
		{Data: []byte(`{"type":"response.output_item.done","output_index":0,"item":{"id":"msg-1","type":"message","status":"completed","role":"assistant","content":[{"type":"output_text","text":"search fallback","annotations":[]}]}}`)},
		{Data: []byte(`{"type":"response.completed","response":{"id":"resp-1","object":"response","created_at":1700000001,"model":"gpt-5.6-sol","status":"completed","output":[]}}`)},
	}), nil
}

func TestCodexAlphaSearchEndpointPreservesCustomPath(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx := authz.WithTestBypass(context.Background())

	entChannel := client.Channel.Create().
		SetName("Codex Custom Alpha Search Channel").
		SetType(channel.TypeCodex).
		SetBaseURL("https://relay.example/backend-api/codex#").
		SetCredentials(objects.ChannelCredentials{
			OAuth: &objects.OAuthCredentials{
				AccessToken:  "access-token",
				RefreshToken: "refresh-token",
				ExpiresAt:    time.Now().Add(time.Hour),
			},
		}).
		SetSupportedModels([]string{"gpt-5.5"}).
		SetDefaultTestModel("gpt-5.5").
		SetEndpoints([]objects.ChannelEndpoint{{
			APIFormat: llm.APIFormatOpenAIAlphaSearch.String(),
			Path:      "/custom/search",
		}}).
		SaveX(ctx)

	channelSvc := NewChannelServiceForTest(client)
	built, err := channelSvc.buildChannelWithOutbounds(entChannel)
	require.NoError(t, err)

	outbound, err := BuildOutboundByAPIFormat(built, llm.APIFormatOpenAIAlphaSearch.String())
	require.NoError(t, err)

	request, err := outbound.TransformRequest(ctx, &llm.Request{
		Model:       "gpt-5.5",
		RequestType: llm.RequestTypeAlphaSearch,
		APIFormat:   llm.APIFormatOpenAIAlphaSearch,
		AlphaSearch: &llm.AlphaSearchRequest{Body: []byte(`{"commands":{"search_query":[]}}`)},
	})
	require.NoError(t, err)
	require.Equal(t, "https://relay.example/backend-api/codex/custom/search", request.URL)
}

type testStoppableOutbound struct {
	stops int
}

func (t *testStoppableOutbound) APIFormat() llm.APIFormat { return llm.APIFormatOpenAIResponse }

func (t *testStoppableOutbound) TransformRequest(context.Context, *llm.Request) (*httpclient.Request, error) {
	return nil, nil
}

func (t *testStoppableOutbound) TransformResponse(context.Context, *httpclient.Response) (*llm.Response, error) {
	return nil, nil
}

func (t *testStoppableOutbound) TransformStream(context.Context, *httpclient.Request, streams.Stream[*httpclient.StreamEvent]) (streams.Stream[*llm.Response], error) {
	return nil, nil
}

func (t *testStoppableOutbound) TransformError(context.Context, *httpclient.Error) *llm.ResponseError {
	return nil
}

func (t *testStoppableOutbound) AggregateStreamChunks(context.Context, *httpclient.Request, []*httpclient.StreamEvent) ([]byte, llm.ResponseMeta, error) {
	return nil, llm.ResponseMeta{}, nil
}

func (t *testStoppableOutbound) Stop() {
	t.stops++
}

func TestStopChannelOutboundsStopsEachOutboundOnce(t *testing.T) {
	primary := &testStoppableOutbound{}
	secondary := &testStoppableOutbound{}

	stopChannelOutbounds(&Channel{
		Outbound: primary,
		Outbounds: map[string]transformer.Outbound{
			llm.APIFormatOpenAIResponse.String():        primary,
			llm.APIFormatOpenAIResponseCompact.String(): secondary,
		},
	})

	require.Equal(t, 1, primary.stops)
	require.Equal(t, 1, secondary.stops)
}

type closeIdleTrackingTransport struct {
	closeIdleCalls int
}

func (*closeIdleTrackingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, nil
}

func (t *closeIdleTrackingTransport) CloseIdleConnections() {
	t.closeIdleCalls++
}

func TestCleanupSwappedChannelsClosesOnlyOwnedHTTPClients(t *testing.T) {
	sharedTransport := &closeIdleTrackingTransport{}
	ownedTransport := &closeIdleTrackingTransport{}
	sharedClient := httpclient.NewHttpClientWithClient(&http.Client{Transport: sharedTransport})
	ownedClient := httpclient.NewHttpClientWithClient(&http.Client{Transport: ownedTransport})
	svc := &ChannelService{httpClient: sharedClient}

	svc.cleanupSwappedChannels([]*Channel{
		{HTTPClient: sharedClient},
		{HTTPClient: ownedClient},
		{},
	})

	require.Zero(t, sharedTransport.closeIdleCalls)
	require.Equal(t, 1, ownedTransport.closeIdleCalls)
}

func TestOnEnabledChannelsSwapDoesNotWaitForOldCleanup(t *testing.T) {
	cleanupStarted := make(chan struct{})
	releaseCleanup := make(chan struct{})
	cleanupDone := make(chan struct{})
	startCount := 0

	svc := &ChannelService{}
	old := &Channel{
		stopTokenProvider: func() {
			close(cleanupStarted)
			<-releaseCleanup
			close(cleanupDone)
		},
	}
	next := &Channel{
		startTokenProvider: func() {
			startCount++
		},
	}

	returned := make(chan struct{})
	go func() {
		svc.onEnabledChannelsSwap([]*Channel{old}, []*Channel{next})
		close(returned)
	}()

	select {
	case <-returned:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("onEnabledChannelsSwap waited for old channel cleanup")
	}

	require.Equal(t, int64(1), svc.GetCacheVersion())
	require.Equal(t, 1, startCount)

	select {
	case <-cleanupStarted:
	case <-time.After(time.Second):
		t.Fatal("old channel cleanup did not start")
	}

	close(releaseCleanup)

	select {
	case <-cleanupDone:
	case <-time.After(time.Second):
		t.Fatal("old channel cleanup did not finish")
	}
}
