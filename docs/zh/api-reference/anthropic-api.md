# Anthropic API 参考

## 概述

AxonHub 支持原生 Anthropic Messages API，适用于偏好 Anthropic 特定功能和响应格式的应用程序。您可以使用 Anthropic SDK 访问 Claude 模型，也可以访问 OpenAI、Gemini 和其他支持的模型。

## 核心优势

- **API 互操作性**：使用 Anthropic Messages API 调用 OpenAI、Gemini 和其他支持的模型
- **零代码变更**：继续使用现有的 Anthropic 客户端 SDK，无需修改
- **自动转换**：AxonHub 在需要时自动在 API 格式之间进行转换
- **提供商灵活性**：使用 Anthropic API 格式访问任何支持的 AI 提供商

## 支持的端点

**端点：**
- `POST /anthropic/v1/messages` - 文本生成
- `POST /v1/messages` - 文本生成 (可选)
- `GET /anthropic/v1/models` - 列出可用模型

**示例请求：**
```go
import (
    "github.com/anthropics/anthropic-sdk-go"
    "github.com/anthropics/anthropic-sdk-go/option"
)

// 使用 AxonHub 配置创建 Anthropic 客户端
client := anthropic.NewClient(
    option.WithAPIKey("your-axonhub-api-key"),
    option.WithBaseURL("http://localhost:8090/anthropic"),
    
)

// 使用 Anthropic API 格式调用 OpenAI 模型
messages := []anthropic.MessageParam{
    anthropic.NewUserMessage(anthropic.NewTextBlock("Hello, GPT!")),
}

response, err := client.Messages.New(ctx, anthropic.MessageNewParams{
    Model:     anthropic.Model("gpt-4o"),
    Messages:  messages,
    MaxTokens: 1024,
})
if err != nil {
    // 适当处理错误
    panic(err)
}

// 从响应中提取文本内容
responseText := ""
for _, block := range response.Content {
    if textBlock := block.AsText(); textBlock != nil {
        responseText += textBlock.Text
    }
}
fmt.Println(responseText)
```

## API 转换能力

AxonHub 自动在 API 格式之间进行转换，实现以下强大场景：

### 使用 Anthropic SDK 调用 OpenAI 模型
```go
// Anthropic SDK 调用 OpenAI 模型
messages := []anthropic.MessageParam{
    anthropic.NewUserMessage(anthropic.NewTextBlock("你好，世界！")),
}

response, err := client.Messages.New(ctx, anthropic.MessageNewParams{
    Model:     anthropic.Model("gpt-4o"),  // OpenAI 模型
    Messages:  messages,
    MaxTokens: 1024,
})

// 访问响应
for _, block := range response.Content {
    if textBlock := block.AsText(); textBlock != nil {
        fmt.Println(textBlock.Text)
    }
}
// AxonHub 自动转换 Anthropic 格式 → OpenAI 格式
```

### 使用 Anthropic SDK 调用 Gemini 模型
```go
// Anthropic SDK 调用 Gemini 模型
messages := []anthropic.MessageParam{
    anthropic.NewUserMessage(anthropic.NewTextBlock("解释量子计算")),
}

response, err := client.Messages.New(ctx, anthropic.MessageNewParams{
    Model:     anthropic.Model("gemini-2.5"),  // Gemini 模型
    Messages:  messages,
    MaxTokens: 1024,
})

// 访问响应
for _, block := range response.Content {
    if textBlock := block.AsText(); textBlock != nil {
        fmt.Println(textBlock.Text)
    }
}
// AxonHub 自动转换 Anthropic 格式 → Gemini 格式
```

## 认证

Anthropic API 格式使用以下认证方式：

- **头部**：`X-API-Key: <your-api-key>`

API 密钥通过 AxonHub 的 API 密钥管理系统进行管理，无论使用哪种 API 格式，都提供相同的权限。

## 流式支持

Anthropic API 格式支持流式响应：

```go
// Anthropic SDK 流式传输
messages := []anthropic.MessageParam{
    anthropic.NewUserMessage(anthropic.NewTextBlock("从一数到五")),
}

stream := client.Messages.NewStreaming(ctx, anthropic.MessageNewParams{
    Model:     anthropic.Model("gpt-4o"),
    Messages:  messages,
    MaxTokens: 1024,
})

// 收集流式内容
var content string
for stream.Next() {
    event := stream.Current()
    switch event := event.(type) {
    case anthropic.ContentBlockDeltaEvent:
        if event.Type == "content_block_delta" {
            content += event.Delta.Text
            fmt.Print(event.Delta.Text) // 边传输边打印
        }
    }
}

if err := stream.Err(); err != nil {
    panic(err)
}

fmt.Println("\n完整响应:", content)
```

### 跨提供商工具流与用量

当上游交错发送多个并行函数调用的参数时，AxonHub 将其转换为顺序排列的 Anthropic 内容块，避免混淆调用 ID 或参数片段。普通首个工具的流式输出不等待完整响应；仅在发现交错后缓冲该段后续内容，直到结束事件或正常 EOF，最多 4096 个块、8 MiB（包含当前工具已有的参数前缀）。该段文本与思考内容之间的相对顺序保持不变。参数不完整、传输失败或缓冲超限时终止流，不猜测修补 JSON，也不伪造成功完成。原生 Responses 透传不受影响。

函数工具的 `input_schema` 缺失或为 null 时，转换阶段补为空对象 schema。此兜底不剥离已有 schema 的方言标识或 union 分支。

正数思考用量通过 `usage.output_tokens_details.thinking_tokens` 返回，流式响应在最终 `message_delta` 中携带；原生 Anthropic 思考用量也保留到统一的 reasoning token 计数。计数限制在非负的输出总量内；缺失、零或无效的出站计数不新增可选明细。`output_tokens` 仍是包含思考的计费总量，不重复累加思考，也不把它当作缓存创建。参见 [Anthropic 流协议](https://platform.claude.com/docs/en/build-with-claude/streaming)与[用量定义](https://github.com/anthropics/anthropic-sdk-python/blob/62de60b27d04f0927a0ccf0f2610597fafcfab6a/src/anthropic/types/output_tokens_details.py)。

## 错误处理

Anthropic 格式错误响应：

```json
{
  "type": "error",
  "error": {
    "type": "invalid_request_error",
    "message": "Invalid API key"
  }
}
```

## 工具支持

AxonHub 通过 Anthropic API 格式支持**函数工具**（自定义函数调用）。但是，**不支持**各提供商特有的工具：

| 工具类型 | 支持状态 | 说明 |
| -------- | -------- | ---- |
| **函数工具（Function Tools）** | ✅ 支持 | 自定义函数定义可跨所有提供商使用 |
| **网页搜索（Web Search）** | ❌ 不支持 | 提供商特有功能（OpenAI、Anthropic 等） |
| **代码解释器（Code Interpreter）** | ❌ 不支持 | 提供商特有功能（OpenAI、Anthropic 等） |
| **文件搜索（File Search）** | ❌ 不支持 | 提供商特有功能 |
| **计算机使用（Computer Use）** | ❌ 不支持 | Anthropic 特有功能 |

> **注意**：仅支持可跨提供商转换的通用函数工具。网页搜索、代码解释器、计算机使用等提供商特有工具需要直接访问提供商的基础设施，无法通过 AxonHub 代理。

## 最佳实践

1. **使用追踪头部**：包含 `AH-Trace-Id` 和 `AH-Thread-Id` 头部以获得更好的可观测性
2. **模型选择**：在请求中明确指定目标模型
3. **错误处理**：为 API 响应实现适当的错误处理
4. **流式处理**：对于长响应使用流式处理以获得更好的用户体验
5. **使用函数工具**：进行工具调用时，请使用通用函数工具而非提供商特有工具

## 迁移指南

### 从 Anthropic 迁移到 AxonHub
```go
// 之前：直接 Anthropic
client := anthropic.NewClient(
    option.WithAPIKey("anthropic-key"),
)

// 之后：使用 Anthropic API 的 AxonHub
client := anthropic.NewClient(
    option.WithAPIKey("axonhub-api-key"),
    option.WithBaseURL("http://localhost:8090/anthropic"),
)
// 您的现有代码继续工作！
```
