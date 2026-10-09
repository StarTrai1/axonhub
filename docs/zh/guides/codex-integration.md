# Codex 集成指南

---

## 概览
AxonHub 可以作为 OpenAI 接口的直接替代方案，使 Codex 能够通过您自己的基础设施连接。本文将介绍配置方法，并说明如何结合 AxonHub 的模型配置文件功能实现灵活路由。

### 关键点
- AxonHub 支持多种 AI 协议/格式转换。你可以配置多个上游渠道（provider/channel），对外提供统一的 OpenAI 兼容接口，供 Codex 使用。
- 你可以开启 `server.trace.codex_trace_enabled`（使用 `Session_id`）或配置 `server.trace.extra_trace_headers` 将 Codex 同一次对话的请求聚合到同一条 Trace。

### 前置要求
- 可访问的 AxonHub 实例。
- 拥有项目访问权限的 AxonHub API Key。
- Codex（OpenAI 兼容工具）的使用权限。
- （可选）已在 AxonHub 控制台配置好的一个或多个模型配置文件。

### 配置 Codex
1. 编辑 `${HOME}/.codex/config.toml`，将 AxonHub 注册为 provider：
   ```toml
   model = "gpt-5"
   model_provider = "axonhub-responses"

   [model_providers.axonhub-responses]
   name = "AxonHub Responses"
   base_url = "http://127.0.0.1:8090/v1"
   env_key = "AXONHUB_API_KEY"
   wire_api = "responses"
   query_params = {}
   ```
2. 导出供 Codex 读取的 API Key：
   ```bash
   export AXONHUB_API_KEY="<your-axonhub-api-key>"
   ```
3. 重启 Codex 以加载配置。

#### 按对话聚合 Trace（重要）
开启内置 Codex 追踪提取后，AxonHub 会将 `Session_id` header 作为 trace ID 使用：

```yaml
server:
  trace:
    codex_trace_enabled: true
```

若 Codex 还会携带其他稳定的对话标识 header（例如 `Conversation_id`），可在 `config.yml` 中将其加入 `extra_trace_headers`，用于在主 trace header 缺失时进行聚合：

```yaml
server: 
  trace:
    extra_trace_headers:
      - Conversation_id
```

**提示**：开启此功能后，AxonHub 会将同一个 Trace 的请求优先转发到同一个上游渠道，从而大幅提高提供商端的缓存命中率（例如 Anthropic 的 Prompt Caching）。

#### 验证
- 发送测试 Prompt，AxonHub 日志中应出现 `/v1/responses` 调用。
- 启用 AxonHub 的追踪功能可查看提示词、回复及延迟信息。

### 使用模型配置文件
AxonHub 的模型配置文件支持将请求模型映射到具体提供商模型：
- 在 AxonHub 控制台创建配置文件并添加映射规则（精确名称或正则）。
- 将配置文件绑定到 API Key。
- 切换活动配置文件即可更改 Codex 的行为，无需调整本地工具设置。

<table>
  <tr align="center">
    <td align="center">
      <a href="../../screenshots/axonhub-profiles.png">
        <img src="../../screenshots/axonhub-profiles.png" alt="Model Profiles" width="250"/>
      </a>
      <br/>
      Model Profiles
    </td>
  </tr>
</table>

#### 示例
- 请求 `gpt-4` → 映射到 `deepseek-reasoner` 以获取更准确的回复。
- 请求 `gpt-3.5-turbo` → 映射到 `deepseek-chat` 以降低成本。

### Responses Lite 续接兼容

上游明确拒绝加密思考后，有界重试会原位保留 Responses Lite 的 `additional_tools` 定义和 GPT-6 的 `configuration_update`。恢复仍要求可见历史完整、工具调用与结果匹配；不会丢弃不透明压缩项、未解析引用或加密函数参数。本地压缩桥接与后续加密思考恢复是两个独立阶段。

上游明确拒绝 `internal_chat_message_metadata_passthrough` 时，可为 Codex 所有携带该元数据的输入项建立兼容规则，包括 `custom_tool_call_output` 上的 `cell_id` 元数据。仅移除该可选元数据属性，保留 ID、工具输出、参数、密文及其他请求字段；继续按渠道、端点、模型、凭据隔离，并遵守六小时持久化有效期。

Codex 的 5 小时、7 天及上游已报告的 GPT-Reserve 窗口显示浏览器本地时区的绝对重置时间（`yyyy-MM-dd HH:mm`）。上游未返回的时间不推测补造；主窗口悬停详情仍保留相对倒计时。

各窗口保留上游报告的实际使用比例。账户整体耗尽不会把尚有余量的周窗口改成已用 100%；普通额度耗尽时，渠道仍按配置的配额路由策略处理。

### 本地压缩检查点

新的本地桥接结果通过 `axonhub-local-v2` 引用携带经过认证加密的摘要。服务重启或请求日志清理后，可以直接恢复，不需要重新生成摘要。引用绑定压缩项 ID、API key、项目和安装实例密钥。恢复数据库备份时应一并保留安装实例密钥；更换该密钥会使此前的加密引用失效。

对于旧的 `axonhub-local-v1` 引用，AxonHub 先使用已有检查点或原压缩记录。原记录过期后，仅在线程、压缩窗口、完整的已有输入前缀及工具身份均匹配时，才从已完成的后续请求恢复。客户端内部执行元数据，以及 reasoning content 缺省与 null 的区别，不阻断该比较。恢复的原始摘要会加密保存，并与普通请求日志分开保留。历史缺失或分支不匹配时明确报错，不推测生成替代摘要。

### Codex 0.154.0 兼容

网关原位保留 `configuration_update`，不为其补造消息 ID，并保留客户端传入的 `client_metadata.parent_response_id` 和 `guardian_credits_requested`。没有 Codex 身份信息的请求默认使用 0.154.0；客户端明确传入的身份信息仍优先。

附加配额数据保留上游报告的 `normal_model_slug` 元数据，不据此重映射请求模型。被动配额查询不声明 `x-openai-codex-luna-reserve: 1`；该能力头适用于能执行 Reserve 选择的客户端，见 [Codex 0.154.0 配额客户端](https://github.com/openai/codex/blob/rust-v0.154.0/codex-rs/backend-client/src/client/rate_limit_resets.rs)。

### Codex 0.162.0 兼容

网关在 Responses 转换和流聚合中保留 `partial_answer` 阶段及显式的 `end_turn: false`。一次推理完成不等于 Codex 当前轮次结束。放在 developer 输入消息中的基础指令、增量 `additional_tools` 声明和工具移除通知保持原有历史顺序。

网关生成的流终止错误使用 `response.failed`，合法重试时间位于 `response.error.headers`，也保留 WebSocket 嵌套错误中的重试提示。原有重试预算和硬额度处理继续生效。正常 Responses 错误转换保留提供商的 misalignment 详情及不透明 `review_target`；隐藏/自定义错误策略继续省略这些详情。

Codex 0.162.0 新增自定义提供商能力覆盖。如果所选 AxonHub 渠道支持原生压缩或已启用本地压缩桥接，可选用以下客户端配置启用 V2 `compaction_trigger` 协议：

```toml
[model_providers.axonhub-responses.capabilities]
remote_compaction = "v2"
external_web_access = true
```

`remote_compaction = "unsupported"` 表示在客户端执行压缩。`external_web_access` 控制 Codex 是否可以请求实时网页搜索，不授予上游搜索能力或账户权限；省略字段时保留客户端的提供商默认值。详见[官方能力定义](https://github.com/openai/codex/blob/rust-v0.162.0/codex-rs/model-provider-info/src/capabilities.rs)。

缺省 Codex 身份版本为 0.162.0，显式客户端身份和动态稳定版刷新继续优先。托管检查使用 0.161.0 / 0.162.0 × gpt-6-sol / gpt-6.1-sol 回环矩阵，包含网关自身生成的失败帧，不代表真实提供商账户可用性验证。

#### Instant steering

Codex 0.162.0 默认启用已有的 `instant_interrupt` 客户端功能。在 Responses Lite WebSocket 模式下，客户端发送 `response.interrupt` 和 `mode: "discard_partial_items"`，读取被中断响应的终态后再携带新输入继续。AxonHub 已在当前连接转发该控制事件，保留已完成输出与用量，并从中断快照中排除未完成项目。HTTP 模式走已有取消路径；所选上游不支持主动中断时，网关不能替它实现同等能力。

公开的[轮内 steering API](https://developers.openai.com/api/docs/guides/steering) 仍只允许 `response.steer` 携带 `type`、`previous_response_id`、`input`，不应额外添加 `instant` 字段。公开 API 的排队 steering 与 Codex 客户端中断流程分别遵循各自的终态和工具结果处理规则。

### 常见问题
- **Codex 认证失败**：确保在启动 Codex 的同一 shell 会话中设置了 `AXONHUB_API_KEY`。
- **模型结果异常**：检查 AxonHub 控制台中当前启用的配置文件映射，必要时禁用或调整规则。

### 相关文档
- [追踪指南](tracing.md)
- [OpenAI API 文档](../api-reference/openai-api.md)
- README 中的 [使用指南](../../../README.md#使用指南--usage-guide)


## Codex 0.159.2 与 GPT-6.1 Sol

网关识别 `gpt-6.1-sol` 和本地别名 `gpt-6.1-sol-fast`。仅在请求未指定服务档位时，Fast 别名设置 `service_tier: priority`。明确传入的客户端版本仍优先，缺省版本更新为 0.159.2。

[公开 API 模型](https://developers.openai.com/api/docs/models/gpt-6.1-sol)支持 `low`、`medium`、`high`、`xhigh`、`max`，API 默认值为 `medium`。Codex 0.159.2 使用自身预设（默认 `low`，另有客户端 `ultra` 选项）；网关不会用 API 默认值覆盖客户端的明确选择。迁移请求中不支持的 `none/minimal` 映射为 `low`，包括保持原顺序的 `configuration_update` 项；启用推理时移除不支持的采样和 logprob 参数。工具调用应使用 Responses 出口，该模型的 Chat Completions 出口仅支持无工具请求。

内置目录记录官方标准输入/缓存读取/缓存写入/输出价格，分别为每百万 token $2/$0.10/$2.50/$10，并包含 >272K 上下文价格和 Fast 模式元数据。实际计费仍以既有渠道价格配置为准。[Astra Ultrafast](https://developers.openai.com/api/docs/guides/ultrafast-mode) 使用 `service_tier: ultrafast`；目录已补充元数据，HTTP、WebSocket、转换和响应使用量路径保留该档位。不将 Sol 6.1 Ultrafast 标为已开放。

首个有效输出前发生 TLS `bad record MAC` 时，按上游传输失败分类并复用既有有界重试。如果已根据明确拒绝修复加密 reasoning，后续重试保留该修复和可见历史。TLS 验证不变。缺少加密错误码或精确 item 拒绝的通用 HTTP400 不会触发删除历史。上游连接或容量仍不可用时，重试仍可能失败。

DevDay 的 Agents API 浏览器会话使用独立 `/v1/agents/sessions` 生命周期；Private Safety Processing 属于组织/项目开通配置，不会随 Responses 模型更新而启用。Decisions API 公告仍为有限预览，本次未找到稳定公开的请求契约。本次更新不宣称已支持这些独立服务。

在上游 WebSocket 活跃期间，GPT-6 系列（包括 Sol 6.1 和本地 Fast 别名）可发送 `response.steer`。控制帧仍绑定同连接已注册的响应，并使用既有有界队列；GPT-5.6 及更早模型不开放该功能。参见[中途调整指令](https://developers.openai.com/api/docs/guides/steering)。
