package antigravity

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
)

func structuredRequest() *httpclient.Request {
	return &httpclient.Request{Body: []byte(`{"model":"claude-sonnet-4-6","request":{"generationConfig":{"responseSchema":{"type":"OBJECT"}}}}`)}
}

func TestClaudeStructuredOutputRequestScope(t *testing.T) {
	require.True(t, claudeStructuredOutput(structuredRequest()))
	for _, req := range []*httpclient.Request{
		nil,
		{Body: []byte(`{"model":"claude-sonnet-4-6","request":{}}`)},
		{Body: []byte(`{"model":"gemini-3.8-pro","request":{"generationConfig":{"responseSchema":{}}}}`)},
		{Body: []byte(`{"model":"claude-sonnet-4-6","request":{"generationConfig":{"responseSchema":{}},"tools":[{"functionDeclarations":[{"name":"json_mode"}]}]}}`)},
	} {
		require.False(t, claudeStructuredOutput(req))
	}
}

func TestClaudeStructuredOutputPreservesJSONAndCandidateBoundaries(t *testing.T) {
	state := make(structuredOutputState)
	body := []byte(`{"responseId":"fixture","candidates":[{"index":0,"content":{"role":"model","parts":[{"thought":true,"text":"reasoning","thoughtSignature":"signed"},{"text":"Here is the result:"},{"functionCall":{"name":"json_mode","args":{"z":9007199254740993,"a":true}}}]}},{"index":1,"content":{"role":"model","parts":[{"functionCall":{"name":"real_tool","args":{}}}]},"finishReason":"OTHER"}]}`)
	got, err := state.rewrite(body)
	require.NoError(t, err)
	require.Equal(t, `{"z":9007199254740993,"a":true}`, gjson.GetBytes(got, "candidates.0.content.parts.1.text").String())
	require.Equal(t, "signed", gjson.GetBytes(got, "candidates.0.content.parts.0.thoughtSignature").String())
	require.Equal(t, "OTHER", gjson.GetBytes(got, "candidates.1.finishReason").String())
	require.NotContains(t, string(got), "Here is the result")
	require.Contains(t, string(body), "json_mode", "input must remain unchanged")
	terminal, err := state.rewrite([]byte(`{"candidates":[{"index":0,"finishReason":"OTHER"},{"index":1,"finishReason":"OTHER"}],"usageMetadata":{"totalTokenCount":42}}`))
	require.NoError(t, err)
	require.Equal(t, "STOP", gjson.GetBytes(terminal, "candidates.0.finishReason").String())
	require.Equal(t, "OTHER", gjson.GetBytes(terminal, "candidates.1.finishReason").String())
	require.EqualValues(t, 42, gjson.GetBytes(terminal, "usageMetadata.totalTokenCount").Int())
}

func TestClaudeStructuredOutputAllResponsePaths(t *testing.T) {
	adapter, err := NewTransformer(Config{APIKey: "synthetic", Project: "fixture"})
	require.NoError(t, err)
	req := structuredRequest()
	body := []byte(`{"response":{"responseId":"fixture","modelVersion":"claude-sonnet-4-6","candidates":[{"index":0,"content":{"role":"model","parts":[{"functionCall":{"name":"json_mode","args":{"answer":42}}}]},"finishReason":"OTHER"}],"usageMetadata":{"promptTokenCount":2,"candidatesTokenCount":3,"totalTokenCount":5}}}`)
	result, err := adapter.TransformResponse(t.Context(), &httpclient.Response{StatusCode: 200, Body: body, Request: req})
	require.NoError(t, err)
	require.JSONEq(t, `{"answer":42}`, *result.Choices[0].Message.Content.Content)
	require.Empty(t, result.Choices[0].Message.ToolCalls)
	require.Equal(t, "stop", *result.Choices[0].FinishReason)
	event := &httpclient.StreamEvent{Data: body}
	stream, err := adapter.TransformStream(t.Context(), req, streams.SliceStream([]*httpclient.StreamEvent{event}))
	require.NoError(t, err)
	var text strings.Builder
	for stream.Next() {
		for _, choice := range stream.Current().Choices {
			if choice.Delta != nil {
				require.Empty(t, choice.Delta.ToolCalls)
				if choice.Delta.Content.Content != nil { text.WriteString(*choice.Delta.Content.Content) }
			}
		}
	}
	require.NoError(t, stream.Err())
	require.NoError(t, stream.Close())
	require.JSONEq(t, `{"answer":42}`, text.String())
	aggregated, _, err := adapter.AggregateStreamChunks(t.Context(), req, []*httpclient.StreamEvent{event})
	require.NoError(t, err)
	require.Contains(t, string(aggregated), `\"answer\":42`)
	require.NotContains(t, string(aggregated), "json_mode")
	require.JSONEq(t, string(body), string(event.Data))

	// A real caller tool remains a tool even with responseSchema enabled.
	req.Body = []byte(`{"model":"claude-sonnet-4-6","request":{"generationConfig":{"responseSchema":{}},"tools":[{"functionDeclarations":[{"name":"json_mode"}]}]}}`)
	result, err = adapter.TransformResponse(t.Context(), &httpclient.Response{StatusCode: 200, Body: body, Request: req})
	require.NoError(t, err)
	require.Len(t, result.Choices[0].Message.ToolCalls, 1)
	require.Equal(t, "json_mode", result.Choices[0].Message.ToolCalls[0].Function.Name)
}

func TestClaudeStructuredOutputInvalidArgumentsFail(t *testing.T) {
	for _, args := range []string{`"not json"`, ``} {
		call := `{"name":"json_mode"}`
		if args != "" { call = `{"name":"json_mode","args":` + args + `}` }
		body := []byte(`{"candidates":[{"content":{"parts":[{"functionCall":` + call + `}]}}]}`)
		require.True(t, json.Valid(body))
		_, err := make(structuredOutputState).rewrite(body)
		require.ErrorContains(t, err, "invalid json_mode arguments")
	}
}

func TestClaudeStructuredOutputJSONStringArguments(t *testing.T) {
	body := []byte(`{"candidates":[{"index":2,"content":{"parts":[{"functionCall":{"name":"json_mode","args":"{\"z\":9007199254740993,\"a\":true}"}}]},"finishReason":"OTHER"}]}`)
	got, err := make(structuredOutputState).rewrite(body)
	require.NoError(t, err)
	require.Equal(t, `{"z":9007199254740993,"a":true}`, gjson.GetBytes(got, "candidates.0.content.parts.0.text").String())
	require.Equal(t, "STOP", gjson.GetBytes(got, "candidates.0.finishReason").String())
}
