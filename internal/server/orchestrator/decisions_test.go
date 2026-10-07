package orchestrator

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/transformer/openai/decisions"
)

func TestDecisionsPipelinePersistsNativeAnswersAndCost(t *testing.T) {
	for _, passThrough := range []bool{false, true} {
		t.Run(fmt.Sprint(passThrough), func(t *testing.T) {
			ctx := authz.WithTestBypass(context.Background())
			client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
			defer client.Close()
			ctx = ent.NewContext(ctx, client)
			project := createTestProject(t, ctx, client)
			ctx = contexts.WithProjectID(ctx, project.ID)
			received := make(chan []byte, 1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/decisions" || r.Header.Get("Authorization") != "Bearer fixture-key" || r.Method != http.MethodPost {
					http.Error(w, "incorrect native endpoint or auth", http.StatusBadRequest)
					return
				}
				body, err := io.ReadAll(r.Body)
				if err != nil {
					http.Error(w, "read body", http.StatusBadRequest)
					return
				}
				received <- body
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"model":"gpt-6-luna","answers":[{"type":"predicate","name":null,"probability":0.8},{"type":"refusal","name":"private","reason":"fixture"}],"usage":{"input_tokens":42,"output_tokens":0,"total_tokens":42},"extra":9007199254740993}`)
			}))
			defer upstream.Close()
			ch, err := client.Channel.Create().SetType(channel.TypeOpenai).SetName("Decisions fixture").
				SetBaseURL(upstream.URL + "/v1").SetCredentials(objects.ChannelCredentials{APIKey: "fixture-key"}).
				SetSupportedModels([]string{"gpt-6-luna"}).SetDefaultTestModel("gpt-6-luna").
				SetSettings(&objects.ChannelSettings{PassThroughBody: lo.ToPtr(passThrough)}).Save(ctx)
			require.NoError(t, err)
			_, err = client.ChannelModelPrice.Create().SetChannelID(ch.ID).SetModelID("gpt-6-luna").SetReferenceID("decisions-fixture").SetPrice(objects.ModelPrice{Items: []objects.ModelPriceItem{
				{ItemCode: objects.PriceItemCodeUsage, Pricing: objects.Pricing{Mode: objects.PricingModeFlatFee, FlatFee: lo.ToPtr(decimal.NewFromInt(99))}},
				{ItemCode: objects.PriceItemCodeDecisionsInputTokens, Pricing: objects.Pricing{Mode: objects.PricingModeUsagePerUnit, UsagePerUnit: lo.ToPtr(decimal.RequireFromString("0.10"))}},
			}}).Save(ctx)
			require.NoError(t, err)
			channelService, requestService, systemService, usageLogService := setupTestServices(t, client)
			require.Same(t, channelService, usageLogService.ChannelService)
			built, err := channelService.GetChannel(ctx, ch.ID)
			require.NoError(t, err)
			channelService.PreloadModelPricesForTest(ctx, built)
			channelService.SetEnabledChannelsForTest([]*biz.Channel{built})
			require.NoError(t, systemService.SetInjectUsageCostEnabled(ctx, true))
			require.NoError(t, systemService.SetRetryPolicy(ctx, &biz.RetryPolicy{Enabled: true, EmptyResponseDetection: true}))
			candidates := populateAPIFormat(ctx, channelsToTestCandidates([]*biz.Channel{built}, "gpt-6-luna"), &llm.Request{Model: "gpt-6-luna", RequestType: llm.RequestTypeDecisions, APIFormat: llm.APIFormatOpenAIDecisions})
			require.Len(t, candidates, 1)
			orch := &ChatCompletionOrchestrator{
				channelSelector: &staticChannelSelector{candidates: candidates},
				Inbound:         decisions.NewInboundTransformer(), RequestService: requestService, ChannelService: channelService,
				PromptProvider: &stubPromptProvider{}, SystemService: systemService, UsageLogService: usageLogService,
				PipelineFactory: pipeline.NewFactory(httpclient.NewHttpClientWithClient(upstream.Client())),
				ModelMapper:     NewModelMapper(), channelLimiterManager: NewChannelLimiterManager(),
			}
			body := []byte(`{"model":"gpt-6-luna","input":"Invoice evidence","questions":[{"type":"predicate","instructions":"Billing issue?"},{"type":"predicate","name":"private","instructions":"Is this private?"}],"extra":9007199254740993}`)
			result, err := orch.Process(ctx, &httpclient.Request{Method: http.MethodPost, Body: body, Headers: http.Header{"Content-Type": {"application/json"}}})
			require.NoError(t, err)
			require.NotNil(t, result.ChatCompletion)
			require.Equal(t, http.StatusOK, result.ChatCompletion.StatusCode)
			require.Equal(t, "refusal", gjson.GetBytes(result.ChatCompletion.Body, "answers.1.type").String())
			require.Equal(t, "9007199254740993", gjson.GetBytes(result.ChatCompletion.Body, "extra").Raw)
			require.InDelta(t, 0.0000042, gjson.GetBytes(result.ChatCompletion.Body, "usage.cost").Float(), 1e-12)
			select {
			case got := <-received:
				require.JSONEq(t, string(body), string(got))
				require.Equal(t, "9007199254740993", gjson.GetBytes(got, "extra").Raw)
			default:
				t.Fatal("upstream did not receive request")
			}
			usage, err := client.UsageLog.Query().Only(ctx)
			require.NoError(t, err)
			require.Equal(t, llm.APIFormatOpenAIDecisions.String(), usage.Format)
			require.Equal(t, int64(42), usage.PromptTokens)
			require.NotNil(t, usage.TotalCost)
			require.InDelta(t, 0.0000042, *usage.TotalCost, 1e-12)
			require.Len(t, usage.CostItems, 1)
			execution, err := client.RequestExecution.Query().Only(ctx)
			require.NoError(t, err)
			require.Equal(t, "gpt-6-luna", execution.UpstreamModelID)
			stored, err := client.Request.Query().Only(ctx)
			require.NoError(t, err)
			require.Equal(t, llm.APIFormatOpenAIDecisions.String(), stored.Format)
			denied := contexts.WithAPIKey(ctx, &ent.APIKey{Profiles: &objects.APIKeyProfiles{ActiveProfile: "restricted", Profiles: []objects.APIKeyProfile{{Name: "restricted", ModelIDs: []string{"other-model"}}}}})
			_, err = orch.Process(denied, &httpclient.Request{Method: http.MethodPost, Body: body, Headers: http.Header{"Content-Type": {"application/json"}}})
			require.ErrorIs(t, err, biz.ErrInvalidModel)
			select {
			case <-received:
				t.Fatal("denied model reached upstream")
			default:
			}
		})
	}
}

func TestDecisionsCandidatesNeverFallBackToChat(t *testing.T) {
	ctx := context.Background()
	req := &llm.Request{Model: "gpt-6-luna", RequestType: llm.RequestTypeDecisions, APIFormat: llm.APIFormatOpenAIDecisions}
	for _, channelType := range []channel.Type{channel.TypeZenmux, channel.TypeTypesafe, channel.TypeCodex} {
		unsupported := &ChannelModelsCandidate{Channel: &biz.Channel{Channel: &ent.Channel{Type: channelType}}}
		require.Empty(t, populateAPIFormat(ctx, []*ChannelModelsCandidate{unsupported}, req))
	}
	supported := &ChannelModelsCandidate{Channel: &biz.Channel{Channel: &ent.Channel{Type: channel.TypeOpenai}}}
	require.Equal(t, []*ChannelModelsCandidate{supported}, populateAPIFormat(ctx, []*ChannelModelsCandidate{supported}, req))
	require.Equal(t, llm.APIFormatOpenAIDecisions.String(), supported.APIFormat)
	// A model override selecting only chat cannot retain a blank native format.
	forced := &ChannelModelsCandidate{Channel: &biz.Channel{Channel: &ent.Channel{Type: channel.TypeOpenai, Settings: &objects.ChannelSettings{ModelProtocols: []objects.ModelProtocol{{Model: "gpt-6-luna", APIFormats: []string{llm.APIFormatOpenAIChatCompletion.String()}}}}}}, Models: []biz.ChannelModelEntry{{RequestModel: "gpt-6-luna", ActualModel: "gpt-6-luna"}}}
	require.Empty(t, populateAPIFormat(ctx, []*ChannelModelsCandidate{forced}, req))
}
