# Peer decisions — 2026-11-05

Each row describes one frozen new commit. Screening is not a claim that every changed line was inspected. Selected patch depth and owning-source evidence are recorded per row.

## upstream: 0 new records

`8a9df52283d9d9c3909c839267ce890a2913231e` → `8a9df52283d9d9c3909c839267ce890a2913231e`; frozen `2026-11-04T17:13:23.402711+00:00`.


## sub2api: 19 new records

`4a4a83ee5ec86b19d3a2dd9b6471de5128963bec` → `876b96d204855ad1aac486b7dd39a95d377d4760`; frozen `2026-11-04T17:13:22.945804+00:00`.

- [01a1a78db2](https://github.com/Wei-Shaw/sub2api/commit/01a1a78db2c3ce88565d924c0804a40062472221) fix(antigravity): neutralize stale system identities on forwarding
  Decision: **retain-request-instructions**. Do not neutralize or strip client system identity preambles based on text heuristics. Existing native instructions and cache prefixes remain owned by the caller.
  Depth: title and file inventory. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [835803a702](https://github.com/Wei-Shaw/sub2api/commit/835803a7021c3255c6f19019329de923085f5f47) fix(antigravity): preserve task-bearing identity preambles
  Decision: **retain-request-instructions**. Do not neutralize or strip client system identity preambles based on text heuristics. Existing native instructions and cache prefixes remain owned by the caller.
  Depth: title and file inventory. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [e47abd8f9b](https://github.com/Wei-Shaw/sub2api/commit/e47abd8f9bc958949989958f0fe4cb59ff3339c5) fix(usage): 用户导出 CSV 每页取 1000 条，遇面板限流 429 按 Retry-After 等待后重试
  Decision: **different-export-client**. Peer paginated CSV export retries its panel 429s using Retry-After. Axon frontend/export implementation differs; no matching failed export established.
  Depth: title and file inventory. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [522f1fb999](https://github.com/Wei-Shaw/sub2api/commit/522f1fb999db5380dc88724a71098c7902b094fc) fix(grok): normalize delegated message input for Responses
  Decision: **defer-agent-message-conversion**. Peer flattens delegated agent_message content to a user message for Grok. Axon does not establish this private item schema/role contract here; avoid guessing role and discarding structured metadata.
  Depth: selected grok_model_input.go patch. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [e452e41e40](https://github.com/Wei-Shaw/sub2api/commit/e452e41e4010b48028e16e5047540974e1b4b78d) feat(openai): 生图账号按 1K/2K/4K 尺寸档位分流
  Decision: **defer-routing-feature**. Image-size account tiers and overflow-affinity restoration are a new scheduler feature with no matching Axon tier state. Keep configured channel/model routing.
  Depth: title and file inventory. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [a699c2baed](https://github.com/Wei-Shaw/sub2api/commit/a699c2baedc886a6970997cf6b362e4f46f354b0) fix(openai): 透传放行空 instructions，响应回显按客户端原值还原
  Decision: **retain-response-provenance**. Peer rewrites echoed response.instructions to the original client value after gateway modifications. Preserve native response provenance instead of adding a new echo-rewrite policy without an Axon failing fixture.
  Depth: selected instructions_echo.go patch. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [6b565e45a6](https://github.com/Wei-Shaw/sub2api/commit/6b565e45a6d8705bbfdf8f0e847f4bb7812f074a) fix(openai): 生图尺寸溢出选号后按首轮前的绑定处理会话粘性
  Decision: **defer-routing-feature**. Image-size account tiers and overflow-affinity restoration are a new scheduler feature with no matching Axon tier state. Keep configured channel/model routing.
  Depth: title and file inventory. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [17772947bd](https://github.com/Wei-Shaw/sub2api/commit/17772947bda617439f20300f9930b4010d64d8a8) fix(kimi): use short cooldown for concurrency-limit 403
  Decision: **retain-rate-limit-policy**. Peer replaces its hardcoded concurrency-403 cooldown with its 429 fallback scheduler. No identical scheduler or failing Kimi fixture established in Axon; keep current retry/cooldown contract.
  Depth: selected ratelimit_cn_providers.go patch. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [45b51a8ac4](https://github.com/Wei-Shaw/sub2api/commit/45b51a8ac4e2096fa440788953b2038226bfc739) Merge pull request #8465 from wucm667/fix/issue-8461-grok-message-input
  Decision: **merge-record**. Merge bookkeeping; constituent new commits in the frozen range are recorded separately, without rereading their patches.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [ccc2c161b5](https://github.com/Wei-Shaw/sub2api/commit/ccc2c161b5121d16fede588004ed05eb92d96f1b) Merge pull request #8503 from wucm667/fix/issue-8502-kimi-concurrency-cooldown
  Decision: **merge-record**. Merge bookkeeping; constituent new commits in the frozen range are recorded separately, without rereading their patches.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [393167ea9a](https://github.com/Wei-Shaw/sub2api/commit/393167ea9a444f9f22972453686b53e993f90cd4) Merge pull request #8436 from feeeei/fix/usage-export-rate-limit
  Decision: **merge-record**. Merge bookkeeping; constituent new commits in the frozen range are recorded separately, without rereading their patches.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [e320a1500b](https://github.com/Wei-Shaw/sub2api/commit/e320a1500be3eb546f85ab29876e38f9841c51df) Merge pull request #8318 from wucm667/fix/issue-8313-antigravity-system-identity
  Decision: **merge-record**. Merge bookkeeping; constituent new commits in the frozen range are recorded separately, without rereading their patches.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [ee74473e03](https://github.com/Wei-Shaw/sub2api/commit/ee74473e038b5c956897cda7aeb9903f3fa3138a) Merge pull request #8499 from feeeei/fix/openai-passthrough-instructions-echo
  Decision: **merge-record**. Merge bookkeeping; constituent new commits in the frozen range are recorded separately, without rereading their patches.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [6f92090d7f](https://github.com/Wei-Shaw/sub2api/commit/6f92090d7f540efd88cd4b81e651120cb36e3ff5) Merge pull request #8462 from feeeei/feat/openai-image-size-tier-routing
  Decision: **merge-record**. Merge bookkeeping; constituent new commits in the frozen range are recorded separately, without rereading their patches.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [721dfb282e](https://github.com/Wei-Shaw/sub2api/commit/721dfb282e3bd94b513ad6d2d65dcb27df246369) feat(auth): 注册邮箱精确黑名单，保存时可批量禁用命中的已注册用户
  Decision: **defer-registration-policy**. Exact-email blacklisting and bulk disabling existing users are new administrative policy and write flows; no matching correctness fix to port.
  Depth: title and file inventory. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [b9d6ff5ca2](https://github.com/Wei-Shaw/sub2api/commit/b9d6ff5ca2b2d34afcc4f6281de7beddd63f9959) Merge pull request #8504 from feeeei/feat/registration-email-blacklist
  Decision: **merge-record**. Merge bookkeeping; constituent new commits in the frozen range are recorded separately, without rereading their patches.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [e353c83226](https://github.com/Wei-Shaw/sub2api/commit/e353c832261c9358949e2071bfb17e99d36aa8cc) chore: update sponsors
  Decision: **project-maintenance**. Peer sponsorship/build/release maintenance does not apply to Axon delivery.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [2510944363](https://github.com/Wei-Shaw/sub2api/commit/251094436325175c9e0ab06e7b962d05369fd766) chore: update sponsors
  Decision: **documentation-only**. Documentation, planning or empty commit; no production gateway change.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [876b96d204](https://github.com/Wei-Shaw/sub2api/commit/876b96d204855ad1aac486b7dd39a95d377d4760) chore: update sponsors
  Decision: **project-maintenance**. Peer sponsorship/build/release maintenance does not apply to Axon delivery.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.

## CLIProxyAPI: 28 new records

`6e6afab99a1939525ecb6bb1f0873c8e7a9df877` → `78a9cc9aaa8ae90a66300d9cbddfe7c894307622`; frozen `2026-11-04T17:13:23.219418+00:00`.

- [e582706557](https://github.com/router-for-me/CLIProxyAPI/commit/e582706557b22a4deeaa640f7d465432f7d91a0c) fix(auth): prevent auto-refresh spin for disabled credentials with rejected refresh tokens
  Decision: **covered-policy**. Existing refresh rejection tracking binds rejection to the exact refresh-token digest and client; the peer disabled-auth scheduler does not exist here.
  Depth: selected patch and owning-source comparison. Evidence: llm/oauth/refresh_rejection.go; llm/oauth/token_provider.go.
- [0130d6f38c](https://github.com/router-for-me/CLIProxyAPI/commit/0130d6f38c91e9bb3980484b8a2ef83be71c0a01) fix(claude): keep Fable-only 429 rejections model-scoped when 5h headers are omitted
  Decision: **different-quota-scope**. Peer treats a Fable 429 as model-scoped without 5h headers. No matching Axon quota-header fixture proves the same account/model attribution; retain existing scoped quota decisions.
  Depth: title, parent and file inventory. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [e25b7dafa0](https://github.com/router-for-me/CLIProxyAPI/commit/e25b7dafa0ea64cb91850d846707f7da731de91e) fix(devin): sanitize embedded exec tool phrases and stop retrying request-caused 403 errors
  Decision: **different-provider**. Devin-specific translation/client lifecycle has no matching Axon provider integration.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [24b0454811](https://github.com/router-for-me/CLIProxyAPI/commit/24b0454811d263af2a0facb6b478a03d5d27b582) fix(antigravity): map safety and filter finish reasons to refusal in claude response translator
  Decision: **covered-partial**. Existing Gemini blocked-finish conversion and Anthropic content_filter-to-refusal handling cover safety refusals. Keep malformed-tool error handling instead of mapping every malformed response to a refusal.
  Depth: selected patch and owning-source comparison. Evidence: llm/transformer/gemini/outbound_convert.go; llm/transformer/anthropic/inbound_stream.go.
- [d38997717d](https://github.com/router-for-me/CLIProxyAPI/commit/d38997717dd38a328bca0b2b0ce3fe8e41a84c8d) fix(management): honor targeted OAuth re-login across all built-in providers
  Decision: **different-auth-lifecycle**. Targeted OAuth re-login and peer auth-file lifecycle do not share Axon channel credential structures.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [a1eeb60ec3](https://github.com/router-for-me/CLIProxyAPI/commit/a1eeb60ec3f70dd20da774e7ea667e2d0bfceaea) fix(codex): disable supports_image_detail_original for non-codex catalog routes
  Decision: **defer-catalog-capability**. Peer conditionally disables image_detail_original on synthesized non-Codex catalog routes. Axon intersects an upstream catalog and retains native fields; no matching synthesized capability defect established.
  Depth: title/file inventory and source contract. Evidence: internal/server/biz/codex_catalog.go.
- [9acf81d3a6](https://github.com/router-for-me/CLIProxyAPI/commit/9acf81d3a6732f541bb40d0247f8ea46b5aa3ef6) fix(claude): drop replayed web search citations when search results are missing
  Decision: **retain-history-citations**. Peer removes replayed citations when search results are absent. Preserve native history and citations unless a matching rejection and safe reconstruction contract are established.
  Depth: title, parent and file inventory. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [dde469d3c2](https://github.com/router-for-me/CLIProxyAPI/commit/dde469d3c2cf7f7ee4dce9b519b0b087b7132dbc) fix(pluginhost): isolate unix plugin dlopen crashes with a preflight subprocess probe
  Decision: **different-plugin-runtime**. CPA dynamic plugin load, quiesce and credential registration are not Axon runtime components.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [54778bd30d](https://github.com/router-for-me/CLIProxyAPI/commit/54778bd30de531eddaab41884bb124029a0f0751) fix(agents): enforce usage of moego libraries for JSON and Base64 operations
  Decision: **documentation-only**. Documentation, planning or empty commit; no production gateway change.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [6d4102f33f](https://github.com/router-for-me/CLIProxyAPI/commit/6d4102f33f1037988330d2383a950bc6ffa1001f) feat(config): support native-entrypoints configuration for Claude Code clients
  Decision: **different-client-configuration**. A new Claude native-entrypoints configuration and device-profile detection belong to the peer client-emulation layer; no Axon request defect established.
  Depth: title, parent and file inventory. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [04208efa8b](https://github.com/router-for-me/CLIProxyAPI/commit/04208efa8b52767e228570dac90c93dde5e25fcb) fix(pluginhost): preserve disabled auth state across plugin refreshes
  Decision: **different-plugin-runtime**. CPA dynamic plugin load, quiesce and credential registration are not Axon runtime components.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [be86430518](https://github.com/router-for-me/CLIProxyAPI/commit/be8643051801879ac5b7147604b1d8db4bae73a6) fix(translator/gemini): nest Claude tool_result images inside functionResponse.parts
  Decision: **covered-superset**. Axon already nests inline tool images/PDFs for Gemini 3+ through placeGeminiToolMedia and retains older-model/out-of-band media handling and parallel tool grouping.
  Depth: selected patch and owning-source comparison. Evidence: llm/transformer/gemini/tool_media.go; llm/transformer/gemini/tool_result_error_test.go.
- [b3442f6544](https://github.com/router-for-me/CLIProxyAPI/commit/b3442f6544c57b29ba809cfcba7733b0729022a6) fix(devin): keep sealed reasoning items sent back via the Responses API
  Decision: **different-provider**. Devin-specific translation/client lifecycle has no matching Axon provider integration.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [193debb81d](https://github.com/router-for-me/CLIProxyAPI/commit/193debb81d92a72313662e99008fc37e4c3e6a80) Merge pull request #7229 from maomaocun/fix/devin-responses-reasoning-passthrough
  Decision: **merge-record**. Merge bookkeeping; constituent new commits in the frozen range are recorded separately, without rereading their patches.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [cfa25b4a0a](https://github.com/router-for-me/CLIProxyAPI/commit/cfa25b4a0a80b52cfee1a84f5a87ebd79def6c15) refactor(translator): build Devin thought step with a single ojson.Doc
  Decision: **different-provider**. Devin-specific translation/client lifecycle has no matching Axon provider integration.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [01b24f1f95](https://github.com/router-for-me/CLIProxyAPI/commit/01b24f1f95dc9b309765212c52b25b25e005ff01) fix(codex): preserve and forward max_output_tokens across request translators
  Decision: **defer-contract-change**. Peer now forwards max_output_tokens to its Codex backend. Axon has explicit tests for blocking these fields on native Codex HTTP/WebSocket paths. No official Codex endpoint contract or Axon provider fixture was established to overturn that compatibility policy; do not infer backend support from the public Responses API.
  Depth: selected patch and owning-source comparison. Evidence: llm/transformer/openai/codex/outbound.go; llm/transformer/openai/codex/outbound_executor_test.go.
- [6d7e6a3bb9](https://github.com/router-for-me/CLIProxyAPI/commit/6d7e6a3bb9da6f53bac316cea17e8061002cee7d) feat(usage): extract cached image/audio and interactions modality tokens
  Decision: **defer-partial-usage-expansion**. Axon already carries aggregate cached tokens and prompt/completion modality details. Additional cached modality aliases and Google Interactions require end-to-end schema/billing support; no Interactions adapter is introduced in this patch. Coverage is partial, not a superset.
  Depth: selected usage_helpers.go patch and owning-source comparison. Evidence: llm/model.go; llm/transformer/gemini/convert.go.
- [fb30a9e8bb](https://github.com/router-for-me/CLIProxyAPI/commit/fb30a9e8bb4ac06c2748da667515ad81920aca4f) fix(thinking): prefer Claude mid-conversation effort directives in usage extraction
  Decision: **defer-beta-directives**. Peer reads mid-conversation system output_config directives for usage/model selection. Axon currently converts top-level effort; native preservation does not prove cross-protocol support for this beta. Defer a coherent directive conversion until its contract and fixtures are established.
  Depth: selected apply.go and directive patch. Evidence: llm/transformer/anthropic/inbound_convert.go.
- [061ffe6fbc](https://github.com/router-for-me/CLIProxyAPI/commit/061ffe6fbc222b43e7bfd39fc77536e3b1e93858) fix(devin): honor Claude mid-conversation effort directives when selecting model variants
  Decision: **different-provider**. Devin-specific translation/client lifecycle has no matching Axon provider integration.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [606e667a8e](https://github.com/router-for-me/CLIProxyAPI/commit/606e667a8ef17d05b4aa18bb52b262b5252f70e4) fix(codex): sanitize invalid characters in Codex input item IDs
  Decision: **retain-native-identities**. Do not globally rewrite native Codex item IDs from peer character heuristics. Retain exact identities and existing rejection-scoped recovery.
  Depth: title, parent and file inventory. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [6e10359968](https://github.com/router-for-me/CLIProxyAPI/commit/6e10359968392c5fef1483cb091393d5fe10aeac) fix(pluginhost): abort hot reload on quiesce failure and coordinate usage replay
  Decision: **different-plugin-runtime**. CPA dynamic plugin load, quiesce and credential registration are not Axon runtime components.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [196ca839aa](https://github.com/router-for-me/CLIProxyAPI/commit/196ca839aa4df0cc04c546a16ae60af3c16d48fb) Merge pull request #7231 from sususu98/feat/usage-cached-modality-tokens
  Decision: **merge-record**. Merge bookkeeping; constituent new commits in the frozen range are recorded separately, without rereading their patches.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [f72f8bfd6a](https://github.com/router-for-me/CLIProxyAPI/commit/f72f8bfd6a1c7d69ca371dd4a7bdf490db50c154) feat(antigravity): bump fallback version to 2.29.0 and add changelist mapping
  Decision: **defer-client-version**. Do not change the Antigravity fallback identity/version solely to match a peer release; no negotiated version failure was established.
  Depth: title, parent and file inventory. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [f3762c64de](https://github.com/router-for-me/CLIProxyAPI/commit/f3762c64de51a0d111e073a14288cf0981f7afcb) feat(auth): extract Antigravity model discovery to sdk/auth
  Decision: **different-sdk**. Antigravity model discovery extraction and ojson ownership/nil corrections apply to CPA SDK model structures, not Axon model discovery.
  Depth: title, parent and file inventory. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [f49bd5d91a](https://github.com/router-for-me/CLIProxyAPI/commit/f49bd5d91ac99cff4be1b94c3ad12e63ee946744) refactor(auth): route Antigravity model JSON operations through ojson
  Decision: **different-sdk**. Antigravity model discovery extraction and ojson ownership/nil corrections apply to CPA SDK model structures, not Axon model discovery.
  Depth: title, parent and file inventory. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [cbf0188eff](https://github.com/router-for-me/CLIProxyAPI/commit/cbf0188effe5f93d448cac38124ed9cf431e1649) Merge pull request #7238 from sususu98/feat/antigravity-models-discovery-sdk-auth
  Decision: **merge-record**. Merge bookkeeping; constituent new commits in the frozen range are recorded separately, without rereading their patches.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [0e02765ccf](https://github.com/router-for-me/CLIProxyAPI/commit/0e02765ccfceefef7f502620c71cdbdf1b7a4ce1) fix(auth): clone parsed model IDs and preserve nil unknown catalogs
  Decision: **different-sdk**. Antigravity model discovery extraction and ojson ownership/nil corrections apply to CPA SDK model structures, not Axon model discovery.
  Depth: title, parent and file inventory. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [78a9cc9aaa](https://github.com/router-for-me/CLIProxyAPI/commit/78a9cc9aaa8ae90a66300d9cbddfe7c894307622) chore(deps): update github.com/router-for-me/moego to v0.1.2
  Decision: **different-dependency**. Axon does not use the peer moego JSON engine; no reason to migrate payload semantics/dependencies.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.

## OmniProxy: 0 new records

`4bcee95bf1831e2f70de29281ef23a207de6e146` → `4bcee95bf1831e2f70de29281ef23a207de6e146`; frozen `2026-11-04T17:13:23.006703+00:00`.


## codex2api: 60 new records

`231c597dd0290dc7751caa80ed63a3bfcf55848f` → `a4bbb455ba7ae9f4e6e02e6de2cae24fe12e6212`; frozen `2026-11-04T17:13:23.117462+00:00`.

- [9baf6a341d](https://github.com/james-6-23/codex2api/commit/9baf6a341d58c1a2120d7fab00247c2173a0288d) docs(outputguard): 添加模型输出失控防护设计文档、实施计划与事故报告
  Decision: **documentation-only**. Documentation, planning or empty commit; no production gateway change.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [ece3a87a93](https://github.com/james-6-23/codex2api/commit/ece3a87a93c5b0366e68d225856d05b28a3a1dd6) feat(outputguard): 实现模型输出失控防护
  Decision: **defer-output-guard**. Peer output-repetition/CPU guard and its settings transaction follow-ups implement a new output cutoff policy. No corresponding Axon incident or detector contract; do not import partial guard lifecycle/DB state.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [7662fd8679](https://github.com/james-6-23/codex2api/commit/7662fd8679bea4cf29a1a87410f353f41fc117d3) feat(frontend): 设置页补充失控防护的 CPU 开销与使用注意事项
  Decision: **different-usage-ui**. Public key concurrency/usage views and account settings UI use codex2api-specific APIs and state. No equivalent Axon defect; retain frontend style and scope.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [e42b36f531](https://github.com/james-6-23/codex2api/commit/e42b36f531b37d4c1ad1d8d2ea1a467b1659e744) fix(outputguard): scan post-end observations and use collision-resistant snapshot digest
  Decision: **defer-output-guard**. Peer output-repetition/CPU guard and its settings transaction follow-ups implement a new output cutoff policy. No corresponding Axon incident or detector contract; do not import partial guard lifecycle/DB state.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [8d136f25b4](https://github.com/james-6-23/codex2api/commit/8d136f25b49a0595ade96ea0cfce1bf7f01f3d99) fix(proxy): project incomplete responses and scan early anthropic/gemini content
  Decision: **defer-output-guard**. Peer output-repetition/CPU guard and its settings transaction follow-ups implement a new output cutoff policy. No corresponding Axon incident or detector contract; do not import partial guard lifecycle/DB state.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [6eafe1f163](https://github.com/james-6-23/codex2api/commit/6eafe1f163c9d3edf90717334cb52d339c81cfa2) fix(proxy,frontend): tighten sse line check, skip ws flush after guard trip, restore confirmed settings
  Decision: **defer-output-guard**. Peer output-repetition/CPU guard and its settings transaction follow-ups implement a new output cutoff policy. No corresponding Axon incident or detector contract; do not import partial guard lifecycle/DB state.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [431bed92e0](https://github.com/james-6-23/codex2api/commit/431bed92e027e8bb3f78a0817e5dc07f63123fd7) fix(admin): keep output guard persistence consistent with settings save
  Decision: **defer-output-guard**. Peer output-repetition/CPU guard and its settings transaction follow-ups implement a new output cutoff policy. No corresponding Axon incident or detector contract; do not import partial guard lifecycle/DB state.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [60b77213b2](https://github.com/james-6-23/codex2api/commit/60b77213b21edf52b9d99ba9d38064ffffa2e21d) fix(oauth): 对齐刷新请求编码并移除过时 UA
  Decision: **retain-oauth-identity**. Peer refresh encoding/client-attestation and maintenance identity changes belong to codex2api credential storage. Preserve Axon provider client identity and grant-scoped refresh contract without a matching refresh failure.
  Depth: title and file inventory. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [4256e36d64](https://github.com/james-6-23/codex2api/commit/4256e36d6410f1109f4d23c42deb32cab27626fc) refactor(codex): 移除维护身份开关并统一客户端解析
  Decision: **retain-oauth-identity**. Peer refresh encoding/client-attestation and maintenance identity changes belong to codex2api credential storage. Preserve Axon provider client identity and grant-scoped refresh contract without a matching refresh failure.
  Depth: title and file inventory. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [dd529628ec](https://github.com/james-6-23/codex2api/commit/dd529628ec28f965e28d30537198d33ea8722541) fix(oauth): 对齐客户端身份与刷新上下文
  Decision: **retain-oauth-identity**. Peer refresh encoding/client-attestation and maintenance identity changes belong to codex2api credential storage. Preserve Axon provider client identity and grant-scoped refresh contract without a matching refresh failure.
  Depth: title and file inventory. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [3512e527f8](https://github.com/james-6-23/codex2api/commit/3512e527f841588b761db01bbeba2132c0b7fe92) chore(deps): bump golang.org/x/net to v0.62.0 (GO-2026-6860)
  Decision: **covered-dependency**. Root and LLM modules already pin golang.org/x/net v0.62.0; no dependency downgrade or duplicate upgrade.
  Depth: title/file inventory and previous committed dependency evidence. Evidence: go.mod; llm/go.mod.
- [44ff33a9a4](https://github.com/james-6-23/codex2api/commit/44ff33a9a46c9bb3923453b2d63620cf060cd8e1) fix(frontend): keep guard fields on autosave only and avoid rounding unedited limits
  Decision: **defer-output-guard**. Peer output-repetition/CPU guard and its settings transaction follow-ups implement a new output cutoff policy. No corresponding Axon incident or detector contract; do not import partial guard lifecycle/DB state.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [49175cbacc](https://github.com/james-6-23/codex2api/commit/49175cbaccd8c2a61e6b80e6bdd7147a6e3e5ab2) fix(proxy): count sse join separators, record cold-start load failures, reject zero guard cpu
  Decision: **defer-output-guard**. Peer output-repetition/CPU guard and its settings transaction follow-ups implement a new output cutoff policy. No corresponding Axon incident or detector contract; do not import partial guard lifecycle/DB state.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [cad0972b29](https://github.com/james-6-23/codex2api/commit/cad0972b29b3391b1bc0bebad0d0e960c026930b) fix(admin): surface output guard read failures and validate updates inside transaction
  Decision: **defer-output-guard**. Peer output-repetition/CPU guard and its settings transaction follow-ups implement a new output cutoff policy. No corresponding Axon incident or detector contract; do not import partial guard lifecycle/DB state.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [4870171e9b](https://github.com/james-6-23/codex2api/commit/4870171e9bea6c621cfcdf18675e22d4e38466eb) fix(proxy): share one output guard across native grok and messages retry attempts
  Decision: **defer-output-guard**. Peer output-repetition/CPU guard and its settings transaction follow-ups implement a new output cutoff policy. No corresponding Axon incident or detector contract; do not import partial guard lifecycle/DB state.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [cbd7a80289](https://github.com/james-6-23/codex2api/commit/cbd7a802890ceca0799609b932c3852946ebf2a6) fix(proxy): guard reasoning summary deltas and snapshot non-stream gemini without finish reason
  Decision: **defer-output-guard**. Peer output-repetition/CPU guard and its settings transaction follow-ups implement a new output cutoff policy. No corresponding Axon incident or detector contract; do not import partial guard lifecycle/DB state.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [9f135d35fc](https://github.com/james-6-23/codex2api/commit/9f135d35fcede34f4121b92234303ce57d246858) fix(outputguard): scope channel identities per retry attempt
  Decision: **defer-output-guard**. Peer output-repetition/CPU guard and its settings transaction follow-ups implement a new output cutoff policy. No corresponding Axon incident or detector contract; do not import partial guard lifecycle/DB state.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [591b7aac97](https://github.com/james-6-23/codex2api/commit/591b7aac9782a1c7d41eea969aa11d57bed91648) test(proxy): make scenario 25 linearity gate robust to ci noise
  Decision: **defer-output-guard**. Peer output-repetition/CPU guard and its settings transaction follow-ups implement a new output cutoff policy. No corresponding Axon incident or detector contract; do not import partial guard lifecycle/DB state.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [344493e302](https://github.com/james-6-23/codex2api/commit/344493e302c59ca76e4a2c275913e13ca4116b44) fix(frontend): guard confirmed baseline versions and keep guard fields out of manual save response
  Decision: **defer-output-guard**. Peer output-repetition/CPU guard and its settings transaction follow-ups implement a new output cutoff policy. No corresponding Axon incident or detector contract; do not import partial guard lifecycle/DB state.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [29522737fd](https://github.com/james-6-23/codex2api/commit/29522737fd81ac2536de1dd99fb953686715588a) fix(proxy): return 502 for native guard trips before any write
  Decision: **defer-output-guard**. Peer output-repetition/CPU guard and its settings transaction follow-ups implement a new output cutoff policy. No corresponding Axon incident or detector contract; do not import partial guard lifecycle/DB state.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [8721fbd31e](https://github.com/james-6-23/codex2api/commit/8721fbd31e8405f820a9613574d7b08ae42e68ee) ci(docker): 为分支构建发布独立镜像标签
  Decision: **project-maintenance**. Peer sponsorship/build/release maintenance does not apply to Axon delivery.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [fe2857f028](https://github.com/james-6-23/codex2api/commit/fe2857f0288f6f669c439484949b7719438d1f04) fix(admin): restore runtime state when output guard save fails
  Decision: **defer-output-guard**. Peer output-repetition/CPU guard and its settings transaction follow-ups implement a new output cutoff policy. No corresponding Axon incident or detector contract; do not import partial guard lifecycle/DB state.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [10300627af](https://github.com/james-6-23/codex2api/commit/10300627af88927d52137ddcdcbd39b36de49768) fix(proxy): charge exact sse join bytes and drop interrupt frames after guard trip
  Decision: **defer-output-guard**. Peer output-repetition/CPU guard and its settings transaction follow-ups implement a new output cutoff policy. No corresponding Axon incident or detector contract; do not import partial guard lifecycle/DB state.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [44e6c40712](https://github.com/james-6-23/codex2api/commit/44e6c40712bfe571ba3c80caa686603ff75dbaad) ci(docker): 支持按摘要将已有镜像标记为 latest
  Decision: **project-maintenance**. Peer sponsorship/build/release maintenance does not apply to Axon delivery.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [442be3a3d2](https://github.com/james-6-23/codex2api/commit/442be3a3d237484c34a52f8d8f2ac45e7db1a3c6) feat(key-usage): 显示密钥实时并发气泡
  Decision: **different-usage-ui**. Public key concurrency/usage views and account settings UI use codex2api-specific APIs and state. No equivalent Axon defect; retain frontend style and scope.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [b9fd3e9e9d](https://github.com/james-6-23/codex2api/commit/b9fd3e9e9d4c02a4076bcd271c6d331abbcec7b0) feat(key-usage): 补齐请求日志来源与思考 Token 显示
  Decision: **different-usage-ui**. Public key concurrency/usage views and account settings UI use codex2api-specific APIs and state. No equivalent Axon defect; retain frontend style and scope.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [2b8f567740](https://github.com/james-6-23/codex2api/commit/2b8f5677400d3ec6ba9e3e85a23b9196786d4a7d) fix(key-usage): 隐藏空闲并发并展示可用资源
  Decision: **different-usage-ui**. Public key concurrency/usage views and account settings UI use codex2api-specific APIs and state. No equivalent Axon defect; retain frontend style and scope.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [626c8db78a](https://github.com/james-6-23/codex2api/commit/626c8db78a9a7df35ce76694db803d781c1c80f9) feat(key-usage): 显示思考强度并合并性能气泡
  Decision: **different-usage-ui**. Public key concurrency/usage views and account settings UI use codex2api-specific APIs and state. No equivalent Axon defect; retain frontend style and scope.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [6f7bf49099](https://github.com/james-6-23/codex2api/commit/6f7bf4909903008d47b9286c3279a602cb79589a) fix(key-usage): 复用管理页指标布局并独立展示缓存
  Decision: **different-usage-ui**. Public key concurrency/usage views and account settings UI use codex2api-specific APIs and state. No equivalent Axon defect; retain frontend style and scope.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [e540adadec](https://github.com/james-6-23/codex2api/commit/e540adadec4b7c9b84d7d15e92454a2230adf91f) fix(admin): apply runtime settings only after the settings transaction commits
  Decision: **defer-output-guard**. Peer output-repetition/CPU guard and its settings transaction follow-ups implement a new output cutoff policy. No corresponding Axon incident or detector contract; do not import partial guard lifecycle/DB state.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [8b2b56eded](https://github.com/james-6-23/codex2api/commit/8b2b56eded53e2cd13a1a3b39757de7ec5f10516) fix(frontend): reconcile guard state and confirmed snapshot with server generation
  Decision: **defer-output-guard**. Peer output-repetition/CPU guard and its settings transaction follow-ups implement a new output cutoff policy. No corresponding Axon incident or detector contract; do not import partial guard lifecycle/DB state.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [f7481f7ff5](https://github.com/james-6-23/codex2api/commit/f7481f7ff524c47f9ccc9363e3b69cb0753c9d1b) fix(frontend): adopt server guard state into confirmed snapshot on newer generation
  Decision: **defer-output-guard**. Peer output-repetition/CPU guard and its settings transaction follow-ups implement a new output cutoff policy. No corresponding Axon incident or detector contract; do not import partial guard lifecycle/DB state.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [bfbedc93c7](https://github.com/james-6-23/codex2api/commit/bfbedc93c7d864b58398c347fdb49e2b9d40b4e5) fix(admin): return errors for settings write failures and stage rate-limit retries
  Decision: **defer-output-guard**. Peer output-repetition/CPU guard and its settings transaction follow-ups implement a new output cutoff policy. No corresponding Axon incident or detector contract; do not import partial guard lifecycle/DB state.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [460f29a5bc](https://github.com/james-6-23/codex2api/commit/460f29a5bc05df0faef7840f4361fec63961802f) fix(proxy): suppress chat keepalives before the first content token
  Decision: **peer-reversal**. Pre-content Chat heartbeat suppression is reversed later in this frozen range. Retain Axon existing streaming/keepalive behavior; no net fix to port.
  Depth: title/parent/file inventory across the frozen range. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [ac69a49633](https://github.com/james-6-23/codex2api/commit/ac69a4963350301995e1117c7ed93ae43b5c07bd) fix(admin): apply runtime limits and global rpm after the settings commit
  Decision: **defer-output-guard**. Peer output-repetition/CPU guard and its settings transaction follow-ups implement a new output cutoff policy. No corresponding Axon incident or detector contract; do not import partial guard lifecycle/DB state.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [a590069457](https://github.com/james-6-23/codex2api/commit/a590069457eff649e32b25aa06080565b810c308) fix(frontend): advance confirmed guard generation on autosave success
  Decision: **defer-output-guard**. Peer output-repetition/CPU guard and its settings transaction follow-ups implement a new output cutoff policy. No corresponding Axon incident or detector contract; do not import partial guard lifecycle/DB state.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [2bbb861ff0](https://github.com/james-6-23/codex2api/commit/2bbb861ff0b4b623dc7f443ad839870f6c3c201e) feat(intelligence): add adaptive pause backoff and recovery probes
  Decision: **defer-probe-policy**. Peer intelligence-test adaptive pause/recovery probes introduce scheduled calls and new backoff policy, not a compatible isolated gateway fix.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [5e298dff8f](https://github.com/james-6-23/codex2api/commit/5e298dff8f6253788add4dbb802ebd842dfdeb76) Merge pull request #849 from bikeread/feat/intelligence-adaptive-backoff
  Decision: **merge-record**. Merge bookkeeping; constituent new commits in the frozen range are recorded separately, without rereading their patches.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [3eefa1457d](https://github.com/james-6-23/codex2api/commit/3eefa1457d4af481a3a576f15cb1fefc2cc1988e) Merge remote-tracking branch 'origin/main' into james-6-23-fix-pr-847
  Decision: **merge-record**. Merge bookkeeping; constituent new commits in the frozen range are recorded separately, without rereading their patches.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [077656c134](https://github.com/james-6-23/codex2api/commit/077656c134ca4c0d2d1e472bb2749b73558410ac) fix(key-usage): 处理 PR #847 评审意见并修复 CI
  Decision: **different-usage-ui**. Public key concurrency/usage views and account settings UI use codex2api-specific APIs and state. No equivalent Axon defect; retain frontend style and scope.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [f450f61eaf](https://github.com/james-6-23/codex2api/commit/f450f61eafb091fe43cfefa59632fe1e760283a1) fix(key-usage): 连续轮询失败后清空并发快照，并更新自助用量报告注释
  Decision: **different-usage-ui**. Public key concurrency/usage views and account settings UI use codex2api-specific APIs and state. No equivalent Axon defect; retain frontend style and scope.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [954d18d55b](https://github.com/james-6-23/codex2api/commit/954d18d55b4151339a9fa0172267de5b00f3105b) Merge pull request #847 from bxb1337/feat/key-usage-log-details
  Decision: **merge-record**. Merge bookkeeping; constituent new commits in the frozen range are recorded separately, without rereading their patches.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [dde216484e](https://github.com/james-6-23/codex2api/commit/dde216484ee57f2c5d2eec90d8a54a75b63cf160) fix: upgrade x/net to address GO-2026-6860
  Decision: **covered-dependency**. Root and LLM modules already pin golang.org/x/net v0.62.0; no dependency downgrade or duplicate upgrade.
  Depth: title/file inventory and previous committed dependency evidence. Evidence: go.mod; llm/go.mod.
- [bb996aa6ad](https://github.com/james-6-23/codex2api/commit/bb996aa6ad16f4737d30bf1d88a15923bc1eccd7) Merge pull request #846 from bxb1337/fix/codex-client-identity-oauth
  Decision: **merge-record**. Merge bookkeeping; constituent new commits in the frozen range are recorded separately, without rereading their patches.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [9d425eeaff](https://github.com/james-6-23/codex2api/commit/9d425eeaffe2b410e20b08609554dc92df6487e9) Merge origin/main into fix/dsh-stream-runaway-guard
  Decision: **merge-record**. Merge bookkeeping; constituent new commits in the frozen range are recorded separately, without rereading their patches.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [9a368d33e8](https://github.com/james-6-23/codex2api/commit/9a368d33e878a153375562a249e74fcd08f105f6) fix(proxy): restore pre-content SSE heartbeats for chat completions
  Decision: **peer-reversal**. Pre-content Chat heartbeat suppression is reversed later in this frozen range. Retain Axon existing streaming/keepalive behavior; no net fix to port.
  Depth: title/parent/file inventory across the frozen range. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [b0067caa1a](https://github.com/james-6-23/codex2api/commit/b0067caa1a2d72e514874f2ab9985b15d6bd9323) feat(antigravity): 配额门禁按模型族隔离并新增临期优先调度
  Decision: **defer-quota-routing**. Model-family expiry scheduling and quota gates are peer-specific routing features; retain existing Axon quota policy.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [a8f096e591](https://github.com/james-6-23/codex2api/commit/a8f096e5913f61244ec78058794a9528756d5dbd) Merge pull request #845 from ggg17226/fix/dsh-stream-runaway-guard
  Decision: **merge-record**. Merge bookkeeping; constituent new commits in the frozen range are recorded separately, without rereading their patches.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [d96627047b](https://github.com/james-6-23/codex2api/commit/d96627047b89ebcc3068894aa6225c320e3d66eb) fix(antigravity): 处理 PR #850 评审意见
  Decision: **defer-quota-routing**. Model-family expiry scheduling and quota gates are peer-specific routing features; retain existing Axon quota policy.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [00d21df7c4](https://github.com/james-6-23/codex2api/commit/00d21df7c4028097799591fe4531c7cf92c51fc8) Merge pull request #850 from james-6-23/james-6-23/idea-antigravity-model-aware-bucket-expiry-sched
  Decision: **merge-record**. Merge bookkeeping; constituent new commits in the frozen range are recorded separately, without rereading their patches.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [048a42f5bc](https://github.com/james-6-23/codex2api/commit/048a42f5bcfd0d1458bc77b4db3ee7444d68bcaf) fix(frontend): Claude 账号与共享分组弹窗的自动暂停阈值按百分比换算
  Decision: **different-usage-ui**. Public key concurrency/usage views and account settings UI use codex2api-specific APIs and state. No equivalent Axon defect; retain frontend style and scope.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [20e4b39994](https://github.com/james-6-23/codex2api/commit/20e4b39994d867abcf6de3d43f26d27635a4c09a) docs(changelog): v3.1.3
  Decision: **documentation-only**. Documentation, planning or empty commit; no production gateway change.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [e09111daa0](https://github.com/james-6-23/codex2api/commit/e09111daa05da9ddf0f647b32e73c0415f0715a9) fix(frontend): 详情抽屉左右方向键避免劫持输入框光标与上层弹窗
  Decision: **different-ui**. Arrow-key guard applies to codex2api AccountDetailSheet global navigation; Axon inspected channel/request screens do not implement that same global handler.
  Depth: title/file inventory and targeted source search. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [c2a7b55b0e](https://github.com/james-6-23/codex2api/commit/c2a7b55b0e0067176efc035f5d904e2186acc30e) refactor(frontend): 提取账号抽屉按键守卫至独立模块并完善覆盖测试
  Decision: **different-ui**. Arrow-key guard applies to codex2api AccountDetailSheet global navigation; Axon inspected channel/request screens do not implement that same global handler.
  Depth: title/file inventory and targeted source search. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [e2efc16c42](https://github.com/james-6-23/codex2api/commit/e2efc16c4242b564efc905eb65ac7d76d07f8b4d) test(frontend): 严格匹配可编辑元素选择器并补充 contenteditable=false 否定用例
  Decision: **different-ui**. Arrow-key guard applies to codex2api AccountDetailSheet global navigation; Axon inspected channel/request screens do not implement that same global handler.
  Depth: title/file inventory and targeted source search. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [b821994ba2](https://github.com/james-6-23/codex2api/commit/b821994ba214c78167002e5fcfb80cc611a16998) Merge pull request #851 from yanhao98/fix/sheet-arrow-keys
  Decision: **merge-record**. Merge bookkeeping; constituent new commits in the frozen range are recorded separately, without rereading their patches.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [42ffd35dd3](https://github.com/james-6-23/codex2api/commit/42ffd35dd31bba6a2a59644e3a1e7848dbe37533) fix(models): 账号文本白名单不再隐藏 /v1/models 中的图片模型
  Decision: **different-model-filter**. Peer distinguishes account text allowlists from image-model discovery. Axon uses its own model/channel visibility policy; do not broaden visibility without an equivalent filter defect.
  Depth: title and file inventory. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [a8cd974021](https://github.com/james-6-23/codex2api/commit/a8cd9740211bbf3da62d7f196e90dfddc9bac925) Merge pull request #852 from james-6-23/james-6-23-fix-image-model-discovery-allowlist
  Decision: **merge-record**. Merge bookkeeping; constituent new commits in the frozen range are recorded separately, without rereading their patches.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [fb3e15f0bb](https://github.com/james-6-23/codex2api/commit/fb3e15f0bbd29eb6b9164c410cb000d337d14a0f) Initial plan
  Decision: **documentation-only**. Documentation, planning or empty commit; no production gateway change.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [a4bbb455ba](https://github.com/james-6-23/codex2api/commit/a4bbb455ba7ae9f4e6e02e6de2cae24fe12e6212) Merge pull request #853 from james-6-23/copilot/fix-analyze-javascript-typescript-job
  Decision: **merge-record**. Merge bookkeeping; constituent new commits in the frozen range are recorded separately, without rereading their patches.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.

## gpt-load: 3 new records

`43b2554fdde9211c5ec63d31f521541d7eb4bba8` → `5f345352d1191bded33cc3a994f19a66b9a6c8fa`; frozen `2026-11-04T17:13:23.117318+00:00`.

- [2c970e10fc](https://github.com/tbphp/gpt-load/commit/2c970e10fc2d514764423591e114e28febeed29a) fix(classic): show all subscription channels (#941)
  Decision: **different-ui**. Vue classic subscription channel picker has no corresponding Axon implementation; retain existing frontend style.
  Depth: title and file inventory. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [518894ea16](https://github.com/tbphp/gpt-load/commit/518894ea163a76549acb39b0e767970b9116e98c) fix(devin): use 127.0.0.1 OAuth redirect URI (#942)
  Decision: **different-provider**. Devin subscription OAuth callback is not an Axon provider integration in this task.
  Depth: title and file inventory. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [5f345352d1](https://github.com/tbphp/gpt-load/commit/5f345352d1191bded33cc3a994f19a66b9a6c8fa) fix(control): show upstream errors in credential tests (#944)
  Decision: **defer-diagnostic-ui**. Peer introduces credential-test error_detail with sanitized summaries, request IDs and response truncation. Axon uses a different channel-test GraphQL result; no matching missing/error exposure fixture established.
  Depth: selected credential_probe.go and error_message.go. Evidence: peer changed-file inventory; no Axon equivalence claimed.

## new-api: 5 new records

`9b473a3fe4fed8d18980fd8d3c6a8908847a5f25` → `706144769e5bed5a47b78721e3bb11da3cf4c0b9`; frozen `2026-11-04T17:13:23.372275+00:00`.

- [52c173038e](https://github.com/QuantumNous/new-api/commit/52c173038e3551618ef7c62f2e4db62ca43a8f69) feat(gemini): add Argon fields and reasoning mappings (#8177)
  Decision: **defer-coherent-argon-support**. Adds Argon continuation tokens, media-processing fields and reasoning maps. This requires end-to-end native/conversion behavior and continuation accounting; do not map CONTINUATION to successful completion or copy a model whitelist alone. Retain as a distinct capability gap, not covered.
  Depth: selected runtime patch; official Gemini SDK continuation documentation. Evidence: llm/transformer/gemini/model.go; llm/transformer/gemini/convert.go.
- [83b18fe6cc](https://github.com/QuantumNous/new-api/commit/83b18fe6cc3fcf489fb64772445c8587d71251f1) feat(channel): return created IDs with input mappings (#8164)
  Decision: **different-channel-api**. Returned creation IDs and input mappings target new-api batch channel creation; Axon uses GraphQL entity creation, without the same response contract.
  Depth: title and file inventory. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [94b5216645](https://github.com/QuantumNous/new-api/commit/94b521664599d416fead8e57d8318717c0e29058) feat(vertex): add per-channel v1beta1 endpoint switch (#8179)
  Decision: **defer-new-setting**. Peer adds a per-channel Vertex v1beta1 UI switch. Axon has its own endpoint configuration; a new setting needs input/output/UI design and verified endpoint demand, not copying another channel model.
  Depth: title and file inventory. Evidence: llm/transformer/gemini/embedding.go.
- [ba5f736779](https://github.com/QuantumNous/new-api/commit/ba5f7367797566175a6cd97a748d430af3602a01) fix(gemini): bill embeddings from upstream usageMetadata (#8182)
  Decision: **adapt**. Preserve actual Gemini embedding prompt counts and modality details for single and batch responses. Keep absent/null counts unknown, preserve explicit zero, ignore invalid negative counts, and never count batch usage per vector or add generation tokens.
  Depth: selected patch and owning-source comparison. Evidence: llm/transformer/gemini/embedding.go; llm/transformer/gemini/embedding_usage_test.go; https://aistudio.google.com/api-reference/embeddings#Resource:EmbeddingUsageMetadata.
- [706144769e](https://github.com/QuantumNous/new-api/commit/706144769e5bed5a47b78721e3bb11da3cf4c0b9) fix(auth): 校验 Redis 鉴权缓存完整性 (#8200)
  Decision: **defer-auth-cache-redesign**. Peer HMAC protects its Redis auth snapshots and session version fields. Axon cache keys, auth objects and invalidation contracts differ; a new cache-signing scheme requires a separate cache migration and threat-boundary review. No shared implementation defect established.
  Depth: selected auth_cache_integrity.go; remaining files screened. Evidence: .agent/rules/cache-compat.md.

## magpie: 183 new records

`40a287b217660c3cb33becc1b911ed8479b5d335` → `83137bcbe971fced599a9e81d42211e1f552b2e0`; frozen `2026-11-04T17:13:23.102677+00:00`.

- [0acba61a35](https://github.com/yetone/magpie/commit/0acba61a35cbfd98995050b20afa49985cf71040) gui: a lone plugin's check-in tab is named for it, not "Plugins"
  Decision: **different-ui-or-test**. Peer GUI layout/local tooling regression has no matching Axon implementation; preserve current UI style.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [a9a8f11e7f](https://github.com/yetone/magpie/commit/a9a8f11e7f9a949b28ae032af8d0b1f21f54aee6) plugin, proc: a plugin host is waited for when it ends, not when its stdout does
  Decision: **different-plugin-runtime**. Peer JS/provider plugin hooks, sign-in, search and page-service lifecycle are outside Axon runtime.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [f4c664a09c](https://github.com/yetone/magpie/commit/f4c664a09ca0e2dcdab3d41290e10ba440a1367f) plugin, gui: magpie web ends its plugin hosts, waited for, before it runs the new version (#3285)
  Decision: **different-plugin-runtime**. Peer JS/provider plugin hooks, sign-in, search and page-service lifecycle are outside Axon runtime.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [c3593dc494](https://github.com/yetone/magpie/commit/c3593dc4944b91819ed12c601b0f2e844b804b5e) provider: the Cursor mover's min is 0.2.39, which lists the Claude Cursor hides from an account once per context size (plugins#634, jiaohanying)
  Decision: **different-provider-installer**. Version pins install external account plugins, not Axon protocol adapters.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [78cd4e6dd4](https://github.com/yetone/magpie/commit/78cd4e6dd4cd4ba380496b3467e6b1becc582cb2) sessions: a call through magpie in Cindy is counted once, as the gateway's (jealousy03888)
  Decision: **different-session-store**. Indexes or rewrites local agent transcript files; Axon request/history database is a different store and identity boundary.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [00098f12bf](https://github.com/yetone/magpie/commit/00098f12bf0659d5c4ed7dcbc98a731541578f79) library: Magpie Browser's "which agents may use it" is a line of its own under its row
  Decision: **different-ui-or-test**. Peer GUI layout/local tooling regression has no matching Axon implementation; preserve current UI style.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [d1cdce1302](https://github.com/yetone/magpie/commit/d1cdce1302d0323fc8518a7d85f693154731a829) provider: the group alone keeps subscription models out of agents' lists (shaoyang.)
  Decision: **retain-model-visibility**. Peer group model filtering and cross-account Codex model lists differ from Axon channel/owner-scoped catalogs. Do not broaden account capability or model visibility.
  Depth: title and file inventory. Evidence: internal/server/biz/codex_catalog.go.
- [92d2ed74f9](https://github.com/yetone/magpie/commit/92d2ed74f9f3a7e91ee3304318ef345ee94d823f) providers: an Ark key's Seedance models are under Video models in its editor, each to tick
  Decision: **defer-provider-policy**. Peer account/model/group pricing, quota display, refresh or scheduling policy uses different state. No equivalent Axon failure established; do not transplant policy or settings.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [af9ac3d25d](https://github.com/yetone/magpie/commit/af9ac3d25df2e61335a97ba34a9610da3ff7ef51) gateway: /v1/models lists image and video models when ?kind= asks for them, chat models alone by default
  Decision: **retain-model-visibility**. Peer group model filtering and cross-account Codex model lists differ from Axon channel/owner-scoped catalogs. Do not broaden account capability or model visibility.
  Depth: title and file inventory. Evidence: internal/server/biz/codex_catalog.go.
- [b2034c1bcf](https://github.com/yetone/magpie/commit/b2034c1bcf8f3581522e18a76bd202beaa86aeff) gateway: an Ark video keeps the caller's content, ratio and own fields, and answers Ark's
  Decision: **defer-native-media-feature**. Ark native paths/media overrides or Copilot image generation require a dedicated provider/endpoint contract. Existing Axon image response-format behavior remains; do not add arbitrary native proxy endpoints here.
  Depth: title and file inventory. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [db87e014f2](https://github.com/yetone/magpie/commit/db87e014f2c53cdcf4c4e10c2d3cd41393ba3cb6) gateway: /v1/magpie/native/<provider>/<path> passes Ark's own images and video-task API on
  Decision: **defer-native-media-feature**. Ark native paths/media overrides or Copilot image generation require a dedicated provider/endpoint contract. Existing Axon image response-format behavior remains; do not add arbitrary native proxy endpoints here.
  Depth: title and file inventory. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [d8501848a7](https://github.com/yetone/magpie/commit/d8501848a7df9425f4ea89f3c6a0b7483f393828) gateway: an Ark image request's own fields go to Ark, and watermark and response_format are the caller's
  Decision: **defer-native-media-feature**. Ark native paths/media overrides or Copilot image generation require a dedicated provider/endpoint contract. Existing Axon image response-format behavior remains; do not add arbitrary native proxy endpoints here.
  Depth: title and file inventory. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [1d0fc2bb08](https://github.com/yetone/magpie/commit/1d0fc2bb08c51d8711759694189b8a1d92ee3a1e) sessions: a fork's copies of its parent's calls are counted once (Codex, ZCode)
  Decision: **different-session-store**. Indexes or rewrites local agent transcript files; Axon request/history database is a different store and identity boundary.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [3f1f1882ac](https://github.com/yetone/magpie/commit/3f1f1882ac42224a034ca07b7d997006e9b88af2) claude: magpie's model names never stay in a Claude Code switched off it (roswellcsy on X)
  Decision: **different-desktop-runtime**. Desktop agent wiring, local installation/configuration, process/UI/library lifecycle has no corresponding server-gateway component.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [187f3e2727](https://github.com/yetone/magpie/commit/187f3e272789370fd30c0ed87c14e8708e78b208) usage: a swipe across the Requests table is never written back by its scrollbar's echo (Pluviophile)
  Decision: **different-ui-or-test**. Peer GUI layout/local tooling regression has no matching Axon implementation; preserve current UI style.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [6c201095d8](https://github.com/yetone/magpie/commit/6c201095d85b3a6f956b4e32da5cc80abe64b642) gateway: in Docker, other containers on the host are shown the host's Docker bridge address
  Decision: **defer-gateway-feature**. Peer request display, prompt/memory attribution or transport lifecycle is tied to its own gateway structures; no exact Axon defect established in this review.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [22f5cf69e5](https://github.com/yetone/magpie/commit/22f5cf69e583dab7723523be514cbafc9c870ebd) library: an agent in WSL links to the distro's one copy of a skill (cutemmuu on X)
  Decision: **different-desktop-runtime**. Desktop agent wiring, local installation/configuration, process/UI/library lifecycle has no corresponding server-gateway component.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [7d022bdb63](https://github.com/yetone/magpie/commit/7d022bdb63202c1d33f34e05e55e3e6dc8236f87) provider: the Cursor mover's min is 0.2.40 (plugins#672)
  Decision: **different-provider-installer**. Version pins install external account plugins, not Axon protocol adapters.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [d136d0fe98](https://github.com/yetone/magpie/commit/d136d0fe981b2545bb83d69eca94218934ea6b3b) gateway-key: rotating a key can give it a value of the user's own (isaac_bai)
  Decision: **defer-provider-policy**. Peer account/model/group pricing, quota display, refresh or scheduling policy uses different state. No equivalent Axon failure established; do not transplant policy or settings.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [08d7bfaaa5](https://github.com/yetone/magpie/commit/08d7bfaaa588da6d701108b01ebacabec7c55e22) redact: Settings › Privacy's Masked words show as dots until Show (noahhrreion)
  Decision: **different-ui-or-test**. Peer GUI layout/local tooling regression has no matching Axon implementation; preserve current UI style.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [fb1f4856a8](https://github.com/yetone/magpie/commit/fb1f4856a8db931820842e1d01926bd0e6b846b8) sessions: a fork's own calls are counted; only its copies are the parent's (ZCode, Codex)
  Decision: **different-session-store**. Indexes or rewrites local agent transcript files; Axon request/history database is a different store and identity boundary.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [62f612fd44](https://github.com/yetone/magpie/commit/62f612fd44af6245a550faaa9c147ce44f12744f) proc: recheck tools installed within a directory clock tick
  Decision: **different-desktop-runtime**. Desktop agent wiring, local installation/configuration, process/UI/library lifecycle has no corresponding server-gateway component.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [b59e4b747f](https://github.com/yetone/magpie/commit/b59e4b747f70ea5ff77db84f08aa377152e532ac) gateway: a conversation on a new account leaves out the old one's seals (#3887)
  Decision: **retain-native-history**. Peer removes old-account seals when switching accounts. Keep Axon credential/session-scoped, rejection-driven replay and the November 4 canonical ancestry fix; do not preemptively strip encrypted state.
  Depth: title and file inventory. Evidence: internal/server/orchestrator/responses_compaction_recovery.go.
- [8c93923638](https://github.com/yetone/magpie/commit/8c9392363879773fcdf8203d4a2ac2a4d16d2b2a) library: no package's tests install an npx server's package or end the machine's servers (#4140)
  Decision: **different-desktop-runtime**. Desktop agent wiring, local installation/configuration, process/UI/library lifecycle has no corresponding server-gateway component.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [55a190f195](https://github.com/yetone/magpie/commit/55a190f195ca1e8b5b5d55b0806e7050aa940912) gateway: conversations opened on one fixed message no longer share an id (#3938)
  Decision: **retain-session-contract**. Peer fallback conversation IDs hash fixed environment preambles until a user opening. Its body-hash identity policy differs from Axon owned session/credential isolation; no cross-session collision demonstrated in Axon.
  Depth: selected opening regression fixture (partial); file inventory. Evidence: internal/server/orchestrator/responses_session.go.
- [985b9f1a0a](https://github.com/yetone/magpie/commit/985b9f1a0a0a2a7357df2d095decdb5815d82d50) gateway: Reasonix's session-context snapshot is the agent's (#3938)
  Decision: **defer-gateway-feature**. Peer request display, prompt/memory attribution or transport lifecycle is tied to its own gateway structures; no exact Axon defect established in this review.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [c0dd5838c2](https://github.com/yetone/magpie/commit/c0dd5838c2738bad673fc2993d5217fa14e1cb33) library: the write-failure card can be hidden until a new failure (#3745)
  Decision: **different-ui-or-test**. Peer GUI layout/local tooling regression has no matching Axon implementation; preserve current UI style.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [8cf9dbe6bd](https://github.com/yetone/magpie/commit/8cf9dbe6bdb8eb05c980f994f36090bf4379d6b6) testenv: the test home holds XDG_CONFIG_DIRS too (#3372)
  Decision: **different-desktop-runtime**. Desktop agent wiring, local installation/configuration, process/UI/library lifecycle has no corresponding server-gateway component.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [ab25a2317b](https://github.com/yetone/magpie/commit/ab25a2317b9bc24e79cae11b5dd2c27d36def1c7) autostart: on Linux the switch follows a package's own entry (#3372)
  Decision: **different-desktop-runtime**. Desktop agent wiring, local installation/configuration, process/UI/library lifecycle has no corresponding server-gateway component.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [af55a7917f](https://github.com/yetone/magpie/commit/af55a7917f39a8708961a0b6b7660c2cd12508a8) tui: quitting where magpie opens at login says how to have the gateway handed on (#3756)
  Decision: **different-desktop-runtime**. Desktop agent wiring, local installation/configuration, process/UI/library lifecycle has no corresponding server-gateway component.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [0ed9891de0](https://github.com/yetone/magpie/commit/0ed9891de0360315226d733e62157c35bf9ab362) agent: magpie's provider under OpenCode 2's providers is kept current (#3883)
  Decision: **different-desktop-runtime**. Desktop agent wiring, local installation/configuration, process/UI/library lifecycle has no corresponding server-gateway component.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [d3de7e8f3e](https://github.com/yetone/magpie/commit/d3de7e8f3e6af1856da11047040bc5bcd6927db3) agent: a provider of the user's own at magpie's gateway, under OpenCode 2's providers, stays theirs (#3915)
  Decision: **different-desktop-runtime**. Desktop agent wiring, local installation/configuration, process/UI/library lifecycle has no corresponding server-gateway component.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [6cfe786351](https://github.com/yetone/magpie/commit/6cfe78635165b650c8c3df8a4079b46da6881d3e) agent: Codex that can't be joined again isn't connected by its helper models alone (#3868)
  Decision: **different-desktop-runtime**. Desktop agent wiring, local installation/configuration, process/UI/library lifecycle has no corresponding server-gateway component.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [78667e9b77](https://github.com/yetone/magpie/commit/78667e9b770c9d669705322c9d61caa4471f462e) gateway: a request in flight shows its model's context window
  Decision: **defer-gateway-feature**. Peer request display, prompt/memory attribution or transport lifecycle is tied to its own gateway structures; no exact Axon defect established in this review.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [b088fd0f09](https://github.com/yetone/magpie/commit/b088fd0f09b0d9769f0a5a04bb2862ba1e4f7426) gui: the panel's header icon is named "Open the main window"; a double-click on the Windows tray icon opens it
  Decision: **different-desktop-runtime**. Desktop agent wiring, local installation/configuration, process/UI/library lifecycle has no corresponding server-gateway component.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [4c0c6c02b8](https://github.com/yetone/magpie/commit/4c0c6c02b8f05ce5d3b9d00ca9e302a810c74cdb) plugin: keep idle plugin sign-ins alive in the background (#5080)
  Decision: **different-plugin-runtime**. Peer JS/provider plugin hooks, sign-in, search and page-service lifecycle are outside Axon runtime.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [d16dcba227](https://github.com/yetone/magpie/commit/d16dcba227ae657d126a4b4f1a96f408267c649e) routing: the stage's agent and magpie columns grow with their words, up to 15% of the stage
  Decision: **different-ui-or-test**. Peer GUI layout/local tooling regression has no matching Axon implementation; preserve current UI style.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [bdb856b23c](https://github.com/yetone/magpie/commit/bdb856b23c8e329f2a4b0dded51d359787650daa) icon: the bird is white in macOS 26's clear and tinted icon styles, where it was a dark glass bird on a dark tile
  Decision: **defer-gateway-feature**. Peer request display, prompt/memory attribution or transport lifecycle is tied to its own gateway structures; no exact Axon defect established in this review.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [f168dd2784](https://github.com/yetone/magpie/commit/f168dd27848b2c7daeb95b20150eed122110949f) gateway: a relayed Messages stream is read in whole events, however its reads are cut (#4868)
  Decision: **covered-parser-boundary**. Axon decodes complete SSE events at its HTTP decoder boundary, including unterminated EOF and bounded event sizes. Peer text-to-tool rewriter has a separate buffer lifetime and is not imported.
  Depth: selected patch and owning-source comparison. Evidence: llm/httpclient/decoder.go; llm/httpclient/decoder_test.go.
- [93b1b49047](https://github.com/yetone/magpie/commit/93b1b49047edb27f736a98bdb2c3a0c216247d28) gui tests: searcher-pick waits for the reloaded Settings to be drawn (#1745)
  Decision: **different-ui-or-test**. Peer GUI layout/local tooling regression has no matching Axon implementation; preserve current UI style.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [94d0fbfcd3](https://github.com/yetone/magpie/commit/94d0fbfcd381d1ab0d65c55e1e100cdcd7316045) provider: the ZCode mover installs opencode-zcode-auth 0.1.46 (plugins#676, #678)
  Decision: **different-provider-installer**. Version pins install external account plugins, not Axon protocol adapters.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [a80575cc86](https://github.com/yetone/magpie/commit/a80575cc8652a68ca01749d8ed6d4ec7d05832d0) library: an MCP server's command wraps, and the editor shows the server as JSON (hema_17384)
  Decision: **different-ui-or-test**. Peer GUI layout/local tooling regression has no matching Axon implementation; preserve current UI style.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [50265256ba](https://github.com/yetone/magpie/commit/50265256ba56ac164a680da0689e1c6d90dac471) library: a note on a skill or an MCP server, the library's own (youyo.p_10001)
  Decision: **different-desktop-runtime**. Desktop agent wiring, local installation/configuration, process/UI/library lifecycle has no corresponding server-gateway component.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [1e1068c86c](https://github.com/yetone/magpie/commit/1e1068c86c5dbdf177b4c5ed86ddcff134d46a80) codex: "Same as the lead" holds every subagent to the lead's model (cleverpigo on X)
  Decision: **defer-gateway-feature**. Peer request display, prompt/memory attribution or transport lifecycle is tied to its own gateway structures; no exact Axon defect established in this review.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [51fa4b7eb0](https://github.com/yetone/magpie/commit/51fa4b7eb06fda55da50e930c0c7dcb0e6fe781b) browser: agents work in tabs of their own, behind the user's
  Decision: **different-agent-bridge**. Peer local Claude process/MCP/browser/search/statusline subsystem is not part of Axon protocol gateway.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [8e27369bd6](https://github.com/yetone/magpie/commit/8e27369bd699ece41a68affc27522c99fa944fca) browser: record_start and record_stop make a GIF of what an agent did
  Decision: **different-agent-bridge**. Peer local Claude process/MCP/browser/search/statusline subsystem is not part of Axon protocol gateway.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [8468afecb3](https://github.com/yetone/magpie/commit/8468afecb3a3751d0de1ea2ce3d55644c897a045) claude: a turn's images say they are attached, so the file notes after them are not taken for the images
  Decision: **different-agent-bridge**. Peer local Claude process/MCP/browser/search/statusline subsystem is not part of Axon protocol gateway.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [7033f50722](https://github.com/yetone/magpie/commit/7033f507225d98fdbd6bef4e79eda8fa810395b7) gui: the panel opens the main window from its edge nearest the tray
  Decision: **different-desktop-runtime**. Desktop agent wiring, local installation/configuration, process/UI/library lifecycle has no corresponding server-gateway component.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [4c0cfafcf8](https://github.com/yetone/magpie/commit/4c0cfafcf80b877cc3d33659c0d3407c4dca575f) gui: a tap that slides a few pixels is a click, not a drag (refeccd)
  Decision: **different-ui-or-test**. Peer GUI layout/local tooling regression has no matching Axon implementation; preserve current UI style.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [7785c42787](https://github.com/yetone/magpie/commit/7785c4278780e5c4a52c2c6d14f5c7284b499d45) provider: raise the ZCode mover min to 0.1.47
  Decision: **different-provider-installer**. Version pins install external account plugins, not Axon protocol adapters.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [b92b3bc5b1](https://github.com/yetone/magpie/commit/b92b3bc5b12b49c0b6f7812174e333f90d8f6ea8) provider: raise the ZCode mover min to 0.1.48
  Decision: **different-provider-installer**. Version pins install external account plugins, not Axon protocol adapters.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [ec3111d873](https://github.com/yetone/magpie/commit/ec3111d8731058b2b2112fb46302e07f0d664ba5) usage: a pool set aside counts in its card's credits a day (jiabubu)
  Decision: **defer-gateway-feature**. Peer request display, prompt/memory attribution or transport lifecycle is tied to its own gateway structures; no exact Axon defect established in this review.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [9ce59b8340](https://github.com/yetone/magpie/commit/9ce59b834067e12b020b2c516c5a5a8853a6d060) routing: a request's row says the credits its reply said it cost (jiabubu)
  Decision: **defer-provider-policy**. Peer account/model/group pricing, quota display, refresh or scheduling policy uses different state. No equivalent Axon failure established; do not transplant policy or settings.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [e4bc3eeb6f](https://github.com/yetone/magpie/commit/e4bc3eeb6fd52d86f6f5458e1aaad031122f6d27) gui: the context window card's head keeps its title, icons and model apart at every width
  Decision: **different-ui-or-test**. Peer GUI layout/local tooling regression has no matching Axon implementation; preserve current UI style.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [7aff42d863](https://github.com/yetone/magpie/commit/7aff42d8632f1f530ba610058d06c4f0ba7399f3) plugin host: the sign-in lock and file waits no longer freeze the event loop (#5112)
  Decision: **defer-gateway-feature**. Peer request display, prompt/memory attribution or transport lifecycle is tied to its own gateway structures; no exact Axon defect established in this review.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [e2d52b5012](https://github.com/yetone/magpie/commit/e2d52b50124b6540314aa81df7ab7554cdb34ce3) provider: ZCode's mover min is 0.1.49, whose -Trial entry is no longer answered "quota exhausted" after a free-first refusal (Can4ry_ on X)
  Decision: **different-provider-installer**. Version pins install external account plugins, not Axon protocol adapters.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [a6f5d738fc](https://github.com/yetone/magpie/commit/a6f5d738fc01a8f760629d9d5821b7e8c81e1c00) gateway: a group's serial walk has a total budget of 540s (#5113)
  Decision: **retain-retry-policy**. Peer changes retry budgets, affinity retries, cooldowns or region/risk classification. These are routing policy changes rather than proven Axon defects; retain bounded retries and existing error contracts.
  Depth: title and file inventory. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [12d4825afa](https://github.com/yetone/magpie/commit/12d4825afac593b50ee4fdfa185b0fed9c5fba70) gateway: a turn of only waits (sleep, wait, delay, pause, poll) is not a tool loop
  Decision: **defer-gateway-feature**. Peer request display, prompt/memory attribution or transport lifecycle is tied to its own gateway structures; no exact Axon defect established in this review.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [c360d9d30d](https://github.com/yetone/magpie/commit/c360d9d30d96fa6adf38f103dda0b107c0bbcf66) gui: a tap that slides off its Form | JSON option still picks it (refeccd)
  Decision: **different-ui-or-test**. Peer GUI layout/local tooling regression has no matching Axon implementation; preserve current UI style.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [b045362a92](https://github.com/yetone/magpie/commit/b045362a92e7d3b0b48c2b88e254ee91dfed9263) routing: importing a shared group whose id is one of yours replaces that group (issennknight)
  Decision: **defer-provider-policy**. Peer account/model/group pricing, quota display, refresh or scheduling policy uses different state. No equivalent Axon failure established; do not transplant policy or settings.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [1cb346f2dc](https://github.com/yetone/magpie/commit/1cb346f2dc448e3484301c7232bc4ed4df69f49a) gui: on Linux the folder and Backups buttons start the opener and return (pkh1585)
  Decision: **different-desktop-runtime**. Desktop agent wiring, local installation/configuration, process/UI/library lifecycle has no corresponding server-gateway component.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [47b7ba3030](https://github.com/yetone/magpie/commit/47b7ba3030928b884eb713f72e715e45e24661cc) models: a model its provider lists at $0 says free in the pickers and Routing (jiangguochao666 on X)
  Decision: **defer-gateway-feature**. Peer request display, prompt/memory attribution or transport lifecycle is tied to its own gateway structures; no exact Axon defect established in this review.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [d8b3749ef7](https://github.com/yetone/magpie/commit/d8b3749ef7e0f74491a0fa0c0b48a0208c662724) agents: Copilot CLI reaches magpie's models through its providers.json (BYOK)
  Decision: **different-desktop-runtime**. Desktop agent wiring, local installation/configuration, process/UI/library lifecycle has no corresponding server-gateway component.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [59c1306035](https://github.com/yetone/magpie/commit/59c1306035874c758a08ecd92de853d4f69d8b58) claude: an auto-mode client's tool loop keeps its interactive run (#4804)
  Decision: **different-agent-bridge**. Peer local Claude process/MCP/browser/search/statusline subsystem is not part of Axon protocol gateway.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [0e1054ce19](https://github.com/yetone/magpie/commit/0e1054ce19b2bebe8688bc1211633e2e488247bd) statusline: the gateway serves Claude Code's status line to another computer (steven_4422)
  Decision: **different-agent-bridge**. Peer local Claude process/MCP/browser/search/statusline subsystem is not part of Axon protocol gateway.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [452c28a78c](https://github.com/yetone/magpie/commit/452c28a78cfcec6a4ab2a4bb79943c06c0a28af8) cli: GET /api/context/reply is named as the window's own (#5026)
  Decision: **different-ui-or-test**. Peer GUI layout/local tooling regression has no matching Axon implementation; preserve current UI style.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [6673171595](https://github.com/yetone/magpie/commit/6673171595d6ad92e832064ee47d1c6c85f39ff8) update: the app and magpie web follow each other's update (visamzh on X)
  Decision: **different-desktop-runtime**. Desktop agent wiring, local installation/configuration, process/UI/library lifecycle has no corresponding server-gateway component.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [df7c94bdf5](https://github.com/yetone/magpie/commit/df7c94bdf5d3bfb116d214f2aa1f5d5839ea721f) gateway: inline image descriptions survive a restart (#5121)
  Decision: **defer-gateway-feature**. Peer request display, prompt/memory attribution or transport lifecycle is tied to its own gateway structures; no exact Axon defect established in this review.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [90bf96a23d](https://github.com/yetone/magpie/commit/90bf96a23d74a8d9804fba8eb3ee28fa7ee374b0) search: a plugin can give a web search API (its search hook)
  Decision: **different-plugin-runtime**. Peer JS/provider plugin hooks, sign-in, search and page-service lifecycle are outside Axon runtime.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [2741b749f9](https://github.com/yetone/magpie/commit/2741b749f95c8548d1f9107c5cfe474ed1d05c8f) sessions: a codex fork's rollout-<n> turn does not end the copied head (#5090)
  Decision: **different-session-store**. Indexes or rewrites local agent transcript files; Axon request/history database is a different store and identity boundary.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [6d1e3f1e29](https://github.com/yetone/magpie/commit/6d1e3f1e29ebe7d862cda8fa4fa842842f3acb49) gui tests: library-problems-hide and panel-open-once load with a minute's room
  Decision: **different-ui-or-test**. Peer GUI layout/local tooling regression has no matching Axon implementation; preserve current UI style.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [68c43bba65](https://github.com/yetone/magpie/commit/68c43bba6548b6a4fa5bfe90b6b9a602bc6c335e) copilot: a Copilot account draws with the image models its /models lists
  Decision: **defer-native-media-feature**. Ark native paths/media overrides or Copilot image generation require a dedicated provider/endpoint contract. Existing Axon image response-format behavior remains; do not add arbitrary native proxy endpoints here.
  Depth: title and file inventory. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [247794c613](https://github.com/yetone/magpie/commit/247794c613440dcb317ad9a8189c659ec2be8c63) codex: an account that is out takes over for other models too (reverses f37742e14)
  Decision: **different-desktop-runtime**. Desktop agent wiring, local installation/configuration, process/UI/library lifecycle has no corresponding server-gateway component.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [e17b6cefc7](https://github.com/yetone/magpie/commit/e17b6cefc7b25c19d1e522f9e8a9626bf68912d9) provider: the Qoder movers install opencode-qoder-auth 0.2.61 (plugins #692, #638)
  Decision: **different-provider-installer**. Version pins install external account plugins, not Axon protocol adapters.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [9a539e131e](https://github.com/yetone/magpie/commit/9a539e131e8a04a28f732fc9d0b32339e9e0f0e1) gateway: a GPT model on a Messages endpoint is asked its effort in output_config.effort (zxz8009195489)
  Decision: **covered-superset**. Axon converts explicit effort to Anthropic output_config based on platform support, without restricting it to GPT model-name regexes.
  Depth: selected patch and owning-source comparison. Evidence: llm/transformer/anthropic/outbound_convert.go; llm/transformer/anthropic/thinking.go.
- [43345c0ca8](https://github.com/yetone/magpie/commit/43345c0ca82a3b14ed2a9fbc59c56cd516f021fb) docs: gateway-routing names the GPT ids sent output_config.effort
  Decision: **documentation-only**. Documentation, planning or empty commit; no production gateway change.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [67bc50aa29](https://github.com/yetone/magpie/commit/67bc50aa29609078cbc72486e6bfda32b8592133) routing: a group switched off is listed after the ones on (justacati)
  Decision: **different-ui-or-test**. Peer GUI layout/local tooling regression has no matching Axon implementation; preserve current UI style.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [759b4d1d7d](https://github.com/yetone/magpie/commit/759b4d1d7da8b983860539e3bd8ce1eaf70a06a4) agents: a new user's list is folded under Get started (Renzexi01)
  Decision: **different-ui-or-test**. Peer GUI layout/local tooling regression has no matching Axon implementation; preserve current UI style.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [525e69b976](https://github.com/yetone/magpie/commit/525e69b976a569bf2143adff788f82d68cefa129) gui: a page served anew reloads once, not once per read (visamzh on X)
  Decision: **different-ui-or-test**. Peer GUI layout/local tooling regression has no matching Axon implementation; preserve current UI style.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [f4e52659a8](https://github.com/yetone/magpie/commit/f4e52659a8b4449a4676c59c3c65e33c414a023e) plugins: a pick listed as another id leaves with its model (axelburks)
  Decision: **defer-gateway-feature**. Peer request display, prompt/memory attribution or transport lifecycle is tied to its own gateway structures; no exact Axon defect established in this review.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [4ca55c6b62](https://github.com/yetone/magpie/commit/4ca55c6b62eeac30351b17ae37ad4f4dad9fd8e8) gateway: a guard test that Only switch Claude Code's sign-in keeps every route off the Claude accounts (zhangxiaoxiaoxx on X)
  Decision: **different-ui-or-test**. Peer GUI layout/local tooling regression has no matching Axon implementation; preserve current UI style.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [d1fa254b1d](https://github.com/yetone/magpie/commit/d1fa254b1d0d43bb636ea5a4d7d99bc511a12e0b) gui: masked words and Never mask are a list, a row per word (noahhrreion on Discord)
  Decision: **different-ui-or-test**. Peer GUI layout/local tooling regression has no matching Axon implementation; preserve current UI style.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [40cfd32b0b](https://github.com/yetone/magpie/commit/40cfd32b0b299d84dbeb001c3cfb6527d9b81cbd) provider: a Codex account with no credits and every window at 100% counts held though allowed isn't false (xiaoq6285)
  Decision: **peer-policy-reversal**. Peer first holds fully used accounts then changes to allowed=false alone. Retain Axon quota normalization instead of following a shifting desktop Send-gate rule.
  Depth: title/parent/file inventory across frozen range. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [efe11df3a0](https://github.com/yetone/magpie/commit/efe11df3a062c84da4b203722975bbbd5deaa934) gui: a press and release on a button with no click after it is clicked once (refeccd, Tap to click)
  Decision: **different-ui-or-test**. Peer GUI layout/local tooling regression has no matching Axon implementation; preserve current UI style.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [cd83c3147f](https://github.com/yetone/magpie/commit/cd83c3147fea120c53f789e5e80263fc5de66692) plugin: a hook's auth.set doesn't sign in again an account signed out while it ran (#5126, #5119)
  Decision: **defer-gateway-feature**. Peer request display, prompt/memory attribution or transport lifecycle is tied to its own gateway structures; no exact Axon defect established in this review.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [83bdfe70d2](https://github.com/yetone/magpie/commit/83bdfe70d2cfa5c166ff95f7896ab571c79c45cd) otlp: OTEL_RESOURCE_ATTRIBUTES and OTEL_SERVICE_NAME name the server a trace or metric came from (#5167)
  Decision: **different-observability**. Peer custom OTLP serialization differs from Axon OpenTelemetry integration.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [5957223d89](https://github.com/yetone/magpie/commit/5957223d89bdbf9f3aa51162103aabea44b7a0f9) library: a project's skills can be given to DeepSeek Harness, in the project's .agents/skills
  Decision: **different-desktop-runtime**. Desktop agent wiring, local installation/configuration, process/UI/library lifecycle has no corresponding server-gateway component.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [d6d3e6cc82](https://github.com/yetone/magpie/commit/d6d3e6cc829a8b5ca72bde3780d676ef773eaf10) provider: Devin's mover min is 0.1.36, which sends another model its earlier reasoning (plugins #703)
  Decision: **different-provider-installer**. Version pins install external account plugins, not Axon protocol adapters.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [1987fe220c](https://github.com/yetone/magpie/commit/1987fe220cd20cc3055da1ee19b74b4c5a20ec46) agent restart: a script run on the app's own executable is not the app (WorkBuddy's sidecar)
  Decision: **different-desktop-runtime**. Desktop agent wiring, local installation/configuration, process/UI/library lifecycle has no corresponding server-gateway component.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [d3c544ddf4](https://github.com/yetone/magpie/commit/d3c544ddf4d245e7b4b680bf2cac546b834b59b4) claude: a replayed transcript marks no earlier turn as the one to answer
  Decision: **different-agent-bridge**. Peer local Claude process/MCP/browser/search/statusline subsystem is not part of Axon protocol gateway.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [f177c36a9b](https://github.com/yetone/magpie/commit/f177c36a9bce287a735b01ef4c1a65fcbeaac559) provider: routed Until used up, Codex's sign-in stays on the account the requests went on to
  Decision: **defer-provider-policy**. Peer account/model/group pricing, quota display, refresh or scheduling policy uses different state. No equivalent Axon failure established; do not transplant policy or settings.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [f0b99745ed](https://github.com/yetone/magpie/commit/f0b99745ed059c3e7da5e21afcbd3d17b17aed46) provider: a Codex account is held on allowed false alone, as the 26.1029 app's Send gate reads it (xiaoq6285)
  Decision: **peer-policy-reversal**. Peer first holds fully used accounts then changes to allowed=false alone. Retain Axon quota normalization instead of following a shifting desktop Send-gate rule.
  Depth: title/parent/file inventory across frozen range. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [07f02d0d96](https://github.com/yetone/magpie/commit/07f02d0d96521c0f5c6a38ba7a86650c401bffc4) provider: moved Zed users get opencode-zed-auth 0.1.17, which drops a thinking display Zed refuses (plugins#708)
  Decision: **different-provider-installer**. Version pins install external account plugins, not Axon protocol adapters.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [c7bbd3faf6](https://github.com/yetone/magpie/commit/c7bbd3faf6de7c59ff65848450b0640a7ee2ecb1) routing: a Claude joined over the network shows its windows in Accounts and keys (yi_time on Discord)
  Decision: **different-ui-or-test**. Peer GUI layout/local tooling regression has no matching Axon implementation; preserve current UI style.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [0ee424e3ee](https://github.com/yetone/magpie/commit/0ee424e3ee61ae545d93160084a8d930b88a8193) codex: a thread Codex 0.166 won't resume for its provider moves with Codex (毅, AcMilan on Discord)
  Decision: **different-session-store**. Indexes or rewrites local agent transcript files; Axon request/history database is a different store and identity boundary.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [3eb2bda4f2](https://github.com/yetone/magpie/commit/3eb2bda4f2e3ba8aa0eb2c7870d9c746798687a1) settings: Codex, Claude Code and Grok Build each take a Long conversations of their own (molianyin4227 on Discord)
  Decision: **defer-gateway-feature**. Peer request display, prompt/memory attribution or transport lifecycle is tied to its own gateway structures; no exact Axon defect established in this review.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [95a0ee1b9e](https://github.com/yetone/magpie/commit/95a0ee1b9e767f9a122be6f299c19acf9248bc51) providers: a long model id no longer squeezes its name field away in Names, prices & levels (#5170, chiperman)
  Decision: **different-ui-or-test**. Peer GUI layout/local tooling regression has no matching Axon implementation; preserve current UI style.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [75e30625f5](https://github.com/yetone/magpie/commit/75e30625f510fe641ffcedce3f7aa165161d2fbf) plugin: a command that serves no pages moves a page's service off the app's port (#5187)
  Decision: **different-plugin-runtime**. Peer JS/provider plugin hooks, sign-in, search and page-service lifecycle are outside Axon runtime.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [2ba044aa66](https://github.com/yetone/magpie/commit/2ba044aa664bb50eaff44d156dfbd6b0f6f7d29b) gui: the Mac panel's page is told when Wails hides it, so it follows Settings' units (blrbrzkj)
  Decision: **different-desktop-runtime**. Desktop agent wiring, local installation/configuration, process/UI/library lifecycle has no corresponding server-gateway component.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [6b47bce670](https://github.com/yetone/magpie/commit/6b47bce670b45c99f4364c382fdf0f1f90f9f575) cli: magpie model compact <agent> sets an agent's own Long conversations
  Decision: **different-desktop-runtime**. Desktop agent wiring, local installation/configuration, process/UI/library lifecycle has no corresponding server-gateway component.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [f8987ce617](https://github.com/yetone/magpie/commit/f8987ce6173abacf503fb97f9bb36f63225c1824) gateway: Qoder 1.1.82's requests find their session in Qoder's own files
  Decision: **defer-gateway-feature**. Peer request display, prompt/memory attribution or transport lifecycle is tied to its own gateway structures; no exact Axon defect established in this review.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [17154e1654](https://github.com/yetone/magpie/commit/17154e16541b7435fecd064fd7b8b100e35cdb20) provider: a Claude account not used in five hours shows its 5-hour window unused, not missing (motui9535)
  Decision: **retain-unknown-quota**. Do not convert missing Claude 5h quota into unused merely from inactivity. Preserve distinction between unknown and zero usage.
  Depth: title and file inventory. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [186453e009](https://github.com/yetone/magpie/commit/186453e0093580e686c78fdddd9dbe9e21638117) provider: a plan key's card says when it was read (jiabubu)
  Decision: **defer-provider-policy**. Peer account/model/group pricing, quota display, refresh or scheduling policy uses different state. No equivalent Axon failure established; do not transplant policy or settings.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [129baeca01](https://github.com/yetone/magpie/commit/129baeca0187ea92a95c9c1602d787ce61941f34) gateway: the same 5xx from two accounts in one request rests neither (mycf_711)
  Decision: **retain-retry-policy**. Peer changes retry budgets, affinity retries, cooldowns or region/risk classification. These are routing policy changes rather than proven Axon defects; retain bounded retries and existing error contracts.
  Depth: title and file inventory. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [0e804be29b](https://github.com/yetone/magpie/commit/0e804be29b0ecf5b251b91f40845bbd1097025da) provider: requests missing together list logins once, and a plan asks OwnPaused of the held list
  Decision: **defer-provider-policy**. Peer account/model/group pricing, quota display, refresh or scheduling policy uses different state. No equivalent Axon failure established; do not transplant policy or settings.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [d5d48f4f9b](https://github.com/yetone/magpie/commit/d5d48f4f9b57e50ac9538817c9d642dd1e081abe) routing: a week at 90% renewing soonest is spent before its reset, not kept for last (ysicing_01858)
  Decision: **defer-provider-policy**. Peer account/model/group pricing, quota display, refresh or scheduling policy uses different state. No equivalent Axon failure established; do not transplant policy or settings.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [00d8aac596](https://github.com/yetone/magpie/commit/00d8aac59600b3af1f425127c08f6a93b8d734c8) routing: Free quota by expiry spends the soonest-lapsing free quota first (wowbbs)
  Decision: **defer-provider-policy**. Peer account/model/group pricing, quota display, refresh or scheduling policy uses different state. No equivalent Axon failure established; do not transplant policy or settings.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [9595a503a9](https://github.com/yetone/magpie/commit/9595a503a982c868cbe9b5465672d14fc2b4f855) provider: a Claude account made first stays first when Claude Code renews its sign-in under another's profile (motui9535)
  Decision: **defer-provider-policy**. Peer account/model/group pricing, quota display, refresh or scheduling policy uses different state. No equivalent Axon failure established; do not transplant policy or settings.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [009076a2a4](https://github.com/yetone/magpie/commit/009076a2a46bb9cd7a78f243d778f6cb492c1e96) gateway: group stand-in tests make Codex low by its 5 hours, a week at 92% is spent first (#5209)
  Decision: **different-ui-or-test**. Peer GUI layout/local tooling regression has no matching Axon implementation; preserve current UI style.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [8ac5f98e2a](https://github.com/yetone/magpie/commit/8ac5f98e2af56fc3bf934a7e761a43f58ab7f185) claude: an interactive run's side question branches after the whole reply, not after its tool call
  Decision: **different-agent-bridge**. Peer local Claude process/MCP/browser/search/statusline subsystem is not part of Axon protocol gateway.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [69eb93e0a2](https://github.com/yetone/magpie/commit/69eb93e0a2d27bb759de4ad7991c81d70dff35a3) antigravity: an account's tiers are asked again each hour, so one that became Ultra lists Gemini 4 without a restart (rick_49551 on Discord)
  Decision: **defer-gateway-feature**. Peer request display, prompt/memory attribution or transport lifecycle is tied to its own gateway structures; no exact Axon defect established in this review.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [b5b6cc5739](https://github.com/yetone/magpie/commit/b5b6cc5739a541a381418b93a3b03bab5dcae590) catalog: a listed model its id names a video model is one (agnes-video-2.5)
  Decision: **reverted-in-range**. Model-name video inference is reverted within this range; no net change to port.
  Depth: title/parent/file inventory. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [ef6bd155ff](https://github.com/yetone/magpie/commit/ef6bd155ff51450567d5f800cb790d49724492a1) provider: a remote magpie that holds this key says so, where its Claude card went missing with no word (yi_time on Discord)
  Decision: **defer-provider-policy**. Peer account/model/group pricing, quota display, refresh or scheduling policy uses different state. No equivalent Axon failure established; do not transplant policy or settings.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [14f6d10de0](https://github.com/yetone/magpie/commit/14f6d10de086a4d0b74d4a82d16fa1fa6a30f543) routing: a remote's Claude card whose read failed says so on its row, not nothing (yi_time on Discord)
  Decision: **different-ui-or-test**. Peer GUI layout/local tooling regression has no matching Axon implementation; preserve current UI style.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [c4245c4c20](https://github.com/yetone/magpie/commit/c4245c4c205c1dd758d20a3a07ebf4867e7f6ad3) gateway: a Messages body's tools with a root union go folded to every upstream
  Decision: **retain-schema-semantics**. Peer pre-folds root unions on all Messages requests. This may change tool parameter constraints and cache identity; no equivalent Axon rejection fixture justifies broad native schema rewriting.
  Depth: selected object_root.go patch. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [70a98dd85c](https://github.com/yetone/magpie/commit/70a98dd85c1c32f3a61ec31c90e7154d71612adf) routing: a long Accounts and keys shows its first rows, the rest behind Show N more (ferock4669, iyou00 on Discord)
  Decision: **different-ui-or-test**. Peer GUI layout/local tooling regression has no matching Axon implementation; preserve current UI style.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [e7b0c398ee](https://github.com/yetone/magpie/commit/e7b0c398ee7312672e5140960bdbb023be1f4730) sessions: find a ZCode title by its gateway id (#5195)
  Decision: **different-session-store**. Indexes or rewrites local agent transcript files; Axon request/history database is a different store and identity boundary.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [0a5dc40abc](https://github.com/yetone/magpie/commit/0a5dc40abc4fca45ca62ac548b1e8962c4202088) gateway: a group's trusted Claude, GPT and Gemini thinking streams as it comes, a refusal or error after it going to the next member (#4166)
  Decision: **retain-stream-boundary**. Peer failover after reasoning has already streamed can mix attempts and identities. Keep Axon established visibility/terminal-event recovery boundary.
  Depth: title and file inventory. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [1a1ca0351b](https://github.com/yetone/magpie/commit/1a1ca0351b0944d45deefe47a5edda79e1f264ef) Revert "catalog: a listed model its id names a video model is one (agnes-video-2.5)"
  Decision: **reverted-in-range**. Model-name video inference is reverted within this range; no net change to port.
  Depth: title/parent/file inventory. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [c03dee7e3e](https://github.com/yetone/magpie/commit/c03dee7e3e5694188e4599a33a69dfc27bfc0301) agent: LiveAgent, magpie's models in its model picker (xiaoxiaofei. on Discord)
  Decision: **different-desktop-runtime**. Desktop agent wiring, local installation/configuration, process/UI/library lifecycle has no corresponding server-gateway component.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [ce21ea8566](https://github.com/yetone/magpie/commit/ce21ea8566baa3e96c86b810694ae3576f570572) usage, routing: counts are said short (K/M, 万/亿), the exact number on hover (skyhuang1994)
  Decision: **different-ui-or-test**. Peer GUI layout/local tooling regression has no matching Axon implementation; preserve current UI style.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [0ebe6c72c9](https://github.com/yetone/magpie/commit/0ebe6c72c9998069cc1ce4f15a3cf1fd398ce454) providers: each account row in the editor has its own Check in now, when the check-in reaches it and it isn't in today (xwoffice_91592 on Discord)
  Decision: **defer-provider-policy**. Peer account/model/group pricing, quota display, refresh or scheduling policy uses different state. No equivalent Axon failure established; do not transplant policy or settings.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [7dd2434d31](https://github.com/yetone/magpie/commit/7dd2434d3174dc79a5925d71fd76b79b73437d7a) remote quotas: a key with Quotas on but told no card shows why too (yi_time)
  Decision: **defer-gateway-feature**. Peer request display, prompt/memory attribution or transport lifecycle is tied to its own gateway structures; no exact Axon defect established in this review.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [78128b5101](https://github.com/yetone/magpie/commit/78128b51015b28eb45af62b8dd7177ba1e857dff) provider: a plugin's thinking model listed as, or the same as, another takes that model's levels (axelburks)
  Decision: **defer-provider-policy**. Peer account/model/group pricing, quota display, refresh or scheduling policy uses different state. No equivalent Axon failure established; do not transplant policy or settings.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [461f9b1ff9](https://github.com/yetone/magpie/commit/461f9b1ff9f01af7555ca4d84ce0e2e589456ad1) providers: a model chip's right-click turns its image input on or off (archsiono on X)
  Decision: **different-ui-or-test**. Peer GUI layout/local tooling regression has no matching Axon implementation; preserve current UI style.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [844c355059](https://github.com/yetone/magpie/commit/844c3550591dc61ab14ca866b94499ea409d322a) dsh: the model shown is the start of the profile in use, and the round's log says what it wrote (#5222)
  Decision: **different-desktop-runtime**. Desktop agent wiring, local installation/configuration, process/UI/library lifecycle has no corresponding server-gateway component.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [9095fd82d8](https://github.com/yetone/magpie/commit/9095fd82d8ba15e17105c07a8647b254a30cbeba) gateway: a conversation kept for its cache is asked again once on a passing 5xx before it moves (jiabubu)
  Decision: **retain-retry-policy**. Peer changes retry budgets, affinity retries, cooldowns or region/risk classification. These are routing policy changes rather than proven Axon defects; retain bounded retries and existing error contracts.
  Depth: title and file inventory. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [7353a4bc2a](https://github.com/yetone/magpie/commit/7353a4bc2a5252b6077c36ef542ebd1a8aaf85d9) gateway: a row told an earlier try's 529 overload says its kind, not the last account's usage_limit_reached (蛋仔)
  Decision: **defer-gateway-feature**. Peer request display, prompt/memory attribution or transport lifecycle is tied to its own gateway structures; no exact Axon defect established in this review.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [1a67ca614e](https://github.com/yetone/magpie/commit/1a67ca614ed6a8180eaf6ea0442dd1272e43d603) provider: Fetch models refreshes each key's own models, keeping what the user un-ticked (young02036)
  Decision: **defer-provider-policy**. Peer account/model/group pricing, quota display, refresh or scheduling policy uses different state. No equivalent Axon failure established; do not transplant policy or settings.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [38f4f74e62](https://github.com/yetone/magpie/commit/38f4f74e62fb2e1e0dab0166a2edb44ef72d5a3f) droid: a reasoning model's entry has reasoningEffort, a built-in's baseModelId
  Decision: **different-desktop-runtime**. Desktop agent wiring, local installation/configuration, process/UI/library lifecycle has no corresponding server-gateway component.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [0f1b40bbc4](https://github.com/yetone/magpie/commit/0f1b40bbc41384c63d7a3c8fc75b4cfe9f5c9483) gateway: a reply in flight shows on Routing how much has arrived and roughly its tokens (#4951, Canda15)
  Decision: **defer-gateway-feature**. Peer request display, prompt/memory attribution or transport lifecycle is tied to its own gateway structures; no exact Axon defect established in this review.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [43849d51c3](https://github.com/yetone/magpie/commit/43849d51c3273881839a8ef3e5aa7109bd2c8d36) routing: By session heads a session by its title, its folder quieter beside it (flandern1211_58540 on Discord)
  Decision: **different-desktop-runtime**. Desktop agent wiring, local installation/configuration, process/UI/library lifecycle has no corresponding server-gateway component.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [2f726b1f95](https://github.com/yetone/magpie/commit/2f726b1f95efd68070246b5e71917b65ae46d807) gateway: a request its vendor's risk control refused isn't sent on to the provider's other accounts (_teamtaylor_)
  Decision: **retain-retry-policy**. Peer changes retry budgets, affinity retries, cooldowns or region/risk classification. These are routing policy changes rather than proven Axon defects; retain bounded retries and existing error contracts.
  Depth: title and file inventory. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [f44fa2ef6a](https://github.com/yetone/magpie/commit/f44fa2ef6ad5d98510686244a448c5ed41daf00f) sessions: gateway mode lists only the gateway's sessions, never this computer's agents' files
  Decision: **different-session-store**. Indexes or rewrites local agent transcript files; Axon request/history database is a different store and identity boundary.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [acd789d452](https://github.com/yetone/magpie/commit/acd789d4527d93f6116312e9fc139ea51d637780) gateway: a finished route drops its tool scan, so socket turn bodies are freed
  Decision: **different-memory-lifetime**. Tool-scan and unrouted timer references are Magpie-specific WebSocket tracing structures; no matching retained Axon object identified.
  Depth: title and file inventory. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [083821dc96](https://github.com/yetone/magpie/commit/083821dc96d4542a730b96a21e1b7fe7df9d15d1) plugins: a search entry can use the plugin's own sign-in, with its own rests
  Decision: **different-plugin-runtime**. Peer JS/provider plugin hooks, sign-in, search and page-service lifecycle are outside Axon runtime.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [93d5c76c0d](https://github.com/yetone/magpie/commit/93d5c76c0d3f1786b8cd1dfdd64159e78070324a) router: an agent with many tools is given them on demand
  Decision: **different-agent-bridge**. Peer local Claude process/MCP/browser/search/statusline subsystem is not part of Axon protocol gateway.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [532d054bee](https://github.com/yetone/magpie/commit/532d054bee7ce33cbc7b5c003c3603804f70b5d4) sessions: every Cursor row has a delete, and the right-hand column lines up
  Decision: **different-session-store**. Indexes or rewrites local agent transcript files; Axon request/history database is a different store and identity boundary.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [7564e46690](https://github.com/yetone/magpie/commit/7564e466903ab5e1b262baed18bcfbbf85182206) library: a subagent names its model, written into Codex's role as model (#4421)
  Decision: **different-desktop-runtime**. Desktop agent wiring, local installation/configuration, process/UI/library lifecycle has no corresponding server-gateway component.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [4062cda781](https://github.com/yetone/magpie/commit/4062cda781b084197a26c346119093f51f185ab5) provider: a custom provider at a Token Plan's or Ark's address is asked for no access key (ysm119)
  Decision: **defer-provider-policy**. Peer account/model/group pricing, quota display, refresh or scheduling policy uses different state. No equivalent Axon failure established; do not transplant policy or settings.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [b61c71448c](https://github.com/yetone/magpie/commit/b61c71448c4fbbce97a664efb1b72e70633dc454) keys: a custom provider's key can be made for its Gemini API (corddis_114)
  Decision: **defer-provider-policy**. Peer account/model/group pricing, quota display, refresh or scheduling policy uses different state. No equivalent Axon failure established; do not transplant policy or settings.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [7818dc316a](https://github.com/yetone/magpie/commit/7818dc316a71f50afcd70a9d7e5eb4e040ec661f) gateway: a relay's Cloudflare 520 to 524 is asked again before the agent sees it, as a 502 or 503 is (vdaoren)
  Decision: **retain-retry-policy**. Peer changes retry budgets, affinity retries, cooldowns or region/risk classification. These are routing policy changes rather than proven Axon defects; retain bounded retries and existing error contracts.
  Depth: title and file inventory. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [dd50cbba25](https://github.com/yetone/magpie/commit/dd50cbba258b537e86a5cde1e06eebdfa2165202) provider: a key's or account's model picker has Select all available (young02036)
  Decision: **defer-provider-policy**. Peer account/model/group pricing, quota display, refresh or scheduling policy uses different state. No equivalent Axon failure established; do not transplant policy or settings.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [a1f1810e93](https://github.com/yetone/magpie/commit/a1f1810e93c086c435e977e34691a1ea92347cd1) gateway: a 400 that only says the request is too long counts as an overflow once it is past half the model's window (#4420, MYCF711)
  Decision: **retain-context-evidence**. Peer infers overflow from generic too-long wording and half-window estimates. Keep explicit context-limit evidence rather than broad automatic compaction.
  Depth: title and file inventory. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [1b579ca4f8](https://github.com/yetone/magpie/commit/1b579ca4f8ed5bcf32eb020a06223b965b37b42f) router: Droid and omp get the hub's tools whole; find_tools' description names each server and its tools
  Decision: **different-agent-bridge**. Peer local Claude process/MCP/browser/search/statusline subsystem is not part of Axon protocol gateway.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [34116ec012](https://github.com/yetone/magpie/commit/34116ec012f2f0d1474f034e4eb849d2d2507e18) gateway: Codex signed in to one account lists another account's models
  Decision: **retain-model-visibility**. Peer group model filtering and cross-account Codex model lists differ from Axon channel/owner-scoped catalogs. Do not broaden account capability or model visibility.
  Depth: title and file inventory. Evidence: internal/server/biz/codex_catalog.go.
- [11d9d0a3a5](https://github.com/yetone/magpie/commit/11d9d0a3a52a0060e93dc548fb5e3a7bce7bc1a9) routing: each account one calm cell in Accounts and keys (danzi002 on Discord)
  Decision: **different-ui-or-test**. Peer GUI layout/local tooling regression has no matching Axon implementation; preserve current UI style.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [8a80796805](https://github.com/yetone/magpie/commit/8a807968059b63561bab80a027ec922b1f21c3df) gateway: the native route passes any path of a keyed provider's own API on, at its host
  Decision: **defer-native-media-feature**. Ark native paths/media overrides or Copilot image generation require a dedicated provider/endpoint contract. Existing Axon image response-format behavior remains; do not add arbitrary native proxy endpoints here.
  Depth: title and file inventory. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [d02b8a000e](https://github.com/yetone/magpie/commit/d02b8a000e1d1736b3b98b5ba7ed06132e75ad17) gui: an agent's own models' section says it is the agent's own sign-in (maxleebeta)
  Decision: **different-ui-or-test**. Peer GUI layout/local tooling regression has no matching Axon implementation; preserve current UI style.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [538026e62b](https://github.com/yetone/magpie/commit/538026e62b68c69609b67829da6ff614097e40fa) gateway: an error frame inside a 200 fails at the status it names (#4855)
  Decision: **covered-partial**. Responses errors already preserve numeric status/status_code with explicit precedence and tests. Do not adopt broad text-based HTTP status guessing or Magpie-specific headers across every protocol.
  Depth: selected patch and owning-source comparison. Evidence: llm/transformer/openai/responses/error.go; llm/transformer/openai/responses/error_detail_test.go.
- [a268aadc40](https://github.com/yetone/magpie/commit/a268aadc40615e39ee1e4ef60bb334ce2f10cc79) gateway: a stopped unrouted timer lets go of its request (#5250)
  Decision: **different-memory-lifetime**. Tool-scan and unrouted timer references are Magpie-specific WebSocket tracing structures; no matching retained Axon object identified.
  Depth: title and file inventory. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [eeecd41505](https://github.com/yetone/magpie/commit/eeecd415054c584420ba42aa0e405d4b1d513304) privacy: masked words and never-mask values are imported in bulk and copied out (acmilan_)
  Decision: **different-ui-or-test**. Peer GUI layout/local tooling regression has no matching Axon implementation; preserve current UI style.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [d7b1145d18](https://github.com/yetone/magpie/commit/d7b1145d18097da0e1ee09690a2e51126dabccc9) gui: English text no longer carries Chinese words (fangleeway)
  Decision: **different-ui-or-test**. Peer GUI layout/local tooling regression has no matching Axon implementation; preserve current UI style.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [f73c2fc8f0](https://github.com/yetone/magpie/commit/f73c2fc8f0ecf19cbe644356dd53609a2d699e9c) gui tests: zhipu-team fills the key, not the proxy's password (#4728)
  Decision: **different-ui-or-test**. Peer GUI layout/local tooling regression has no matching Axon implementation; preserve current UI style.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [50aaa57268](https://github.com/yetone/magpie/commit/50aaa572684dc575c631a85d2b4029aa3bf99566) agents: Codex's Lead's pick, already chosen, still takes away the config's subagent model
  Decision: **different-ui-or-test**. Peer GUI layout/local tooling regression has no matching Axon implementation; preserve current UI style.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [127f1e77dc](https://github.com/yetone/magpie/commit/127f1e77dc2a5da03a2d5498c7091854c1bbc75f) gateway: a turn to another magpie goes as its new input after the turn it kept
  Decision: **different-relay-protocol**. Remote-Magpie delta forwarding relies on its retained-turn protocol. Axon already owns its native Responses continuation/checkpoint chain; no interoperability contract warrants replacing it.
  Depth: title and file inventory. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [823e7c250b](https://github.com/yetone/magpie/commit/823e7c250bc9ecb82827d7c40e4765c09101ba75) providers: Claude Code's and Codex's Accounts say switching needs no connecting (Greatrobbort on X)
  Decision: **different-ui-or-test**. Peer GUI layout/local tooling regression has no matching Axon implementation; preserve current UI style.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [ef1eebd3cd](https://github.com/yetone/magpie/commit/ef1eebd3cd16acfe6f23777d80a93df1db08d549) gateway: a test waits for a held request's report before its HOME goes
  Decision: **defer-gateway-feature**. Peer request display, prompt/memory attribution or transport lifecycle is tied to its own gateway structures; no exact Axon defect established in this review.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [7b5e773873](https://github.com/yetone/magpie/commit/7b5e77387329512c0bccdbe365e0defaeacc4ede) provider: a model at a window (claude-opus-5-5@300k) is priced as its model, so Cursor's sized ids have a cost
  Decision: **defer-provider-policy**. Peer account/model/group pricing, quota display, refresh or scheduling policy uses different state. No equivalent Axon failure established; do not transplant policy or settings.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [cbb09383fb](https://github.com/yetone/magpie/commit/cbb09383fb4a32f5b3518b39421dcf6c36dd8532) plugin: a GitHub-tagged package's card says its own name and summary (#3608, plugins#394)
  Decision: **different-plugin-runtime**. Peer JS/provider plugin hooks, sign-in, search and page-service lifecycle are outside Axon runtime.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [6bef4fe043](https://github.com/yetone/magpie/commit/6bef4fe0436184f6be393fcf1f549b2941f97b83) docs: a Library page in en, zh and ja, linked from every docs page
  Decision: **documentation-only**. Documentation, planning or empty commit; no production gateway change.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [d6e3a36ffc](https://github.com/yetone/magpie/commit/d6e3a36ffcc9a5830e1f800a18d488afd16153d8) codex: threads of the provider Codex was on before magpie follow it onto magpie (hxy920830 on X)
  Decision: **different-session-store**. Indexes or rewrites local agent transcript files; Axon request/history database is a different store and identity boundary.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [e928b57f0e](https://github.com/yetone/magpie/commit/e928b57f0ed4ea86539d0f4dc5843d662a17dff5) browser: the user's tab is never put behind, not even for a moment
  Decision: **different-agent-bridge**. Peer local Claude process/MCP/browser/search/statusline subsystem is not part of Axon protocol gateway.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [f280b12e51](https://github.com/yetone/magpie/commit/f280b12e51403c898c6a580ba58138b16c6a7cd8) library: the Copies note says an edit in a copy goes to the library and the other agents
  Decision: **different-ui-or-test**. Peer GUI layout/local tooling regression has no matching Axon implementation; preserve current UI style.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [387b386c84](https://github.com/yetone/magpie/commit/387b386c8427e8dc3e61a1fc0795e85018448721) codex: threads of the provider an older beside wiring took down move to OpenAI (yikelizi9438 on Discord)
  Decision: **different-session-store**. Indexes or rewrites local agent transcript files; Axon request/history database is a different store and identity boundary.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [5910b51244](https://github.com/yetone/magpie/commit/5910b51244b17238ec0b0332c2210676d7fb7d04) gui: Settings › Sync's server form loads with syncfiles.js, out of the core (#5139)
  Decision: **different-ui-or-test**. Peer GUI layout/local tooling regression has no matching Axon implementation; preserve current UI style.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [dd61299231](https://github.com/yetone/magpie/commit/dd612992311b3af4670db909cac5e97c670691aa) claudetui: /usage's saved figures, drawn while Claude Code refreshes them, are not told as today's
  Decision: **defer-gateway-feature**. Peer request display, prompt/memory attribution or transport lifecycle is tied to its own gateway structures; no exact Axon defect established in this review.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [0a6cccc3ff](https://github.com/yetone/magpie/commit/0a6cccc3ffd974e7bf421d3ac4fbd7e1cb9e0512) groups: a found group the user changed takes in a new provider with its model
  Decision: **defer-provider-policy**. Peer account/model/group pricing, quota display, refresh or scheduling policy uses different state. No equivalent Axon failure established; do not transplant policy or settings.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [e3ed21c7a2](https://github.com/yetone/magpie/commit/e3ed21c7a2fbba6bb940097472e324530cc2f3ae) provider: the ZCode mover's min is 0.1.50 (plugins#729)
  Decision: **different-provider-installer**. Version pins install external account plugins, not Axon protocol adapters.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [9d0c9b41ea](https://github.com/yetone/magpie/commit/9d0c9b41eab677e904b38f1cd413099f05528ff9) serve: follow an update put in where it runs from; a replaced magpie.real is magpie
  Decision: **different-desktop-runtime**. Desktop agent wiring, local installation/configuration, process/UI/library lifecycle has no corresponding server-gateway component.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [8e97e26d67](https://github.com/yetone/magpie/commit/8e97e26d6789610160fc7a16afbad5d53baa4ce0) providers: WorkBuddy's sign-in warns that Tencent may ban the account first
  Decision: **defer-provider-policy**. Peer account/model/group pricing, quota display, refresh or scheduling policy uses different state. No equivalent Axon failure established; do not transplant policy or settings.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [7661a0aa72](https://github.com/yetone/magpie/commit/7661a0aa7299f9519e257a5c9ac17976af1da791) provider: Fetch models ticks each key's own models, the first key's too (young02036)
  Decision: **defer-provider-policy**. Peer account/model/group pricing, quota display, refresh or scheduling policy uses different state. No equivalent Axon failure established; do not transplant policy or settings.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [4a9cfb52e5](https://github.com/yetone/magpie/commit/4a9cfb52e597be73d597f9560497e82250c3d5a0) usage: a plugin account told by its balance alone is drawn over time
  Decision: **defer-gateway-feature**. Peer request display, prompt/memory attribution or transport lifecycle is tied to its own gateway structures; no exact Axon defect established in this review.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [18726b9fb6](https://github.com/yetone/magpie/commit/18726b9fb618d92446e10b18b999658da4a889ba) statusline: a held key is told how to see its line, ?format=json, and /v1/magpie/limit says what holds a key (steven_4422)
  Decision: **different-agent-bridge**. Peer local Claude process/MCP/browser/search/statusline subsystem is not part of Axon protocol gateway.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [c32b820a91](https://github.com/yetone/magpie/commit/c32b820a91e63f2737aee3eb46700449ecba8dde) gateway: a "User location is not supported" refusal rests no account, and the next try goes on a new connection (#5182)
  Decision: **retain-retry-policy**. Peer changes retry budgets, affinity retries, cooldowns or region/risk classification. These are routing policy changes rather than proven Axon defects; retain bounded retries and existing error contracts.
  Depth: title and file inventory. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [2d04fe73dd](https://github.com/yetone/magpie/commit/2d04fe73ddaf9470be139078a2583a82f58a0c17) gateway: Antigravity's tool enums go without a blank value (#5235)
  Decision: **retain-schema-values**. Removing blank enum/const values changes accepted tool arguments. Do not silently drop valid schema data for all Antigravity requests from a peer-only error report.
  Depth: selected codeassist.go patch. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [d17a0b3ca0](https://github.com/yetone/magpie/commit/d17a0b3ca0ca4fd4585bb57354b5e00d09091249) usage: opening the Usage page no longer stutters (_hii11 on Discord)
  Decision: **different-ui-or-test**. Peer GUI layout/local tooling regression has no matching Axon implementation; preserve current UI style.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [79477604a5](https://github.com/yetone/magpie/commit/79477604a516a0d58aea876ba9b0afdcd6558b1e) providers: a plugin option that says placement "top" sits under the accounts, above Routing (#5269)
  Decision: **different-plugin-runtime**. Peer JS/provider plugin hooks, sign-in, search and page-service lifecycle are outside Axon runtime.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [fd85c0a1e9](https://github.com/yetone/magpie/commit/fd85c0a1e9c363df173fe9e947b4136ffe5b0600) gateway: a test's memory root reads nothing of the machine it runs on (#5118)
  Decision: **defer-gateway-feature**. Peer request display, prompt/memory attribution or transport lifecycle is tied to its own gateway structures; no exact Axon defect established in this review.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [676b143175](https://github.com/yetone/magpie/commit/676b1431757e62463bc00e14fe9d27b6334cab3e) gateway: a model refused for the user's region is asked once more, then the agent is told Google's restriction on that model (#5182)
  Decision: **retain-retry-policy**. Peer changes retry budgets, affinity retries, cooldowns or region/risk classification. These are routing policy changes rather than proven Axon defects; retain bounded retries and existing error contracts.
  Depth: title and file inventory. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [ab5191f8d7](https://github.com/yetone/magpie/commit/ab5191f8d789f85e42901e5af5cf001ee93c0aad) provider: Zed's mover min is 0.1.18, which names each request's thread (plugins#731)
  Decision: **different-provider-installer**. Version pins install external account plugins, not Axon protocol adapters.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [775630dac9](https://github.com/yetone/magpie/commit/775630dac947f1ad08f487c94ad03f4b63c86da9) usage: the compaction picker says each window against the setting now
  Decision: **different-ui-or-test**. Peer GUI layout/local tooling regression has no matching Axon implementation; preserve current UI style.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [83137bcbe9](https://github.com/yetone/magpie/commit/83137bcbe971fced599a9e81d42211e1f552b2e0) sessions: TestCCListingKeepsItsNames counts the names' allocations, not Linux's stats (#4925)
  Decision: **different-ui-or-test**. Peer GUI layout/local tooling regression has no matching Axon implementation; preserve current UI style.
  Depth: title, parent and file inventory screening. Evidence: peer changed-file inventory; no Axon equivalence claimed.

## cc-switch: 9 new records

`7afceec14b977ecc2f4373e8ffb40e7223b435ac` → `ca58c0313360eae7c851fbd7553c1614c7af03e7`; frozen `2026-11-04T17:13:24.323694+00:00`.

- [51fce968bc](https://github.com/farion1231/cc-switch/commit/51fce968bca807aeee93be9420c651afb582c2c8) fix(presets): refresh stale default models (grok-4.7, kimi-k3.1-pro, qwen3.7-plus) (#9193)
  Decision: **different-desktop-presets**. Desktop provider defaults and Codex config editor concurrency fields do not change gateway wire contracts. Do not modify user CLI configuration or catalog from these presets.
  Depth: title and file inventory. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [b8fa4199c1](https://github.com/farion1231/cc-switch/commit/b8fa4199c10b4c8f79546da46b972ef89356ab0d) fix(codex): follow up on sub-agent concurrency limit review notes (#9148)
  Decision: **different-desktop-presets**. Desktop provider defaults and Codex config editor concurrency fields do not change gateway wire contracts. Do not modify user CLI configuration or catalog from these presets.
  Depth: title and file inventory. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [e217bfcbbb](https://github.com/farion1231/cc-switch/commit/e217bfcbbbbbf82f4619ab62f08fb002635b7858) perf(proxy): reduce forwarding allocations and failover database reads (#9161)
  Decision: **defer-performance-port**. Broad Rust/Tauri allocation, database and SSE refactor is not a portable Go fix. No Axon performance trace establishes the same bottleneck; retain current payload/stream lifetimes.
  Depth: title and file inventory. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [e78784715e](https://github.com/farion1231/cc-switch/commit/e78784715ebc0c15922ca5b37e57c328a0d08545) fix(proxy): stop classifier retry loops (#9201)
  Decision: **defer-client-loop-policy**. Blocks a client after three tool-result turns matching classifier-unavailable text. This is a new cross-turn admission policy, distinct from bounded gateway retries and safeguards. No Axon incident/contract establishes that threshold; do not infer tool outcome semantics from strings.
  Depth: selected classifier_guard.rs and forwarder.rs. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [c70d61de0c](https://github.com/farion1231/cc-switch/commit/c70d61de0c38f833821eafd201d876e37046a251) fix(linux): bump @tauri-apps/cli so the AppImage stops bundling libwayland-client (#9229)
  Decision: **different-desktop-runtime**. Tauri UI scheduling, AppImage dependencies, session reader layout and renderer lazy loading have no Axon counterpart.
  Depth: title and file inventory. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [d16064bde9](https://github.com/farion1231/cc-switch/commit/d16064bde9f63484e7eea0537282a9599f4930f8) fix(sessions): improve reader toolbar layout and metadata wrapping (#9109)
  Decision: **different-desktop-runtime**. Tauri UI scheduling, AppImage dependencies, session reader layout and renderer lazy loading have no Axon counterpart.
  Depth: title and file inventory. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [98777515d5](https://github.com/farion1231/cc-switch/commit/98777515d512519209510bce6352ae1984988c81) fix(ui): keep the remaining DB-lock-waiting sync commands off the UI thread (#9178)
  Decision: **different-desktop-runtime**. Tauri UI scheduling, AppImage dependencies, session reader layout and renderer lazy loading have no Axon counterpart.
  Depth: title and file inventory. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [fc9b17cbfd](https://github.com/farion1231/cc-switch/commit/fc9b17cbfdc6bf330f3c46a0db761e0e02ed4f0d) perf(db): batch provider endpoint reads and skip unchanged saves (#9180)
  Decision: **different-storage**. Batch endpoint reads and skip-unchanged saves target the peer SQLite provider DAO; no shared data access implementation.
  Depth: title and file inventory. Evidence: peer changed-file inventory; no Axon equivalence claimed.
- [ca58c03133](https://github.com/farion1231/cc-switch/commit/ca58c0313360eae7c851fbd7553c1614c7af03e7) perf(ui): lazy-load secondary pages and provider editors (#9181)
  Decision: **different-desktop-runtime**. Tauri UI scheduling, AppImage dependencies, session reader layout and renderer lazy loading have no Axon counterpart.
  Depth: title and file inventory. Evidence: peer changed-file inventory; no Axon equivalence claimed.

