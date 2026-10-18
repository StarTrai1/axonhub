# Anthropic API Reference

## Overview

AxonHub supports the native Anthropic Messages API for applications that prefer Anthropic's specific features and response format. You can use the Anthropic SDK to access not only Claude models but also OpenAI, Gemini, and other supported models.

## Key Benefits

- **API Interoperability**: Use Anthropic Messages API to call OpenAI, Gemini, and other supported models
- **Zero Code Changes**: Continue using your existing Anthropic client SDK without modification
- **Automatic Translation**: AxonHub automatically converts between API formats when needed
- **Provider Flexibility**: Access any supported AI provider using the Anthropic API format

## Supported Endpoints

**Endpoints:**
- `POST /anthropic/v1/messages` - Text generation
- `POST /v1/messages` - Text generation (alternative)
- `GET /anthropic/v1/models` - List available models

**Example Request:**
```go
import (
    "github.com/anthropics/anthropic-sdk-go"
    "github.com/anthropics/anthropic-sdk-go/option"
)

// Create Anthropic client with AxonHub configuration
client := anthropic.NewClient(
    option.WithAPIKey("your-axonhub-api-key"),
    option.WithBaseURL("http://localhost:8090/anthropic"),
    
)

// Call OpenAI model using Anthropic API format
messages := []anthropic.MessageParam{
    anthropic.NewUserMessage(anthropic.NewTextBlock("Hello, GPT!")),
}

response, err := client.Messages.New(ctx, anthropic.MessageNewParams{
    Model:     anthropic.Model("gpt-4o"),
    Messages:  messages,
    MaxTokens: 1024,
})
if err != nil {
    // Handle error appropriately
    panic(err)
}

// Extract text content from response
responseText := ""
for _, block := range response.Content {
    if textBlock := block.AsText(); textBlock != nil {
        responseText += textBlock.Text
    }
}
fmt.Println(responseText)
```

## API Translation Capabilities

AxonHub automatically translates between API formats, enabling powerful scenarios:

### Use Anthropic SDK with OpenAI Models
```go
// Anthropic SDK calling OpenAI model
messages := []anthropic.MessageParam{
    anthropic.NewUserMessage(anthropic.NewTextBlock("What is machine learning?")),
}

response, err := client.Messages.New(ctx, anthropic.MessageNewParams{
    Model:     anthropic.Model("gpt-4o"),  // OpenAI model
    Messages:  messages,
    MaxTokens: 1024,
})

// Access response
for _, block := range response.Content {
    if textBlock := block.AsText(); textBlock != nil {
        fmt.Println(textBlock.Text)
    }
}
// AxonHub automatically translates Anthropic format → OpenAI format
```

### Use Anthropic SDK with Gemini Models
```go
// Anthropic SDK calling Gemini model
messages := []anthropic.MessageParam{
    anthropic.NewUserMessage(anthropic.NewTextBlock("Explain quantum computing")),
}

response, err := client.Messages.New(ctx, anthropic.MessageNewParams{
    Model:     anthropic.Model("gemini-2.5"),  // Gemini model
    Messages:  messages,
    MaxTokens: 1024,
})

// Access response
for _, block := range response.Content {
    if textBlock := block.AsText(); textBlock != nil {
        fmt.Println(textBlock.Text)
    }
}
// AxonHub automatically translates Anthropic format → Gemini format
```

## Authentication

The Anthropic API format uses the following authentication:

- **Header**: `X-API-Key: <your-api-key>`

The API keys are managed through AxonHub's API Key management system and provide the same permissions regardless of which API format you use.

## Streaming Support

Anthropic API format supports streaming responses:

```go
// Anthropic SDK streaming
messages := []anthropic.MessageParam{
    anthropic.NewUserMessage(anthropic.NewTextBlock("Count to five")),
}

stream := client.Messages.NewStreaming(ctx, anthropic.MessageNewParams{
    Model:     anthropic.Model("gpt-4o"),
    Messages:  messages,
    MaxTokens: 1024,
})

// Collect streamed content
var content string
for stream.Next() {
    event := stream.Current()
    switch event := event.(type) {
    case anthropic.ContentBlockDeltaEvent:
        if event.Type == "content_block_delta" {
            content += event.Delta.Text
            fmt.Print(event.Delta.Text) // Print as it streams
        }
    }
}

if err := stream.Err(); err != nil {
    panic(err)
}

fmt.Println("\nComplete response:", content)
```

### Cross-provider tool streaming and usage

When an upstream interleaves arguments from parallel function calls, AxonHub converts them into sequential Anthropic content blocks without mixing call IDs or argument fragments. The ordinary first-tool path continues streaming immediately. Only a detected interleaved tail is buffered, through its finish event or clean EOF, with limits of 4096 chunks and 8 MiB including the active argument prefix. Text and reasoning in that tail retain their relative order. Invalid arguments, transport failures, and buffer overflow fail the stream instead of repairing JSON or manufacturing a successful completion. Native Responses passthrough is unchanged.

Function tools whose `input_schema` is absent or null receive an empty object schema during conversion. Existing schemas, including dialect identifiers and union branches, are not stripped by this fallback.

When Anthropic function tools are converted to Responses, an omitted `strict` becomes explicit `false`, retaining best-effort tool input semantics rather than opting into Responses schema normalization. Explicit `true` and `false` are preserved. Native Responses requests retain their own defaults. See [OpenAI strict mode](https://developers.openai.com/api/docs/guides/function-calling#strict-mode) and [Anthropic strict tool use](https://platform.claude.com/docs/en/build-with-claude/structured-outputs).

Positive reasoning usage is exposed as `usage.output_tokens_details.thinking_tokens`, including on the final streaming `message_delta`. Native Anthropic thinking usage is also retained in the unified reasoning-token count. Counts are bounded to the non-negative output total; absent, zero, or invalid outgoing counts do not add an optional breakdown. `output_tokens` remains the inclusive billing total: thinking is neither added again nor treated as cache creation. See the [Anthropic streaming contract](https://platform.claude.com/docs/en/build-with-claude/streaming) and [usage definition](https://github.com/anthropics/anthropic-sdk-python/blob/62de60b27d04f0927a0ccf0f2610597fafcfab6a/src/anthropic/types/output_tokens_details.py).

### Structured response schemas

When a native Anthropic, Chat Completions, or Responses request carries a JSON Schema response constraint, conversion to a Claude destination restores it in `output_config.format` after reasoning normalization. This keeps the schema when an effort level is changed or omitted. Schema property order and constraints are retained; no prompt text, tool definitions, or reasoning levels are added to compensate for a missing format. OpenAI envelope fields such as `name` and `strict` are not sent as Claude format fields. Malformed or missing schema objects fail before an upstream request is sent.

This mapping applies to direct Claude, Claude Code, Bedrock and Vertex destinations. It does not infer format support from effort support on third-party Anthropic-compatible endpoints: DeepSeek documents only `output_config.effort`. Model-specific schema restrictions still belong to the provider. Keeping the same schema also avoids unnecessarily changing a parameter that affects prompt caching; this is not a measured cache-hit improvement. See [Claude structured outputs](https://platform.claude.com/docs/en/build-with-claude/structured-outputs) and [DeepSeek's Anthropic compatibility](https://api-docs.deepseek.com/guides/anthropic_api/).

## Error Handling

Anthropic format error responses:

```json
{
  "type": "error",
  "error": {
    "type": "invalid_request_error",
    "message": "Invalid API key"
  }
}
```

## Tool Support

AxonHub supports function tools across providers and hosted web search through compatible Anthropic and Responses channels.

| Tool Type | Support Status | Notes |
| --------- | -------------- | ----- |
| **Function Tools** | ✅ Supported | Custom function definitions work across all providers |
| **Web Search** | ✅ Supported | Hosted search on compatible Anthropic/Responses channels |
| **Code Interpreter** | ❌ Not Supported | Provider-specific (OpenAI, Anthropic, etc.) |
| **File Search** | ❌ Not Supported | Provider-specific |
| **Computer Use** | ❌ Not Supported | Anthropic-specific |

> Native `web_search_20250305` and `web_search_20260209` declarations map to Responses `web_search`. Named search selection remains mandatory even when other function tools are present. A client function named `web_search` keeps its function identity. Search terminal events become paired `server_tool_use` and `web_search_tool_result` blocks; failures retain a tool error result, and absent encrypted results remain an empty list. Other provider-specific tools still require a supported native channel.

For OpenAI requests converted to Claude Haiku 5.5, `reasoning.effort: none` maps to disabled thinking with low effort. Adaptive thinking, forced tool selection, signed history and the existing 100k-token pricing tier remain supported. Native Anthropic parameters retain their explicit values; this translation does not rewrite stored thinking blocks. See the [Haiku 5.5 migration guide](https://platform.claude.com/docs/en/models/haiku-5-5/migration-guide).

## Best Practices

1. **Use Tracing Headers**: Include `AH-Trace-Id` and `AH-Thread-Id` headers for better observability
2. **Model Selection**: Specify the target model explicitly in your requests
3. **Error Handling**: Implement proper error handling for API responses
4. **Streaming**: Use streaming for better user experience with long responses
5. **Use Function Tools**: For tool calling, use generic function tools instead of provider-specific tools

## Migration Guide

### From Anthropic to AxonHub
```go
// Before: Direct Anthropic
client := anthropic.NewClient(
    option.WithAPIKey("anthropic-key"),
)

// After: AxonHub with Anthropic API
client := anthropic.NewClient(
    option.WithAPIKey("axonhub-api-key"),
    option.WithBaseURL("http://localhost:8090/anthropic"),
)
// Your existing code continues to work!
```
