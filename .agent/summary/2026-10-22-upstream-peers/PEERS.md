# 固定范围审查

起点：2026-10-21（星期三）20:02 北京时间 / 12:02 UTC。各 HEAD 于 2026-10-22 17:46 北京时间固定；未刷新范围。共 24 条记录，其中 3 条 sub2api 已审记录复用上轮结论，21 条本轮筛选；合并提交与分支提交不当成两个独立修复。按文件范围排除其他业务与文档，再对协议、流、工具、缓存及依赖候选读补丁并核对 AxonHub 实现，不是六个项目的全量安全审计。

- gpt-load: `dd33ae390c23575988b04b3f551873ae70f3eba0`（新增 1 条）
- OmniProxy: `56448d20ccd797bc32aae1c02a5e2f2030b8b6c8`（新增 0 条）
- new-api: `d8e65613c09adf3663f960ea4316414a99694751`（新增 13 条）
- codex2api: `1bdfc85cecff2559422fa4eb39fdd880bfc2c462`（新增 0 条）
- CLIProxyAPI: `0db19025571684b198a2ef508f99b41c717337e8`（新增 3 条）
- sub2api: `dba770d56a0ee0ae5bad1b3dc5e11614d323163c`（新增 4 条）

|项目/提交|内容|结论|依据|
|---|---|---|---|
|gpt-load [dd33ae39](https://github.com/tbphp/gpt-load/commit/dd33ae390c23575988b04b3f551873ae70f3eba0)|fix(ui): round credit balances up to whole points (#899)|范围不匹配|两套 Vue 订阅积分卡向上取整；本分支不是该订阅积分账户 UI。 |
|new-api [d8e65613](https://github.com/QuantumNous/new-api/commit/d8e65613c09adf3663f960ea4316414a99694751)|feat(web): copy model names from usage log details|不扩大范围|日志模型名复制按钮属于独立 UI 增强，不修复网关协议/历史/工具/路由兼容性。 |
|new-api [88021132](https://github.com/QuantumNous/new-api/commit/8802113263b8a94bf8957a1231b4ec04705d7dd9)|feat(task): let ordinary channels compete on shared plugin endpoints|不同架构|JavaScript 任务插件与普通渠道竞争同一路径；本仓库没有该插件注册/执行框架。 |
|new-api [eb169e74](https://github.com/QuantumNous/new-api/commit/eb169e74d772afdb281970d0cbe0b62fbf9fa994)|feat(pricing): adjust synced upstream prices by the site exchange rate|不同业务合同|New API 站点汇率、倍率表达式和价格同步 UI；不移植计费配置架构。 internal/server/biz/channel_price.go|
|new-api [e2321a5a](https://github.com/QuantumNous/new-api/commit/e2321a5a9f590e8398868cb7ef56af3d833faa07)|chore(web): sort locale keys|范围不匹配|另一项目 locale 排序；按变更文件筛除，未下载 API 省略的整份翻译。 |
|new-api [db80ac8e](https://github.com/QuantumNous/new-api/commit/db80ac8e0c4063e67938cf651c240c69f4f4cf54)|fix(task): let task plugins validate their own requests|不同架构|JS 任务插件请求校验归属与 usage 表达式合同；不引入另一套任务执行器。 |
|new-api [dbe97beb](https://github.com/QuantumNous/new-api/commit/dbe97beb9b06e8a4e23b5b8f009d882cc49f70bb)|feat(task): pass client request headers to task plugin drivers|不同架构|JS 驱动请求上下文增加过滤凭据的客户端头；本仓库没有该驱动上下文。 |
|new-api [556517fa](https://github.com/QuantumNous/new-api/commit/556517fa57d255c73428325f6a094fd8b4cab76c)|feat(task): accept multipart bodies on native plugin routes|不同架构|插件原生路由 multipart 解码合同；不移植其 JS 路由层。 |
|new-api [94cc3ff0](https://github.com/QuantumNous/new-api/commit/94cc3ff0906db37703d5ce2bc6a479209b7b9b23)|fix(billing): bill Claude cache writes that arrive without a 5m/1h split|适配采用|缓存写总量只有部分或没有 TTL 拆分时，补足默认 5m 部分；无变体配置保持共享价格；计费不修改原始用量。 internal/server/biz/cost_calc.go; internal/server/biz/cost_cache_split_test.go|
|new-api [7e659672](https://github.com/QuantumNous/new-api/commit/7e6596728ff640392bd77169ebb6137a8fe0c309)|fix(relay): stop cutting off slow clients during large stream events|适配采用|HTTP SSE/WS 写预算按排队字节累积；控制心跳遵循进行中的数据写期限；写入失败、刷新错误、取消、成功交付边界继续保留。 internal/server/api/stream_write_budget.go; internal/server/api/chat.go; internal/server/api/responses_websocket.go|
|new-api [0c2af978](https://github.com/QuantumNous/new-api/commit/0c2af9780e45f6f9073528fb0fcbdcd64b5d9495)|fix(relay): finish delivered Responses streams without false disconnect errors (#7956)|已有目标行为|SSE 写入/刷新及 WS WriteMessage 成功后才确认终态交付；Close/取消不能覆盖成功交付，EOF 未确认不算成功。 internal/server/api/stream_delivery.go; internal/server/api/chat.go:487; internal/server/api/stream_delivery_e2e_test.go|
|new-api [7d8b9126](https://github.com/QuantumNous/new-api/commit/7d8b91268dab69d7150f345c448b5e250163146a)|fix: upgrade Go to 1.26.10 for CVE-2026-111943 (#7942)|采用安全修复|本仓库已经使用 1.27 分支，升级到修复同一 CVE 的 1.27.3，不降到另一项目的 1.26.10。 .go-version; Dockerfile|
|new-api [4d36f2c5](https://github.com/QuantumNous/new-api/commit/4d36f2c5245dd6676075be1c4f668dfe56d3340a)|chore(deps-dev): bump vitest from 4.1.10 to 5.0.4 in /web (#7838)|不同工具链|本项 Vitest major 升级；本前端使用 node --test 和 Playwright，没有 Vitest 依赖。 frontend/package.json|
|new-api [eea05e01](https://github.com/QuantumNous/new-api/commit/eea05e010c7f212e1a617638fe27164f24d9478d)|chore(deps): bump github.com/klauspost/compress from 1.18.3 to 1.18.7 (#7931)|采用|官方 1.18.7 为 s2.NewDict 越界读安全修复；统一 root/llm/schema 模块依赖，CI tidy/校验。 go.mod; llm/go.mod; cmd/schema/go.mod|
|CLIProxyAPI [0db19025](https://github.com/router-for-me/CLIProxyAPI/commit/0db19025571684b198a2ef508f99b41c717337e8)|fix(codex): use original oauth credentials for alpha search|已有实现|Alpha Search 直接使用 OAuth AccessToken；不存在 SIWC 替换层。 llm/transformer/openai/codex/alpha_search.go:127|
|CLIProxyAPI [d232ee10](https://github.com/router-for-me/CLIProxyAPI/commit/d232ee109a1bab91a0b2aaff82cd48c42b9dc876)|docs(readme): add Chenju sponsor to README files|范围不匹配|赞助商文档与图片。 |
|CLIProxyAPI [009f8d7a](https://github.com/router-for-me/CLIProxyAPI/commit/009f8d7ae35f87937058e1d2989f400ee2e78114)|fix(thinking): preserve standalone Claude effort on Codex requests|已有目标行为|独立 output_config.effort 写入 ReasoningEffort 并保留原生元数据；原生回传不会因此强行启用 thinking。未声称与其全部参数优先级完全等价。 llm/transformer/anthropic/inbound_convert.go:420; llm/transformer/anthropic/outbound_convert.go:244|
|sub2api [dba770d5](https://github.com/Wei-Shaw/sub2api/commit/dba770d56a0ee0ae5bad1b3dc5e11614d323163c)|Merge pull request #8170 from StarryKira/fix/issue-8119-grok-fast-pricing|不同价格来源|AxonHub 从渠道价目/模型库解析价格，无 unknown-Grok 硬编码家族兜底；不复制另一项目价格表。 internal/server/biz/channel_price.go:183|
|sub2api [21cc9269](https://github.com/Wei-Shaw/sub2api/commit/21cc926933163e015d2b632be07055759c983541)|Merge pull request #8275 from apple050620312/fix/docker-go-1.27.3|去重采用|合并提交与 59100d32 为同一 Docker Go 更新，补丁只审一次。 .go-version; Dockerfile|
|sub2api [59100d32](https://github.com/Wei-Shaw/sub2api/commit/59100d324e9fafa6776182b3d3ea2b36e3edaf14)|fix(build): 对齐上游 Go 1.27.3 镜像版本|采用|官方已确认 Go 1.27.3；同步 CI/发布公共版本及 Docker。 .go-version; Dockerfile|
|sub2api [7da09cab](https://github.com/Wei-Shaw/sub2api/commit/7da09cab4e875463baa79fc150adde700c5438df)|chore: update sponsors|范围不匹配|赞助商文档与图片。 |
|sub2api [aa48a8bb](https://github.com/Wei-Shaw/sub2api/commit/aa48a8bbd80c6ac956dbcbaba651f417b3cf78e3)|Merge pull request #8255 from wucm667/fix/issue-8248-cloudflare-json-403|复用上轮结论|见 ../2026-10-21-upstream-64763/PEERS.md；未复查补丁。 |
|sub2api [888c6057](https://github.com/Wei-Shaw/sub2api/commit/888c6057333e011b8cfeb81a867f445d229d1432)|Merge pull request #8188 from wucm667/fix/issue-8187-tool-schema-constants|复用上轮结论|见 ../2026-10-21-upstream-64763/PEERS.md；未复查补丁。 |
|sub2api [5e45fa48](https://github.com/Wei-Shaw/sub2api/commit/5e45fa482b186145b84ddb2d79bc93a509dba1d2)|Merge pull request #8224 from wucm667/fix/issue-8222-deepseek-image-size|复用上轮结论|见 ../2026-10-21-upstream-64763/PEERS.md；未复查补丁。 |

官方核验：[Go 1.27.3 / 1.26.10 安全公告](https://groups.google.com/g/golang-announce/c/66pojueaQek)；[compress v1.18.7](https://github.com/klauspost/compress/releases/tag/v1.18.7)；[Anthropic 缓存 TTL](https://platform.claude.com/docs/en/build-with-claude/prompt-caching)。时间预算按字节适配采用 new-api 思路，未复制其网关框架。
