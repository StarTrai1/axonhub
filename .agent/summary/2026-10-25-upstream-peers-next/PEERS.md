# Frozen upstream and peer review

User cutoff: 2026-10-25T00:07:00+08:00. previous frozen head ancestry, not author timestamps; prior API freezes were 00:08:57–00:09:01 Beijing. Late merged commits with older author dates remain included.

5 upstream records; 25 peer records (22 unique nonmerge commits and 3 merge records). CLIProxyAPI and OmniProxy have no new commits. The current snapshot was frozen at 2026-10-25 21:51:28 Beijing.

## upstream

Repository: looplj/axonhub (unstable).
Previous: `a7ad1f9b7a49fcb7bf305d5e25ae266cb1d97580` at 2026-10-24T16:08:57.337157+00:00.
Frozen: `66ad2fc37ce0f7683a52a9238e8b38fc17e7ccef` at 2026-10-25T13:51:28.567092+00:00.

- [e6c67fe43b](https://github.com/looplj/axonhub/commit/e6c67fe43b909de811cd9d6358c37e744feb9fa4) **merge** — fix(ui): repair provider quota popover grouping and sort (#2751). Quota grouping/sorting and persisted display controls; retain fork paid-reset AlertDialog and account presentation.
- [3cd5b590fd](https://github.com/looplj/axonhub/commit/3cd5b590fdf73e084e3862fb43bea69c70ae3d7c) **merge-adapt** — ci: add Go vulnerability scanning (#2753). Go vulnerability scanning and x/* updates; enable dev triggers and use .go-version. Dependency reconciliation stays in CI.
- [1c41403da1](https://github.com/looplj/axonhub/commit/1c41403da1463f70429548de6b459b0605d456d5) **merge-adapt** — fix(users): use AutoCompleteSelect for Add User picker (#2752). Paginated server email search in project Add User; preserve IME guards, add cached selected label across empty pages, dialog portal/a11y regression and schema-inferred query type.
- [07ce14d8ae](https://github.com/looplj/axonhub/commit/07ce14d8ae048983c23eb275629fe11fc3cdc2a0) **covered** — fix(system): raise the per-channel max retry limit to 10 (#2663). Per-channel maximum retry count of 10 already present on baseline; upstream adds no behavior.
- [66ad2fc37c](https://github.com/looplj/axonhub/commit/66ad2fc37ce0f7683a52a9238e8b38fc17e7ccef) **merge-adapt** — chore: sync model developers data (#2693). Merge catalog updates with newer fork entries; restore malformed Chinese structured-output label in both catalog copies.

## sub2api

Repository: Wei-Shaw/sub2api (main).
Previous: `37067e8e340e9440d173a92b4faa05e8ce58368f` at 2026-10-24T16:09:01.807855+00:00.
Frozen: `ba81c7fd758e425d67f856d601147ef0c5263d3f` at 2026-10-25T13:51:28.638200+00:00.

- [fd998129ec](https://github.com/Wei-Shaw/sub2api/commit/fd998129ec8375c6623e061c7e5e391752dc954a) **skip-architecture** — feat(billing): support per-image billing for multi-image requests. Peer introduces group image_count_billing, endpoint/pricing gates and count fallback policy. AxonHub cost_calc.go uses token price items and lacks this group override/per-image billing pipeline. Not claimed a superset; do not silently introduce charging policy.
- [1487fa511b](https://github.com/Wei-Shaw/sub2api/commit/1487fa511bffbb26ebcebb82a717264c84ffb3a0) **skip-architecture** — fix(billing): count only images returned in upstream response. Peer introduces group image_count_billing, endpoint/pricing gates and count fallback policy. AxonHub cost_calc.go uses token price items and lacks this group override/per-image billing pipeline. Not claimed a superset; do not silently introduce charging policy.
- [cb2959245f](https://github.com/Wei-Shaw/sub2api/commit/cb2959245f7ab452e152a2e77bf811f9be66cbfa) **skip-architecture** — test(api): include image_count_billing in available groups contract. API tests depend on peer image_count_billing group contract, which this fork does not implement.
- [aa1a1b11c8](https://github.com/Wei-Shaw/sub2api/commit/aa1a1b11c864a009ddd83e1f9010b0f92bd1da0b) **skip-architecture** — fix(billing): gate per-image billing by endpoint and pricing mode. Peer introduces group image_count_billing, endpoint/pricing gates and count fallback policy. AxonHub cost_calc.go uses token price items and lacks this group override/per-image billing pipeline. Not claimed a superset; do not silently introduce charging policy.
- [3f1c8f36ce](https://github.com/Wei-Shaw/sub2api/commit/3f1c8f36ce8c2cd378015830a90e89970b9a9ea7) **skip-doc-assets** — chore: update Sponsors. Sponsor updates have no runtime relevance to this fork.
- [3ce09c8aff](https://github.com/Wei-Shaw/sub2api/commit/3ce09c8aff085ce42538d539d515742d8650e6f0) **skip-architecture** — fix(billing): keep tokens for channel per-request image tiers. Peer introduces group image_count_billing, endpoint/pricing gates and count fallback policy. AxonHub cost_calc.go uses token price items and lacks this group override/per-image billing pipeline. Not claimed a superset; do not silently introduce charging policy.
- [ba81c7fd75](https://github.com/Wei-Shaw/sub2api/commit/ba81c7fd758e425d67f856d601147ef0c5263d3f) **merge-duplicate** — Merge pull request #8323 from wx-11/images-price. Merge record reuses decisions for its already reviewed component commits; not counted as an independent patch review.

## CLIProxyAPI

Repository: router-for-me/CLIProxyAPI (main).
Previous: `bc0643405a1d0adf77dd0f109972ac3313d66397` at 2026-10-24T16:08:57.122356+00:00.
Frozen: `bc0643405a1d0adf77dd0f109972ac3313d66397` at 2026-10-25T13:51:28.480331+00:00.


## OmniProxy

Repository: mibgb65-cloud/OmniProxy (master).
Previous: `56448d20ccd797bc32aae1c02a5e2f2030b8b6c8` at 2026-10-24T16:08:57.161235+00:00.
Frozen: `56448d20ccd797bc32aae1c02a5e2f2030b8b6c8` at 2026-10-25T13:51:28.686552+00:00.


## codex2api

Repository: james-6-23/codex2api (main).
Previous: `1511374d878ad5a27fc7a3293aa2fb67eb5f96d4` at 2026-10-24T16:08:57.471922+00:00.
Frozen: `d94e6ccd20339867389896d9b1cbe5464da7a797` at 2026-10-25T13:51:28.758394+00:00.

- [1957d8d7fd](https://github.com/james-6-23/codex2api/commit/1957d8d7fd5c8c1b7876d4b1870d429837aae80d) **skip-architecture** — feat(proxy): 统一 Codex 出站 attestation. Peer native Live sideband/generated platform attestation subsystem is absent. Do not synthesize device identity, transplant OS attestation, or broaden default Live permissions without the corresponding protocol/permission architecture.
- [6a4d8a15c1](https://github.com/james-6-23/codex2api/commit/6a4d8a15c1d7700b3fd66b7f09eccc7502925b02) **skip-architecture** — feat(proxy): 接入统一 Live attestation 与会话快照. Peer native Live sideband/generated platform attestation subsystem is absent. Do not synthesize device identity, transplant OS attestation, or broaden default Live permissions without the corresponding protocol/permission architecture.
- [317b0815f2](https://github.com/james-6-23/codex2api/commit/317b0815f2dad948d42361bbef3cfd4e4787e719) **skip-architecture** — fix(proxy): 修正 Codex OAuth Live sideband 端点. Peer native Live sideband/generated platform attestation subsystem is absent. Do not synthesize device identity, transplant OS attestation, or broaden default Live permissions without the corresponding protocol/permission architecture.
- [2b19913e54](https://github.com/james-6-23/codex2api/commit/2b19913e5444d5709b38ca67a0263dec3124a1e1) **skip-architecture** — feat(admin): 默认允许自动渠道 API Key 使用 ChatGPT Live. Peer native Live sideband/generated platform attestation subsystem is absent. Do not synthesize device identity, transplant OS attestation, or broaden default Live permissions without the corresponding protocol/permission architecture.
- [73c9ae32d2](https://github.com/james-6-23/codex2api/commit/73c9ae32d2b72208e479713466e012645effe019) **skip-architecture** — fix(admin): 精简 ChatGPT Live 权限提示文案. Peer native Live sideband/generated platform attestation subsystem is absent. Do not synthesize device identity, transplant OS attestation, or broaden default Live permissions without the corresponding protocol/permission architecture.
- [33444ca9fd](https://github.com/james-6-23/codex2api/commit/33444ca9fd5c83e171da909879aced8ca6bba019) **skip-architecture** — fix(proxy): 仅使用本项目生成的 Codex attestation. Peer native Live sideband/generated platform attestation subsystem is absent. Do not synthesize device identity, transplant OS attestation, or broaden default Live permissions without the corresponding protocol/permission architecture.
- [fe8695331d](https://github.com/james-6-23/codex2api/commit/fe8695331d80fa493c420b87b241ca4f6e119510) **adapt** — fix(proxy): 保留 Responses 主动中止的零输出终态. Explicit incomplete interrupted/steered/content_filter/max_messages outcomes bypass empty retry in HTTP/SSE while preserving reasons and usage. Ordinary empty completed/max_output_tokens/unknown outcomes retain retry. Pipeline tests assert attempt counts and terminal data.
- [6794275a4a](https://github.com/james-6-23/codex2api/commit/6794275a4abfbfe7523596f991439eabac9ec5a9) **adapt-partial-covered** — fix(proxy): 按 Codex 协议支持 Responses WebSocket 中断. Connection-bound interruption, stale response protection, terminal idempotency and late-ack drain already exist. Add missing top-level duplicate/case-ambiguous control validation, including escaped duplicates, before routing; nested tool/user JSON remains opaque.
- [c592242fd1](https://github.com/james-6-23/codex2api/commit/c592242fd10465cccf93223cf6f8af845c01abb1) **merge-duplicate** — Merge pull request #827 from bxb1337/feat/unified-codex-attestation. Merge record reuses decisions for its already reviewed component commits; not counted as an independent patch review.
- [d94e6ccd20](https://github.com/james-6-23/codex2api/commit/d94e6ccd20339867389896d9b1cbe5464da7a797) **merge-duplicate** — Merge pull request #829 from bxb1337/fix/responses-ws-interrupt. Merge record reuses decisions for its already reviewed component commits; not counted as an independent patch review.

## gpt-load

Repository: tbphp/gpt-load (main).
Previous: `7ba66c3f7a6315a08b89bdf29f8da92c1aaede1b` at 2026-10-24T16:08:57.617858+00:00.
Frozen: `d3307afa7991e3100a1564bd4f3190b19e78890f` at 2026-10-25T13:51:28.648121+00:00.

- [39941b659d](https://github.com/tbphp/gpt-load/commit/39941b659d1b80bc43a478b783ae0758166e3c17) **skip-or-covered** — chore(web): bump the web-minor-and-patch group in /web with 5 updates (#915). Vue packages absent; Vite 8.3.5 already integrated; no blind ESLint 9-to-10 ecosystem migration.
- [d3307afa79](https://github.com/tbphp/gpt-load/commit/d3307afa7991e3100a1564bd4f3190b19e78890f) **skip-or-covered** — chore(deps): bump the go-minor-and-patch group with 4 updates (#914). Pion WebRTC and embedded CPA SDK absent. Codex 0.164 identity already covered; peer Claude/Grok/Antigravity version strings alone do not establish this fork client compatibility.

## new-api

Repository: QuantumNous/new-api (main).
Previous: `e835b1bfc43b565d625537830dbfb19328925134` at 2026-10-24T16:08:57.556495+00:00.
Frozen: `34003e9c2eed2c20e617f583fdfe54bf1bf42237` at 2026-10-25T13:51:28.666644+00:00.

- [9823ebc7ea](https://github.com/QuantumNous/new-api/commit/9823ebc7ea4ac7102d5d8478d395c7d7eaf6c285) **adapt** — fix(billing): bill cache_creation_input_tokens reported in prompt_tokens_details (#7909). Accept cache_creation_input_tokens in shared Chat/Responses prompt usage decoding. Preserve existing first-present precedence (including canonical explicit zero), never sum aliases. Added round-trip and billing quantity/cost regression.
- [bc795cc118](https://github.com/QuantumNous/new-api/commit/bc795cc1185a0ecef0ca51f73d337667026ec62f) **covered** — fix:修复stop为非字符串和[]string的协议转换问题 (#7935). llm/transformer/openai/model.go Stop.UnmarshalJSON decodes string/[]string and rejects invalid scalar/list types; no unchecked []any assertion.
- [f1569d1ea7](https://github.com/QuantumNous/new-api/commit/f1569d1ea7e87cd1a73c5f89d989b6b3bdaf68b3) **skip-architecture** — fix(pricing): classify dynamic per-request billing correctly (#8010). Peer dynamic per-request expression pricing classifier/UI has no matching AxonHub pricing expression engine.
- [4d962e464d](https://github.com/QuantumNous/new-api/commit/4d962e464de7a16ff8bf16c660307899d49108ba) **covered** — fix(relaykit): preserve tool argument numbers in Claude responses (#7996). Anthropic inbound_convert.go passes tool arguments via xjson.SafeJSONRawMessage, avoiding float64 round trips.
- [0165fef98d](https://github.com/QuantumNous/new-api/commit/0165fef98d41b02a7368e89dc5fa2df651f8587b) **covered** — fix(relaykit): preserve Gemini text alongside tool calls (#7998). Gemini outbound_convert.go sets message text and tool calls independently; outgoing conversion also appends tool parts after text.
- [34003e9c2e](https://github.com/QuantumNous/new-api/commit/34003e9c2eed2c20e617f583fdfe54bf1bf42237) **skip-peer-test** — test: stabilize channel affinity cache keys (#8009). Fix is a collision in peer-specific test cache keys; matching global/test setup is absent.
