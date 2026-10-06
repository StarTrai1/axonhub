# OpenAI Decisions API

AxonHub 通过 `POST /v1/decisions` 提供原生 `openai/decisions` 协议。使用 AxonHub API Key 设置 `Authorization: Bearer …`，请求头设置 `Content-Type: application/json`。沿用现有模型权限、模型映射、渠道代理、有界重试和请求／用量记录。

OpenAI 当前公测支持 `gpt-6-luna`。在 OpenAI 或 OpenAI Responses 渠道配置该模型，也可使用映射到它的别名。这两种渠道内置独立的 Decisions 端点，即使主 Responses 端点使用 WebSocket，Decisions 仍使用 HTTP。其他兼容渠道可在确认上游支持后，显式添加 `openai/decisions` 和自定义地址／路径。不支持原生端点的候选渠道会被排除。

```json
{
  "model": "gpt-6-luna",
  "input": "客户反馈收到的设备屏幕已经开裂。",
  "questions": [
    {"type": "predicate", "name": "damage", "instructions": "客户是否报告了物理损坏？"}
  ]
}
```

`input` 接受文本，或包含 `input_text` 与内嵌 base64 `input_image` 的 user 消息。不支持远程图片地址、图片 file ID、非 user 消息、工具、文件、音频和流式请求。每次请求最多 128 张图片。

`questions` 和 `answers` 均为有序数组。问题支持 `predicate`（返回 `probability`）、`choice`（提供 `choices`，返回 `choice`）和 `score`（提供有序 `levels`，返回从零起算的等级索引加权分数）。保留可选名称、单问题拒绝、概率分布及扩展字段。该接口不转换成聊天、Responses 或 TypeSafe System One。

## 配置费用记录

在渠道模型价格编辑器中添加 **Decisions 输入 Token**（`decisions_input_tokens`），按完整的 `usage.input_tokens` 计算。Decisions 不使用聊天的输入、输出、缓存价格；其他请求也不使用 Decisions 价格项。缺少专用价格时费用保持未知。可选的网关 `usage.cost` 字段遵循现有费用注入开关。

截至 2026 年 10 月 7 日，官方 Decisions 输入价格为每百万 Token 0.10 美元，不单独收取输出和缓存费用。可使用全量阶梯（volume）价格：272,000 Token 及以下为 0.10，超过后整段输入为 0.20，以表达长上下文倍数。区域处理附加费需要配置到相应渠道价格。自动同步的聊天目录价格不会填充此专用项，也不会覆盖已明确配置的 Decisions 价格。

来源：[Decisions 指南](https://developers.openai.com/api/docs/guides/decisions)、[GPT-6 Luna 模型](https://developers.openai.com/api/docs/models/gpt-6-luna)。
