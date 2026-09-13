package biz

import (
	"context"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/ent/enttest"
	entrequest "github.com/looplj/axonhub/internal/ent/request"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/pkg/xcache"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
)

func TestExtractOutboundReasoningEffort(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		format llm.APIFormat
		body   string
		want   *string
	}{
		{
			name:   "mapped final effort",
			format: llm.APIFormatOpenAIChatCompletion,
			body:   `{"model":"glm-5.2","reasoning_effort":"max"}`,
			want:   lo.ToPtr("max"),
		},
		{
			name:   "unmapped effort",
			format: llm.APIFormatOpenAIChatCompletion,
			body:   `{"model":"gpt-5","reasoning_effort":"xhigh"}`,
			want:   lo.ToPtr("xhigh"),
		},
		{
			name:   "responses effort",
			format: llm.APIFormatOpenAIResponse,
			body:   `{"model":"gpt-5","reasoning":{"effort":"high"}}`,
			want:   lo.ToPtr("high"),
		},
		{
			name:   "compact responses effort",
			format: llm.APIFormatOpenAIResponseCompact,
			body:   `{"model":"gpt-5","reasoning":{"effort":"max"}}`,
			want:   lo.ToPtr("max"),
		},
		{
			name:   "GPT-6 latest configuration update overrides the cache prefix effort",
			format: llm.APIFormatOpenAIResponse,
			body:   `{"model":"gpt-6-astra","reasoning":{"effort":"low"},"input":[{"type":"configuration_update","reasoning":{"effort":"high"}},{"role":"user","content":"first turn"},{"type":"configuration_update","reasoning":{"effort":"max"}},{"role":"user","content":"next turn"}]}`,
			want:   lo.ToPtr("max"),
		},
		{
			name:   "WebSocket continuation with no top-level effort",
			format: llm.APIFormatOpenAIResponse,
			body:   `{"type":"response.create","model":"gpt-6-astra","previous_response_id":"resp_previous","input":[{"type":"configuration_update","reasoning":{"effort":"high"}},{"role":"user","content":"continue"}]}`,
			want:   lo.ToPtr("high"),
		},
		{
			name:   "empty and non-string updates do not replace a known effort",
			format: llm.APIFormatOpenAIResponse,
			body:   `{"reasoning":{"effort":"low"},"input":[{"type":"configuration_update","reasoning":{"effort":"high"}},{"type":"configuration_update","reasoning":{"effort":" "}},{"type":"configuration_update","reasoning":{"effort":0}}]}`,
			want:   lo.ToPtr("high"),
		},
		{
			name:   "tool output and unrelated input do not configure effort",
			format: llm.APIFormatOpenAIResponse,
			body:   `{"reasoning":{"effort":"low"},"input":[{"type":"function_call_output","call_id":"call_1","output":[{"type":"configuration_update","reasoning":{"effort":"max"}}]},{"role":"user","reasoning":{"effort":"high"}}]}`,
			want:   lo.ToPtr("low"),
		},
		{
			name:   "compact does not apply unsupported configuration updates",
			format: llm.APIFormatOpenAIResponseCompact,
			body:   `{"reasoning":{"effort":"low"},"input":[{"type":"configuration_update","reasoning":{"effort":"max"}}]}`,
			want:   lo.ToPtr("low"),
		},
		{
			name:   "anthropic output config effort",
			format: llm.APIFormatAnthropicMessage,
			body:   `{"model":"deepseek-v4-flash","output_config":{"effort":"max"}}`,
			want:   lo.ToPtr("max"),
		},
		{
			name:   "missing effort",
			format: llm.APIFormatOpenAIChatCompletion,
			body:   `{"model":"gpt-4"}`,
		},
		{
			name:   "responses missing effort",
			format: llm.APIFormatOpenAIResponse,
			body:   `{"model":"gpt-5","reasoning":{"summary":"auto"}}`,
		},
		{
			name:   "anthropic missing output config effort",
			format: llm.APIFormatAnthropicMessage,
			body:   `{"model":"deepseek-v4-flash","thinking":{"type":"adaptive"}}`,
		},
		{
			name:   "non string effort",
			format: llm.APIFormatOpenAIChatCompletion,
			body:   `{"model":"gpt-5","reasoning_effort":5}`,
		},
		{
			name:   "different API format",
			format: llm.APIFormatGeminiContents,
			body:   `{"reasoning_effort":"max"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			body := []byte(tt.body)
			got := extractOutboundReasoningEffort(httpclient.Request{Body: body}, tt.format)
			require.Equal(t, tt.want, got)
			require.Equal(t, tt.body, string(body), "usage reporting must not rewrite the request or its cache prefix")
		})
	}
}

func TestRequestService_CreateRequestExecutionPersistsReasoningEffort(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:request_reasoning_effort?mode=memory&_fk=0")
	t.Cleanup(func() { client.Close() })

	ctx := authz.WithTestBypass(ent.NewContext(context.Background(), client))
	systemService := NewSystemService(SystemServiceParams{Ent: client})
	channelService := NewChannelServiceForTest(client)
	usageLogService := NewUsageLogService(client, systemService, channelService)
	dataStorageService := NewDataStorageService(DataStorageServiceParams{
		SystemService: systemService,
		CacheConfig:   xcache.Config{Mode: xcache.ModeMemory},
		Client:        client,
	})
	requestService := NewRequestService(
		client,
		systemService.CacheConfig,
		systemService,
		usageLogService,
		dataStorageService,
		NewLiveStreamRegistry(),
	)

	channelEntity, err := client.Channel.Create().
		SetName("opencode-go").
		SetType(channel.TypeOpencodeGo).
		SetBaseURL("https://opencode.ai/zen/go").
		SetCredentials(objects.ChannelCredentials{APIKey: "test-key"}).
		SetSupportedModels([]string{"glm-5.2"}).
		SetDefaultTestModel("glm-5.2").
		SetStatus(channel.StatusEnabled).
		Save(ctx)
	require.NoError(t, err)

	requestEntity, err := client.Request.Create().
		SetModelID("glm-5.2").
		SetReasoningEffort("xhigh").
		SetFormat(string(llm.APIFormatAnthropicMessage)).
		SetRequestBody([]byte(`{"model":"glm-5.2"}`)).
		SetStatus(entrequest.StatusProcessing).
		SetStream(false).
		Save(ctx)
	require.NoError(t, err)

	execution, err := requestService.CreateRequestExecution(
		ctx,
		&Channel{Channel: channelEntity},
		"glm-5.2",
		requestEntity,
		httpclient.Request{
			Body:      []byte(`{"model":"glm-5.2","reasoning_effort":"max"}`),
			APIFormat: string(llm.APIFormatOpenAIChatCompletion),
		},
		llm.APIFormatOpenAIChatCompletion,
		false,
	)
	require.NoError(t, err)
	require.NotNil(t, execution.ReasoningEffort)
	require.Equal(t, "max", *execution.ReasoningEffort)

	responsesExecution, err := requestService.CreateRequestExecution(
		ctx,
		&Channel{Channel: channelEntity},
		"gpt-5.6-luna",
		requestEntity,
		httpclient.Request{
			Body:      []byte(`{"model":"gpt-5.6-luna","reasoning":{"effort":"high"}}`),
			APIFormat: string(llm.APIFormatOpenAIResponse),
		},
		llm.APIFormatOpenAIResponse,
		false,
	)
	require.NoError(t, err)
	require.NotNil(t, responsesExecution.ReasoningEffort)
	require.Equal(t, "high", *responsesExecution.ReasoningEffort)

	astraBody := []byte(`{"model":"gpt-6-astra","reasoning":{"effort":"low"},"input":[{"type":"configuration_update","reasoning":{"effort":"high"}},{"role":"user","content":"continue"}]}`)
	astraExecution, err := requestService.CreateRequestExecution(
		ctx,
		&Channel{Channel: channelEntity},
		"gpt-6-astra",
		requestEntity,
		httpclient.Request{Body: astraBody, APIFormat: string(llm.APIFormatOpenAIResponse)},
		llm.APIFormatOpenAIResponse,
		false,
	)
	require.NoError(t, err)
	require.NotNil(t, astraExecution.ReasoningEffort)
	require.Equal(t, "high", *astraExecution.ReasoningEffort)
	storedExecution, err := client.RequestExecution.Get(ctx, astraExecution.ID)
	require.NoError(t, err)
	require.Equal(t, astraExecution.ReasoningEffort, storedExecution.ReasoningEffort)

	anthropicExecution, err := requestService.CreateRequestExecution(
		ctx,
		&Channel{Channel: channelEntity},
		"deepseek-v4-flash",
		requestEntity,
		httpclient.Request{
			Body:      []byte(`{"model":"deepseek-v4-flash","output_config":{"effort":"max"}}`),
			APIFormat: string(llm.APIFormatAnthropicMessage),
		},
		llm.APIFormatAnthropicMessage,
		false,
	)
	require.NoError(t, err)
	require.NotNil(t, anthropicExecution.ReasoningEffort)
	require.Equal(t, "max", *anthropicExecution.ReasoningEffort)
}
