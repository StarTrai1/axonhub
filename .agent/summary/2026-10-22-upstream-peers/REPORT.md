# 上游与同行集成

状态：实现已合并，等待 GitHub CI 验证与源代码生成；尚未发布。

## 上游 9 条提交

|提交|处理|
|---|---|
|48984732|OpenAI 渠道允许 SystemOne；移植混合端点默认选 Chat 规则，保留原生 Codex/Decisions/SystemOne 测活及协议优先级。|
|b7cf0524|接入请求/用量指标；指标终态去重与本分支手动换渠道注册表并存。|
|8bd3e931|无 tools 时移除 tool_choice；与 Codex tools:[] 的传输最终化修复并存，不恢复旧 Lite 猜测。|
|19862baf|补齐 Gemini OpenAI 并行工具流缺失 index，避免多调用聚合为同一个。|
|c06e896e|arguments.done 不重复发送工具 ID/type/name，避免下游拼接重复。|
|090e542e|按渠道覆盖重试次数；0 禁用、nil 继承；全局关闭与容量耗尽 veto 优先，手动切换预算保留。|
|a14d1013|服务端设置 patch + 版本冲突合并；补入本分支搜索、传输与 maxSingleChannelRetries 字段，确保前端新接口可用。|
|f30e3c9b|按 Antigravity 模型池筛选配额，避免 Claude/GPT 耗尽阻断 Gemini。|
|71bdd328|上游团队 README 更新。|

不是上游全部已实现：以上新增实现整体合入，测活冲突使用本分支覆盖更完整的原生实现。同行逐提交结论见 PEERS.md，固定 SHA 见 peers-ledger.json。

## 同行适配

- 大型 SSE/WS 事件按字节给写入预算，跨事件保留待排空时间，沿用交付确认和错误传播。
- 缓存写入总量与 TTL 拆分不齐时补齐默认 5m 计费；无变体仍使用共享价格，不改写原始 usage。
- Go 工具链 1.27.2 → 1.27.3；三个模块 compress → 1.18.7。

新增回归：大型 SSE/WS、预算排空、缓存 total-only/partial/full/split-only、设置 patch GraphQL/服务持久化、容量 veto 与渠道 retry override、显式测活协议优先级。既有协议、工具、历史、隔离、Windows 与并发 CI 继续验证。
