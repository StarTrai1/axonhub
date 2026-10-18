package anthropic_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer"
	"github.com/looplj/axonhub/llm/transformer/anthropic"
	"github.com/looplj/axonhub/llm/transformer/openai"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

func TestAnthropicStructuredOutputConversion(t *testing.T) {
	const schema = `{"type":"object","properties":{"z":{"type":"integer","minimum":7},"a":{"type":"string","enum":["yes","no"]}},"required":["z","a"],"additionalProperties":false}`
	for _, source := range []struct {
		name string
		inbound transformer.Inbound
		body string
	}{
		{"anthropic", anthropic.NewInboundTransformer(), `{"model":"claude-opus-5-5","max_tokens":8192,"messages":[{"role":"user","content":"answer"}],"thinking":{"type":"adaptive"},"cache_control":{"type":"ephemeral"},"output_config":{"effort":"max","format":{"type":"json_schema","schema":%s}}}`},
		{"chat", openai.NewInboundTransformer(), `{"model":"claude-opus-5-5","messages":[{"role":"user","content":"answer"}],"reasoning_effort":"max","response_format":{"type":"json_schema","json_schema":{"name":"answer","strict":true,"schema":%s}}}`},
		{"responses", responses.NewInboundTransformer(), `{"model":"claude-opus-5-5","input":"answer","reasoning":{"effort":"max"},"text":{"format":{"type":"json_schema","name":"answer","strict":true,"schema":%s}}}`},
	} {
		for _, persisted := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/persisted=%t", source.name, persisted), func(t *testing.T) {
				ctx := context.Background()
				raw := &httpclient.Request{Headers: http.Header{"Content-Type": {"application/json"}}, Body: []byte(fmt.Sprintf(source.body, schema))}
				common, err := source.inbound.TransformRequest(ctx, raw)
				require.NoError(t, err)
				if persisted {
					encoded, err := json.Marshal(common)
					require.NoError(t, err)
					common = &llm.Request{}
					require.NoError(t, json.Unmarshal(encoded, common))
				}
				before, err := json.Marshal(common)
				require.NoError(t, err)
				outbound, err := anthropic.NewOutboundTransformer("https://example.invalid", "test-key")
				require.NoError(t, err)
				for range 2 {
					wire, err := outbound.TransformRequest(ctx, common)
					require.NoError(t, err)
					require.Equal(t, schema, gjson.GetBytes(wire.Body, "output_config.format.schema").Raw)
					require.Equal(t, "json_schema", gjson.GetBytes(wire.Body, "output_config.format.type").String())
					require.Equal(t, "max", gjson.GetBytes(wire.Body, "output_config.effort").String())
					require.False(t, gjson.GetBytes(wire.Body, "output_config.format.name").Exists())
					require.False(t, gjson.GetBytes(wire.Body, "output_config.format.strict").Exists())
					require.Equal(t, "answer", gjson.GetBytes(wire.Body, "messages.0.content").String())
					if source.name == "anthropic" && !persisted {
						require.Equal(t, "ephemeral", gjson.GetBytes(wire.Body, "cache_control.type").String())
					}
				}
				after, err := json.Marshal(common)
				require.NoError(t, err)
				require.Equal(t, string(before), string(after))
			})
		}
	}
}

func TestAnthropicStructuredOutputSurvivesEffortNormalization(t *testing.T) {
	for _, tc := range []struct{ model, effort, expected string }{
		{"claude-sonnet-4-5", "max", ""},
		{"claude-opus-5-5", "none", "low"},
		{"claude-opus-4-6", "xhigh", "max"},
	} {
		t.Run(tc.model, func(t *testing.T) {
			src := &llm.Request{APIFormat: llm.APIFormatOpenAIResponse, Model: tc.model, ReasoningEffort: tc.effort,
				Messages: []llm.Message{{Role: "user", Content: llm.MessageContent{Content: lo.ToPtr("answer")}}},
				ResponseFormat: &llm.ResponseFormat{Type: "json_schema", JSONSchema: json.RawMessage(`{"schema":{"type":"object"}}`)}}
			outbound, err := anthropic.NewOutboundTransformer("https://example.invalid", "test-key")
			require.NoError(t, err)
			wire, err := outbound.TransformRequest(context.Background(), src)
			require.NoError(t, err)
			require.Equal(t, tc.expected, gjson.GetBytes(wire.Body, "output_config.effort").String())
			require.Equal(t, `{"type":"object"}`, gjson.GetBytes(wire.Body, "output_config.format.schema").Raw)
		})
	}
}

func TestAnthropicStructuredOutputBoundaries(t *testing.T) {
	for _, encoded := range []string{`{`, `null`, `{}`, `{"schema":null}`, `{"schema":[]}`, `{"schema":true}`} {
		t.Run(encoded, func(t *testing.T) {
			src := &llm.Request{Model: "claude-opus-5-5", Messages: []llm.Message{{Role: "user", Content: llm.MessageContent{Content: lo.ToPtr("answer")}}},
				ResponseFormat: &llm.ResponseFormat{Type: "json_schema", JSONSchema: json.RawMessage(encoded)}}
			outbound, err := anthropic.NewOutboundTransformer("https://example.invalid", "test-key")
			require.NoError(t, err)
			wire, err := outbound.TransformRequest(context.Background(), src)
			require.ErrorIs(t, err, transformer.ErrInvalidRequest)
			require.Nil(t, wire)
		})
	}
	for _, platform := range []anthropic.PlatformType{anthropic.PlatformDeepSeek, anthropic.PlatformMoonshot, anthropic.PlatformZai} {
		t.Run(string(platform), func(t *testing.T) {
			outbound, err := anthropic.NewOutboundTransformerWithConfig(&anthropic.Config{Type: platform, BaseURL: "https://example.invalid"})
			require.NoError(t, err)
			src := &llm.Request{Model: "provider-model", ReasoningEffort: "high", Messages: []llm.Message{{Role: "user", Content: llm.MessageContent{Content: lo.ToPtr("answer")}}},
				ResponseFormat: &llm.ResponseFormat{Type: "json_schema", JSONSchema: json.RawMessage(`{"schema":{"type":"object"}}`)}}
			wire, err := outbound.TransformRequest(context.Background(), src)
			require.NoError(t, err)
			require.False(t, gjson.GetBytes(wire.Body, "output_config.format").Exists())
		})
	}
}
