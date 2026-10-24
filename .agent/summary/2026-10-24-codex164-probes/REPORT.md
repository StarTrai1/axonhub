# 2026-10-24 Codex 请求、探测与过载重试修复

## 证据与处理

从 dev `b46b8ddf` 创建 `fix/codex164-probe-errors-20261024`，PR #13。依据只读 SQLite 在线快照，quick_check=ok；八条目标请求的结构、错误及执行次数见 request-evidence.json。失败记录没有保存原始上游错误响应体，因此错误参数以已持久化的 error_message 为限。

1. **agent #67515/#67519/#67521**：每条只有一次上游执行，HTTP 400，明确拒绝 `input[0].internal_chat_message_metadata_passthrough.content_item_metadata`。日志客户端为 `codex_vscode/0.164.0-alpha.11.1`，Version 为 0.164.0。最终上游请求确实带此字段。既有恢复可以解析结构化 error.param，但从错误 message 提取路径时仅识别 `.status`。现解析完整路径，再走严格的字段与索引验证，复用有次数限制的同渠道 metadata 兼容重试及渠道/地址/模型/凭据范围内的记忆。拒绝冲突索引、语义字段及不支持的深层路径，不修改内容、工具参数、reasoning 密文或 compaction。

2. **keepalive any #67455**：HTTP 400 的持久化错误明确带 `[param=tools]`。实际是 `codex_exec/0.164.0`，声明了 Code Mode exec/wait、用户询问、clock 和 collaboration 等工具。旧脚本功能开关不足以覆盖模型目录中指定的 tool_mode 和 experimental_supported_tools。为每次题库探测建立独立无工具模型目录，保留实际模型 ID 和本阶段 effort，使用简短答题指令；禁用交互、时钟、目标管理等工具功能。目录沿用 attempt 清理，不动用户常用配置。仍由真实官方 CLI 发请求；客户端自身 retries=0，网关内部重试保留。

3. **any 测活 #67464–#67466**：三条均为原生 Responses 测试请求，HTTP 400 `invalid codex request`。请求只带 input/instructions/model/parallel_tool_calls/store/stream/tools，parallel_tool_calls=true，没有 reasoning.effort、tool_choice 或 text.verbosity。当前原生探测入口绕过既有 Codex 请求构造，旧模型列表也遗漏 GPT-6 Sol/Luna/6.1 Sol。现统一使用共享 GPT-6 判断，原生探测复用测试身份，携带 low effort、low verbosity、空工具、显式 auto 及 parallel=false；传输层保留显式 auto。上游没有指出此类错误的具体字段，所以不能把其中一个差异单独断言为唯一原因。回归覆盖实际入口和 Codex relay 最终请求。

4. **any #67457**：唯一执行 198922 返回 HTTP 500，`当前模型 gpt-6.1-sol 负载已经达到上限，请稍后重试`。读取到的全局策略为 enabled=true、max_single_channel_retries=10、max_channel_retries=6、retry_delay_ms=1000。代码在 CanRetry 和本地压缩开流重试中有过载的一票否决，造成一次请求即结束。按本轮明确要求恢复临时 500/503 过载的有界重试：有其他物理渠道时优先切换，最后渠道按现有次数预算重试；退避为 2/4/8/16/32 秒、之后封顶 32 秒，附 0–500ms 抖动，并尊重上游 Retry-After。长配额等待、余额耗尽、Provisioned-Managed 429 和取消仍停止。不会在流已交付输出后重新生成。

## 源码依据与验证范围

Codex 官方 rust-v0.164.0 的 `codex-rs/core/src/tools/mod.rs` 确认 model.tool_mode 优先于功能开关；`tools/spec_plan.rs` 确认模型声明的时钟/交互工具与部分功能开关相互独立。`codex-rs/core/config.schema.json` 与 `codex-rs/protocol/src/openai_models.rs` 用于独立目录格式。来源：https://github.com/openai/codex/tree/rust-v0.164.0/codex-rs 。未修改或安装 Codex 本身。

编译、测试、lint 和格式化仅在 GitHub CI。新增了错误 message 解析边界、原生探测最终请求、过载实际执行次数/预算/取消，以及真实 Codex 0.162.1/0.163.0/0.164.0 请求工具列表断言。首轮 CI 发现测试使用了错误的 pipeline.Result 字段名，并发现 goal 工具仍由默认功能启用；均已修正，保留严格无工具断言。已有“过载只执行一次”的旧测试随新要求更新。

当前报告不代表最终 CI 或线上验收。最终 SHA、检查链接、标签和 GoReleaser 启动收据写入本目录 DELIVERY.md / DELIVERY.json。按既定流程不进行付费推理、生产配置/数据库修改、EXE 安装或服务重启。
