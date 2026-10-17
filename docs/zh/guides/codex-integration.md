# Codex 集成指南

---

## 概览
AxonHub 可以作为 OpenAI 接口的直接替代方案，使 Codex 能够通过您自己的基础设施连接。本文将介绍配置方法，并说明如何结合 AxonHub 的模型配置文件功能实现灵活路由。

### 关键点
- AxonHub 支持多种 AI 协议/格式转换。你可以配置多个上游渠道（provider/channel），对外提供统一的 OpenAI 兼容接口，供 Codex 使用。
- 你可以开启 `server.trace.codex_trace_enabled`（使用 `Session_id`）或配置 `server.trace.extra_trace_headers` 将 Codex 同一次对话的请求聚合到同一条 Trace。

### 前置要求与 AxonHub 设置
- 开发机可以访问 AxonHub 实例。
- 拥有项目访问权限的 AxonHub API Key。
- 可以使用 Codex（OpenAI 兼容工具）。
- AxonHub 中已有一个 Codex 渠道。该渠道提供上游地址、凭据和 HTTP 传输配置。

配置 Codex 前，请在 AxonHub 的 **Codex 兼容**设置页中：

1. 开启兼容开关。
2. 选择用于原生模型目录的 Codex 渠道。
3. 点击渠道的**测试**按钮，确认其 `/models` 接口可访问。测试不会保存设置、启用渠道或修改渠道状态。

兼容开关默认关闭。保存开启的配置时必须选择一个现存 Codex 渠道。即使开关关闭，也可以执行目录测试；测试失败不会阻止保存。目录结果只会过滤为当前调用者可见的模型；这不等于允许手工指定该模型，推理请求仍由现有权限链路检查。

### 配置 Codex 认证与路由
1. 编辑 `${HOME}/.codex/auth.json`，在两个支持的字段中都填入 AxonHub API Key。保留双字段可以兼容不同 Codex 客户端版本，避免认证字段变化导致不可预期的问题：
   ```json
   {
     "personal_access_token": "ah-***",
     "OPENAI_API_KEY": "ah-***"
   }
   ```
2. 编辑 `${HOME}/.codex/config.toml`，让 provider 和认证 API 都指向同一个 `/codex` 兼容入口：
   ```toml
   model = "gpt-6.1-sol"
   model_provider = "openai"
   openai_base_url = "https://your-axonhub-host/codex"
   chatgpt_base_url = "https://your-axonhub-host/codex"
   web_search = "live"

   [features]
   fast_mode = true
   context_management = true
   ```
3. 在 macOS/Linux 启动 Codex 的同一个 shell 中设置必需的认证 API 地址：
   ```bash
   export CODEX_AUTHAPI_BASE_URL=https://your-axonhub-host/codex
   ```
   Windows 用户请在**系统环境变量**中添加 `CODEX_AUTHAPI_BASE_URL=https://your-axonhub-host/codex`。不要设置独立的目录 URL；whoami、models 和 Responses 请求都使用 `/codex` 入口。
4. 重启 Codex 以加载配置。

本指南使用的兼容入口如下：

- `GET /codex/v1/user-auth-credential/whoami`：本地 API Key 身份信息。
- `GET /codex/api/codex/accounts/check`：Codex 账户选择所需的账户元数据。
- `GET /codex/models`：选定渠道的原生目录与调用者可见模型 ID 的交集。
- `POST /codex/images/generations`：通过普通模型/渠道路由调用原生图片生成接口。
- `POST /codex/images/edits`：通过普通模型/渠道路由调用原生图片编辑接口。
- `POST /codex/responses`：Responses HTTP/SSE 推理。
- `GET /codex/responses`：Responses WebSocket 推理。

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
- 启动 Codex 并发送测试 Prompt，AxonHub 日志中应出现 `/codex/responses`。
- 确认模型列表从 `/codex/models` 加载，且选定渠道仍可用于推理。
- 如果 Codex 客户端使用账户发现功能，请确认使用同一 API Key 可以访问 `/codex/api/codex/accounts/check`。
- 图片生成和编辑分别使用 `/codex/images/generations` 与 `/codex/images/edits`；实际路由到的推理渠道不一定是目录渠道，但必须提供对应的原生图片端点和模型。
- 启用 AxonHub 的追踪功能可查看提示词、回复及延迟信息。

当前版本的远程 compaction-v2 复用普通 Responses 路径：向 `/codex/responses` 发送 `compaction_trigger`，再把返回的压缩项用于后续 Responses 请求。不实现 history/notes API，也不新增独立的 `/codex/responses/compact` 路由；这些未支持路径返回 JSON 404。`context_management` 会启用 Codex 的 Responses 压缩流程，history notes 仍属于后续兼容工作范围。`fast_mode` 会让客户端请求 Responses 的 `service_tier`；该等级是否可用由上游模型目录和响应决定。当前没有独立的 `ultrafast` 接口或本地能力探测。既有 `/v1` 路由保持不变。

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

上游返回 `validation_error` / `invalid encrypted summary`，或具体 reasoning 项的 `reasoning hydration failed: Encrypted content could not be decrypted or parsed.` 时，会进入已有的有界恢复流程。只有完整匹配的加密拒绝、完整可见历史和匹配的项目/API Key 才允许恢复。先保留原生检查点修复被拒绝的思考；检查点仍被拒绝时，再使用已认证的保留源历史或已保存摘要。历史不足或重试预算耗尽时停止，不补造历史。

新的本地桥接结果通过 `axonhub-local-v2` 引用携带经过认证加密的摘要。服务重启或请求日志清理后，可以直接恢复，不需要重新生成摘要。引用绑定压缩项 ID、API key、项目和安装实例密钥。恢复数据库备份时应一并保留安装实例密钥；更换该密钥会使此前的加密引用失效。

对于旧的 `axonhub-local-v1` 引用，AxonHub 先使用已有检查点或原压缩记录。原记录过期后，仅在线程、压缩窗口、完整的已有输入前缀及工具身份均匹配时，才从已完成的后续请求恢复。客户端内部执行元数据，以及 reasoning content 缺省与 null 的区别，不阻断该比较。恢复的原始摘要会加密保存，并与普通请求日志分开保留。历史缺失或分支不匹配时明确报错，不推测生成替代摘要。

### 前台失败后的后台记忆费用控制

当标记为 `request_kind=turn` 的前台请求最终失败时，网关取消同一项目、API Key 和线程内正在运行的 `request_kind=memory` 请求，并暂停新的后台记忆请求 5 分钟。新请求在访问上游前返回 HTTP 400 和 `codex_memory_paused`；暂停期间仍可正常重试前台对话，新发起的前台请求成功后提前解除暂停。中间一次已恢复的渠道错误不会触发暂停，后台记忆自身失败不会延长暂停。

每次 WebSocket 帧内的任务元数据优先于连接建立时的 prewarm 元数据。其他线程、其他 API Key、子代理、搜索和压缩请求不因此被取消。该策略需要客户端提供明确的任务类型与线程身份；它不会永久关闭记忆功能，也不能退回上游已经处理的 token。暂停状态保存在当前网关进程的有限内存缓存中，重启后清除。

### Codex 0.154.0 兼容

网关原位保留 `configuration_update`，不为其补造消息 ID，并保留客户端传入的 `client_metadata.parent_response_id` 和 `guardian_credits_requested`。没有 Codex 身份信息的请求默认使用 0.154.0；客户端明确传入的身份信息仍优先。

附加配额数据保留上游报告的 `normal_model_slug` 元数据，不据此重映射请求模型。被动配额查询不声明 `x-openai-codex-luna-reserve: 1`；该能力头适用于能执行 Reserve 选择的客户端，见 [Codex 0.154.0 配额客户端](https://github.com/openai/codex/blob/rust-v0.154.0/codex-rs/backend-client/src/client/rate_limit_resets.rs)。

### Codex 0.162.x 兼容

网关在 Responses 转换和流聚合中保留 `partial_answer` 阶段及显式的 `end_turn: false`。一次推理完成不等于 Codex 当前轮次结束。放在 developer 输入消息中的基础指令、增量 `additional_tools` 声明和工具移除通知保持原有历史顺序。

网关生成的流终止错误使用 `response.failed`，合法重试时间位于 `response.error.headers`，也保留 WebSocket 嵌套错误中的重试提示。原有重试预算和硬额度处理继续生效。正常 Responses 错误转换保留提供商的 misalignment 详情及不透明 `review_target`；隐藏/自定义错误策略继续省略这些详情。

Codex 0.162.0 新增自定义提供商能力覆盖。如果所选 AxonHub 渠道支持原生压缩或已启用本地压缩桥接，可选用以下客户端配置启用 V2 `compaction_trigger` 协议：

```toml
[model_providers.axonhub-responses.capabilities]
remote_compaction = "v2"
external_web_access = true
```

`remote_compaction = "unsupported"` 表示在客户端执行压缩。`external_web_access` 控制 Codex 是否可以请求实时网页搜索，不授予上游搜索能力或账户权限；省略字段时保留客户端的提供商默认值。详见[官方能力定义](https://github.com/openai/codex/blob/rust-v0.162.0/codex-rs/model-provider-info/src/capabilities.rs)。

缺省 Codex 身份版本为 0.163.0，显式客户端身份和动态稳定版刷新继续优先。托管检查使用 0.162.1 / 0.163.0 × gpt-6-sol / gpt-6.1-sol 回环矩阵；两个版本均检查部分回答续传、developer 输入指令、网关失败帧的重试等待、加密 agent-message 传递、用量、并发和取消，不代表真实提供商账户可用性验证。

[0.163.0 版本](https://github.com/openai/codex/releases/tag/rust-v0.163.0)默认启用 instant steering，并在复用 WebSocket 前等待取消确认。网关会确认属于当前连接且已经结束的响应的迟到 interrupt，并排空在终止响应之后到达的确认；中继不返回确认或确认 ID 不匹配时淘汰该连接，保留终止响应和用量。网关同时保留可选的 `output.encrypted_content` 回放、`history_initialization` 元数据及上游模型目录字段。后台压缩和工具输出保留仍由客户端控制，不凭空开启模型或 Daybreak 权限。

#### Instant steering

Codex 0.162.0 默认启用已有的 `instant_interrupt` 客户端功能。在 Responses Lite WebSocket 模式下，客户端发送 `response.interrupt` 和 `mode: "discard_partial_items"`，读取被中断响应的终态后再携带新输入继续。AxonHub 已在当前连接转发该控制事件，保留已完成输出与用量，并从中断快照中排除未完成项目。HTTP 模式走已有取消路径；所选上游不支持主动中断时，网关不能替它实现同等能力。

公开的[轮内 steering API](https://developers.openai.com/api/docs/guides/steering) 仍只允许 `response.steer` 携带 `type`、`previous_response_id`、`input`，不应额外添加 `instant` 字段。公开 API 的排队 steering 与 Codex 客户端中断流程分别遵循各自的终态和工具结果处理规则。

### 常见问题
- **Codex 认证失败**：确认 `${HOME}/.codex/auth.json` 的 `personal_access_token` 是有效的 AxonHub API Key，并确认 `CODEX_AUTHAPI_BASE_URL`、`openai_base_url` 和 `chatgpt_base_url` 指向同一个 `/codex` 入口。
- **模型列表为空或不可用**：确认兼容开关已开启、已选择 Codex 渠道，且该渠道的 `/models` 接口通过设置页测试。目录过滤不代替推理授权。
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

## 搜索专用渠道

在 Codex 渠道的编辑窗口开启 **仅搜索** 后，该渠道只接收 `openai/search`（`/v1/alpha/search`）请求，不再参与 Responses HTTP、Responses WebSocket、Chat Completions、压缩或其他生成路由。关闭后恢复原有路由方式。

搜索专用渠道可在“搜索模型”中明确选择 GPT-5.6 及以上模型。例如，来自 `gpt-6-astra` 的搜索可固定使用 `gpt-6.1-sol`。选择“跟随请求模型”或未指定时，沿用当前对话在搜索请求中携带的模型；不会选取默认测试模型或其他任意模型。项目与 API Key 的渠道范围、标签及配额限制继续生效。

该设置不会让不支持搜索的上游获得搜索能力。搜索专用渠道的 `/alpha/search` 不可用时会返回错误或按既有重试策略尝试其他渠道，禁止在该渠道回退到 `/responses` 模拟搜索。

联网搜索策略提供“原生搜索”“使用其他渠道”和“仅 MCP”。对不支持独立搜索的生成渠道，选择“使用其他渠道”会在候选筛选时排除它，`openai/search` 直接选择其他符合项目/API Key 范围、标签与配额约束的渠道；没有可用渠道时立即返回无可用路由，不在已排除渠道上消耗重试次数。该设置保留正常对话及其搜索工具声明，不控制上游自行执行的 hosted search。“仅 MCP”还会移除生成请求中的原生搜索工具。前端不会同时启用“仅搜索”和“使用其他渠道”；外部写入的冲突配置以不接收独立搜索为准。

旧的“兼容搜索”配置升级后按原生搜索读取；已移除 GPT-5.5 Responses 模拟搜索、能力缓存和 Responses Lite 搜索工具注入及重试。原生搜索接口不可用时按现有跨渠道重试策略处理，不改发生成请求。
