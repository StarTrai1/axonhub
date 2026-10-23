# Frozen upstream and peer review

Cutoff: 2026-10-22 09:30 UTC (Thursday 17:30 Beijing). Frozen SHA ledger: 25 upstream commits; 40 peer records including two merge duplicates.

## gpt-load

https://github.com/tbphp/gpt-load — `5b7a5ee310e217ea0365f2bfb74cbd62fbcee7ec`; frozen 2026-10-23T18:32:29.176582+00:00.

- `5b7a5ee3` fix(gateway): size the request body limit to available memory (#903) — **skip**. Memory-sized request limits and Jev auto-model parser are a different capacity policy; retain existing bounded AxonHub request readers.
- `81aae47b` fix(cpa): retry Codex overloads before output (#905) — **covered**. llm/pipeline Responses retry integration already buffers preamble, detects capacity errors, and vetoes replay after visible content or hosted tools.
- `d9453eb7` fix(usage): price advisor iterations and enforce advisor model access (#904) — **skip**. Advisor iteration pricing and model ACL depend on gpt-load scheduler/receipt architecture; not a portable commit for AxonHub pricing.
- `2a94750b` fix(antigravity): distinguish quota window labels (#907) — **skip**. Antigravity subscription quota-window presentation; no matching subscription observation schema in this gateway.

## OmniProxy

https://github.com/mibgb65-cloud/OmniProxy — `56448d20ccd797bc32aae1c02a5e2f2030b8b6c8`; frozen 2026-10-23T18:32:29.252967+00:00.


## new-api

https://github.com/QuantumNous/new-api — `33c61a24234aa20cb10e8a644304322c476361de`; frozen 2026-10-23T18:32:29.134110+00:00.

- `33c61a24` fix: prevent stale values when switching edit drawer records (#7989) — **skip**. New API key/redemption/model drawers and expiry contract differ; no related drawer transplant.
- `30748c65` fix(logging): 隐藏访问日志中的查询凭据 (#7972) — **covered**. internal/server/middleware/access_log.go records URL.Path only, never RawQuery.
- `7fb6e57a` fix(channels): preserve advanced custom body passthrough settings (#7971) — **skip**. New API advanced JSON passthrough drawer fields do not map to AxonHub ChannelSettings/body override operations.
- `e9d4d635` feat(billing): bill OpenAI 12-hour prompt cache writes at their own price (#7965) — **skip**. 12-hour cache-write pricing is a new New API billing-expression contract; AxonHub retains total cache writes but has no matching 12-hour price slot. Not claimed as covered.
- `d0b840e7` fix(relay): keep the client's JSON Schema property order — **covered**. AxonHub tool Parameters/ParametersJsonSchema use json.RawMessage; native body passthrough retains schema key order. No relaykit map round-trip.

## codex2api

https://github.com/james-6-23/codex2api — `1bdfc85cecff2559422fa4eb39fdd880bfc2c462`; frozen 2026-10-23T18:32:29.266706+00:00.


## CLIProxyAPI

https://github.com/router-for-me/CLIProxyAPI — `8289ec4c71ad914d0612c2776352b76ba3d355a6`; frozen 2026-10-23T18:32:29.061603+00:00.

- `8289ec4c` fix(usage): preserve interactions token semantics when merging stream usage — **skip**. CPA interactions plugin TokenBreakdown contract absent; AxonHub per-protocol usage conversion remains authoritative.
- `888f2382` fix(api): add RESP protocol bounds validation and connection panic recovery — **skip**. CPA Redis RESP multiplexer/home certificate path absent; HTTP/WebSocket bounds unchanged.
- `29150a22` fix(schema): preserve Gemini $ref sibling constraints — **covered-native**. Native Gemini ParametersJsonSchema is raw, so $ref sibling constraints remain intact. Antigravity uses a separate restricted-schema sanitizer; its allOf flattening is not claimed equivalent to JSON Schema preservation.
- `3f5d3a16` fix(gemini/vertex): preserve non-content finish reasons and surface prompt blocks — **adapted-partial**. Existing Gemini candidate finish-reason conversion covers partless refusal; added explicit no-candidate promptFeedback rejection in HTTP and SSE paths. CPA pending usage/translation state is not transplanted.
- `83cb1b95` fix(gemini): populate token limits in Gemini model catalog responses — **skip**. CPA /v1beta model registry response enhancement; not the AxonHub model catalog API.
- `e5434f65` fix(signature): sign the first Gemini function call after a text part — **skip**. CPA native signature sanitizer and bypass insertion absent; preserve actual signatures and existing AxonHub role/tool conversion.
- `e1fb2521` fix(claude): preserve tool arguments for implicitly open empty object schemas — **covered**. AxonHub tool arguments are preserved; no CPA parameterless-schema cleaner replaces implicitly open object arguments with {}.
- `376015c3` fix(codex): map commentary phase messages to thinking blocks in Claude response translator — **skip**. Commentary-to-thinking changes visible content semantics and replay signatures; preserve distinct commentary and reasoning instead of reclassifying client-visible text.
- `41a4b9f4` Merge pull request #6903 from router-for-me/home — **merge-duplicate**. Merge of e45f9426; reuse that decision, no repeated patch review.
- `e45f9426` feat(home): add cluster latency telemetry and probing — **skip**. CPA home cluster telemetry/probing absent; no live paid probes introduced.
- `d946c4fb` feat(codex): ignore use_siwc and enforce non-SIWC credentials for Codex client requests — **skip**. CPA SIWC credential eligibility/conductor is absent; no use_siwc or credential-selection policy imported.
- `45501610` fix(gemini): enable server-side tool invocations when mixing built-in and function tools — **adapted**. Mixed native Gemini hosted tools and function declarations now set includeServerSideToolInvocations; explicitly removed on Antigravity.
- `13874f1f` fix(pluginhost): pass decoded request body to frontend auth plugins — **skip**. CPA frontend-auth plugin ABI absent; no decoded request plugin contract to update.
- `a014ba0e` test(pluginhost,handlers): fix flaky races in plugin load and stream cancel tests — **skip**. CPA plugin host lifecycle and its flaky tests absent.
- `b90190db` fix(usage): propagate claude usage.speed into response service tier — **skip**. usage.speed pricing requires mapping Anthropic fast/standard into AxonHub price tiers; not blindly substituted for service_tier. Existing fast request support retained.
- `ceb045a1` ci(workflow): bump Go version to 1.26.10 in release pipeline — **covered**. AxonHub already pins Go 1.27.3, newer than peer 1.26.10.
- `07b5a7a1` feat(usage): report effective outbound service tier after payload rules — **skip**. CPA plugin/Redis usage sinks and outbound payload-rule service-tier reporting differ from AxonHub request/execution accounting.
- `8bd17955` fix(management): read Handler.cfg under h.mu in OAuth login flows and token save helpers — **skip**. CPA management Handler.cfg mutex and OAuth token-save helpers absent.
- `57a12996` fix(watcher): preserve file credentials when auth directory is unreadable during full scan — **skip**. CPA filesystem credential watcher absent; AxonHub credentials are database-owned.
- `ad09c779` test(translator): skip replay target model allocation budget test under race detector — **skip**. CPA allocation microbenchmark is absent; no test skip introduced.
- `43f27e3a` fix(responses): add context_length_exceeded code to Claude prompt overflow stream errors — **adapted**. Anthropic HTTP and SSE prompt-is-too-long errors acquire context_length_exceeded only when no explicit provider code exists.
- `df860eb9` fix(translator): keep tool_use-deferred Claude web search open across tool outputs — **skip**. CPA deferred Claude web-search state machine differs; preserve AxonHub provider extension/tool result representation instead of transplanting translator state.
- `88732ad5` fix(vertex): use regional endpoint hosts for multi-region us and eu locations — **covered**. llm/vertex/executor.go already selects aiplatform.us/eu.rep.googleapis.com for multi-region locations; configured Gemini base URL prefixes are also retained.
- `4138edb1` fix(auth): skip token rejection and refresh for request-scoped 401 rules — **skip**. CPA auth-conductor request-scoped 401 override rules absent; do not weaken credential rejection globally.
- `d2e61d19` fix(claude): validate cloak_mode never headers against configured defaults — **skip**. CPA cloak_mode never and device profile header policy absent; preserve explicit AxonHub client identity settings.
- `d495e24c` fix(claude): map model_context_window_exceeded to incomplete max_output_tokens on responses — **adapted**. Anthropic model_context_window_exceeded now maps to length in streamed and nonstreamed conversion; Responses renders incomplete/max_output_tokens.

## sub2api

https://github.com/Wei-Shaw/sub2api — `5c185c5c5d4f5e409e2139806e0964ae944e03e8`; frozen 2026-10-23T18:32:29.016564+00:00.

- `5c185c5c` chore: sync VERSION to 0.2.18 [skip ci] — **skip**. Peer VERSION only.
- `ac1b8f12` chore: update sponsors — **skip**. Peer sponsors/assets only.
- `82862680` fix(deps): bump dompurify to 3.4.17 for GHSA-cg2g-xh64-v4q7 / GHSA-mrv2-3pqr-j5jr — **skip**. DOMPurify not used by this frontend; existing rehype-sanitize contract retained.
- `c5b81128` Merge pull request #8305 from apple050620312/fix/openai-overload-recovery-20261023 — **merge-duplicate**. Merge of c591412c; reuse decision without repeated patch review.
- `c591412c` fix(openai): 合并 Codex overload 身份与空流安全重试修复 — **covered-partial**. Capacity retry/preamble and Retry-After-ms already supported. Do not force codex-tui identity to CLI: preserve explicit identity settings and real client headers; peer risk-control explanation is not provider evidence.

## upstream

https://github.com/looplj/axonhub — `0d82a8e4400c039ce24b9f89a6b0745045c627f9`; frozen 2026-10-23T18:32:29.162684+00:00.

- `0d82a8e4` feat(channels): add Cloudflare Workers AI channel type (#2674) — **merged**. Integrated frozen upstream commit; conflicting fork behavior retained and CI generation required.
- `9c1ea3bf` feat(channel): configurable multi-key selection strategies (sticky/random/round-robin/priority/fixed) (#2548) — **merged**. Integrated frozen upstream commit; conflicting fork behavior retained and CI generation required.
- `f8f12109` fix(channels): make channel table responsive on mobile and narrow viewports (#2689) — **merged**. Integrated frozen upstream commit; conflicting fork behavior retained and CI generation required.
- `605b828d` feat(requests): show the cache hit rate in the tokens column (#2661) — **merged**. Integrated frozen upstream commit; conflicting fork behavior retained and CI generation required.
- `6980148a` feat(ui): categorize channels with filter pills and section headers in provider quota popover (#2688) — **merged**. Integrated frozen upstream commit; conflicting fork behavior retained and CI generation required.
- `03437bb1` feat: add server-side Claude auto-mode reviewer (#2722) — **merged**. Integrated frozen upstream commit; conflicting fork behavior retained and CI generation required.
- `a8a78c5e` chore(deps): bump @google/genai in /integration_test/gemini/node (#2739) — **merged**. Integrated frozen upstream commit; conflicting fork behavior retained and CI generation required.
- `4d4cfb3f` chore(deps): bump github.com/aws/aws-sdk-go-v2/feature/s3/manager in / (#2737) — **merged**. Integrated frozen upstream commit; conflicting fork behavior retained and CI generation required.
- `c0e3374c` chore(deps): bump vite, @vitejs/plugin-react-swc and @tailwindcss/vite (#2738) — **merged**. Integrated frozen upstream commit; conflicting fork behavior retained and CI generation required.
- `4d2c2edd` chore(deps): bump github.com/Masterminds/semver/v3 in / (#2733) — **merged**. Integrated frozen upstream commit; conflicting fork behavior retained and CI generation required.
- `42b9bb6c` chore(deps): bump github.com/aws/aws-sdk-go-v2 in / (#2736) — **merged**. Integrated frozen upstream commit; conflicting fork behavior retained and CI generation required.
- `9eaf0fb7` chore(deps): bump golang.org/x/sync in / (#2730) — **merged**. Integrated frozen upstream commit; conflicting fork behavior retained and CI generation required.
- `2acd7b9e` chore(deps): bump ariga.io/atlas in / (#2735) — **merged**. Integrated frozen upstream commit; conflicting fork behavior retained and CI generation required.
- `ebdf97ef` chore(deps): bump github.com/anthropics/anthropic-sdk-go (#2727) — **merged**. Integrated frozen upstream commit; conflicting fork behavior retained and CI generation required.
- `49cb7a31` chore(deps): bump node from 22-alpine to 25-alpine (#2726) — **merged**. Integrated frozen upstream commit; conflicting fork behavior retained and CI generation required.
- `b35ced17` chore(deps): bump google.golang.org/genai in /integration_test/gemini (#2728) — **merged**. Integrated frozen upstream commit; conflicting fork behavior retained and CI generation required.
- `ef768894` chore(deps): bump go.uber.org/zap in / (#2729) — **merged**. Integrated frozen upstream commit; conflicting fork behavior retained and CI generation required.
- `d2e6faee` chore(deps): bump github.com/openai/openai-go/v3 (#2731) — **merged**. Integrated frozen upstream commit; conflicting fork behavior retained and CI generation required.
- `78fd232b` chore(deps): bump cloud.google.com/go/storage in / (#2732) — **merged**. Integrated frozen upstream commit; conflicting fork behavior retained and CI generation required.
- `204644b4` chore(deps): bump github.com/alicebob/miniredis/v2 in / (#2734) — **merged**. Integrated frozen upstream commit; conflicting fork behavior retained and CI generation required.
- `73151f77` chore(deps): configure Dependabot for all dependency surfaces (#2725) — **merged**. Integrated frozen upstream commit; conflicting fork behavior retained and CI generation required.
- `128e5d7f` build: pin Go builder image to 1.27.3 and Alpine 3.24 — **partial-and-merged**. Go 1.27.3 already pinned; adopt exact Alpine 3.24 image.
- `57f15ffc` fix(deps): synchronize compress 1.18.7 across Go modules (#2723) — **covered-and-merged**. compress 1.18.7 already pinned; retain direct root dependency and upstream ancestry.
- `1ced7546` chore(deps-dev): bump vite from 7.3.5 to 7.3.7 in /frontend — **merged**. Integrated frozen upstream commit; conflicting fork behavior retained and CI generation required.
- `6250e7d3` ci: Scope Docker GHA cache by arch in workflows (#2712) — **merged**. Integrated frozen upstream commit; conflicting fork behavior retained and CI generation required.
