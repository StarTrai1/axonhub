# Frozen upstream and peer review — 2026-11-06

The cutoff is the previous committed ledger. Every new record received one title/parent/file-inventory screening; selected runtime patches received the recorded inspection. Partial patch inspection and inventory-only decisions are not full coverage claims.

## upstream — 15 records
`8a9df52283d9d9c3909c839267ce890a2913231e` → `f85d7173e655bf2f90ad0fd00a19ea55fe21e9e8`

- [e1feaaf8fa](https://github.com/looplj/axonhub/commit/e1feaaf8fa96a1e4a3487bb36f0e9d4ab3bce86a) chore(deps): group routine updates on a weekly schedule
  merged — Merge upstream changes; retain fork superset CI and explicit protocol precedence. Resolve dependencies through CI generation. Review: upstream merge diff and conflict inspection.
- [29267ce5f3](https://github.com/looplj/axonhub/commit/29267ce5f3e5f40a4bf4adedcdb9c8315df53fb0) chore(deps): apply pending dependabot upgrades
  merged — Merge upstream changes; retain fork superset CI and explicit protocol precedence. Resolve dependencies through CI generation. Review: upstream merge diff and conflict inspection.
- [3294d85212](https://github.com/looplj/axonhub/commit/3294d852129f29655310bd4eb8bb5bf0d91fdcb5) chore(deps-dev): bump @vitejs/plugin-react in /frontend (#2891)
  merged — Merge upstream changes; retain fork superset CI and explicit protocol precedence. Resolve dependencies through CI generation. Review: upstream merge diff and conflict inspection.
- [eddc6c8a3d](https://github.com/looplj/axonhub/commit/eddc6c8a3d02836d507321cdf6e50098cd16dd75) chore(deps): bump shiki from 3.23.0 to 4.5.0 in /frontend (#2890)
  merged — Merge upstream changes; retain fork superset CI and explicit protocol precedence. Resolve dependencies through CI generation. Review: upstream merge diff and conflict inspection.
- [f7af0bfb57](https://github.com/looplj/axonhub/commit/f7af0bfb5768f1c86e5a1f1b8d7eb0bd581dffac) chore(deps): bump react-i18next from 15.7.4 to 17.1.10 in /frontend (#2888)
  merged — Merge upstream changes; retain fork superset CI and explicit protocol precedence. Resolve dependencies through CI generation. Review: upstream merge diff and conflict inspection.
- [01d4328744](https://github.com/looplj/axonhub/commit/01d4328744c6ddba1bc35d2f1eee821457789e86) chore(deps-dev): bump knip from 5.87.0 to 6.44.1 in /frontend (#2887)
  merged — Merge upstream changes; retain fork superset CI and explicit protocol precedence. Resolve dependencies through CI generation. Review: upstream merge diff and conflict inspection.
- [bb1d00aee6](https://github.com/looplj/axonhub/commit/bb1d00aee67131205727e3192edcd7f8b12c90a7) chore(deps): bump the npm-minor-patch group across 1 directory with 41 updates (#2886)
  merged — Merge upstream changes; retain fork superset CI and explicit protocol precedence. Resolve dependencies through CI generation. Review: upstream merge diff and conflict inspection.
- [d1395b266b](https://github.com/looplj/axonhub/commit/d1395b266b4fad711026c41d5a760da153f98cf8) chore(deps): bump the shared-go-modules group across 3 directories with 18 updates (#2889)
  merged — Merge upstream changes; retain fork superset CI and explicit protocol precedence. Resolve dependencies through CI generation. Review: upstream merge diff and conflict inspection.
- [a5e49d0171](https://github.com/looplj/axonhub/commit/a5e49d01716496cd5e3b32037753ce397502f6e6) fix(openai): map reasoning_text from Copilot chat completions to reasoning (#2894)
  merged — Merge upstream changes; retain fork superset CI and explicit protocol precedence. Resolve dependencies through CI generation. Review: upstream merge diff and conflict inspection.
- [a95d2247bf](https://github.com/looplj/axonhub/commit/a95d2247bf1b4ecacf8e47db3113f7c706a09dd1) fix(anthropic): map content_filter finish reason to refusal stop reason (#2892)
  merged — Merge upstream changes; retain fork superset CI and explicit protocol precedence. Resolve dependencies through CI generation. Review: upstream merge diff and conflict inspection.
- [4d5cf665cd](https://github.com/looplj/axonhub/commit/4d5cf665cd3c0237bb184e83efd7ddce25b24c0a) fix(anthropic): map refusal and pause_turn stop reasons in streaming outbound (#2884)
  merged — Merge upstream changes; retain fork superset CI and explicit protocol precedence. Resolve dependencies through CI generation. Review: upstream merge diff and conflict inspection.
- [831c95d7e8](https://github.com/looplj/axonhub/commit/831c95d7e89daa2ff20e7c990eba3dbe34b54475) ci: run frontend/backend checks only when relevant files change (#2897)
  merged — Merge upstream changes; retain fork superset CI and explicit protocol precedence. Resolve dependencies through CI generation. Review: upstream merge diff and conflict inspection.
- [69f39b8df2](https://github.com/looplj/axonhub/commit/69f39b8df29e7dbf95655392d1a734dac05a5aec) fix(responses): prefer transport family when selecting endpoints (#2881)
  merged — Merge upstream changes; retain fork superset CI and explicit protocol precedence. Resolve dependencies through CI generation. Review: upstream merge diff and conflict inspection.
- [bf21e8cf13](https://github.com/looplj/axonhub/commit/bf21e8cf13037500009b99de0bc7ab0b2504b593) fix(channels): update Anthropic default models away from retiring Claude 4.5 (#2902)
  merged — Merge upstream changes; retain fork superset CI and explicit protocol precedence. Resolve dependencies through CI generation. Review: upstream merge diff and conflict inspection.
- [f85d7173e6](https://github.com/looplj/axonhub/commit/f85d7173e655bf2f90ad0fd00a19ea55fe21e9e8) docs: introduce plugin system solution (#2896)
  merged — Merge upstream changes; retain fork superset CI and explicit protocol precedence. Resolve dependencies through CI generation. Review: upstream merge diff and conflict inspection.

## sub2api — 5 records
`876b96d204855ad1aac486b7dd39a95d377d4760` → `25608d216bb7be04e4a646830cb065cd09b7d7e9`

- [e97a48d84b](https://github.com/Wei-Shaw/sub2api/commit/e97a48d84be5f4b23898ab2d2f968482dbd0b3ce) fix: GPT-5.5已不提供服务，默认导出模型改为GPT5.6-SOL
  different-export-default — Peer export model default is administrative configuration, not an Axon native model compatibility fix. Review: title and file inventory.
- [457602c8fc](https://github.com/Wei-Shaw/sub2api/commit/457602c8fc07112fb6e38642adea88a61d7c9759) fix(backend): bound gateway resources and background work
  partial-coverage-defer-broad-resource-refactor — Axon already bounds SSE events, pre-output buffers and owned history. The 104-file peer resource-budget/background-worker redesign and per-write deadlines are not equivalent to these limits and were not wholesale ported without a matching lifecycle fixture. Review: file inventory and partial selected timeout/body-limit patches.
- [503617a82f](https://github.com/Wei-Shaw/sub2api/commit/503617a82fa5b58e4bb1954fb98d4494233d9193) Merge pull request #8484 from StarryKira/codex/backend-resource-safety
  merge-record — Merge record for peer changes, screened without re-reviewing its already listed component commits. Review: parents and file inventory.
- [8e92c892b4](https://github.com/Wei-Shaw/sub2api/commit/8e92c892b47f3f934f18ce63da4c4dcb75f0b6bd) Merge pull request #8495 from FeeiCN/patch-1
  merge-record — Merge record for peer changes, screened without re-reviewing its already listed component commits. Review: parents and file inventory.
- [25608d216b](https://github.com/Wei-Shaw/sub2api/commit/25608d216bb7be04e4a646830cb065cd09b7d7e9) chore: update sponsors
  peer-maintenance — Sponsors/repository maintenance; no Axon runtime patch. Review: title and file inventory.

## CLIProxyAPI — 19 records
`78a9cc9aaa8ae90a66300d9cbddfe7c894307622` → `585f97deac9aa57c700301221fb1a7085e1827cc`

- [c3938b72bd](https://github.com/router-for-me/CLIProxyAPI/commit/c3938b72bdaaa803ab983eeeb0005ce59cef505c) fix(logging,clienterror): classify mid-stream cyber blocks as 400 and persist error logs
  retain-native-refusal-and-error-provenance — Do not turn every native stop/refusal into a synthetic HTTP fault or copy peer response-writer buffering. Preserve upstream errors and Axon pre-output retry admission; memory failover follows configured rules. Review: title and changed-file inventory.
- [0a94b2aefb](https://github.com/router-for-me/CLIProxyAPI/commit/0a94b2aefb520469869ef8ea7b66b74a3da68e1a) fix(logging,executor): preserve Claude Fast classified error status and cap deferred body buffering
  retain-native-refusal-and-error-provenance — Do not turn every native stop/refusal into a synthetic HTTP fault or copy peer response-writer buffering. Preserve upstream errors and Axon pre-output retry admission; memory failover follows configured rules. Review: title and changed-file inventory.
- [69a032dea3](https://github.com/router-for-me/CLIProxyAPI/commit/69a032dea3688fbab0d67686d1631ac9d2ea95f3) perf,fix(logging,websocket): avoid 200 OK body buffering and synchronize ws disconnect error logging
  retain-native-refusal-and-error-provenance — Do not turn every native stop/refusal into a synthetic HTTP fault or copy peer response-writer buffering. Preserve upstream errors and Axon pre-output retry admission; memory failover follows configured rules. Review: title and changed-file inventory.
- [03a8743c3c](https://github.com/router-for-me/CLIProxyAPI/commit/03a8743c3cf9d1f803a4503d038b745b2dc2b739) fix(executor): classify non-stream Claude policy refusals as 400 request faults
  retain-native-refusal-and-error-provenance — Do not turn every native stop/refusal into a synthetic HTTP fault or copy peer response-writer buffering. Preserve upstream errors and Axon pre-output retry admission; memory failover follows configured rules. Review: title and changed-file inventory.
- [508ac2bce0](https://github.com/router-for-me/CLIProxyAPI/commit/508ac2bce0966af94a30e6d44c43283390932b45) fix(executor,handlers): require stream-level evidence for Gemini filter blocks and pre-log terminal chunk errors
  retain-native-refusal-and-error-provenance — Do not turn every native stop/refusal into a synthetic HTTP fault or copy peer response-writer buffering. Preserve upstream errors and Axon pre-output retry admission; memory failover follows configured rules. Review: title and changed-file inventory.
- [ec14dc4703](https://github.com/router-for-me/CLIProxyAPI/commit/ec14dc4703f0a5b7185c0c625032af30b94f0a0e) feat(kvstore): add local bbolt key-value store and integration routing
  different-storage-architecture — Do not add a bbolt store to replace Axon Ent/xcache and durable response ownership. Review: title and file inventory.
- [d57270f465](https://github.com/router-for-me/CLIProxyAPI/commit/d57270f465cc69ba73b9c34c2c783aa4a9196e2f) fix(executor): support Codex remote compaction v2 on openai-compatible routes
  retain-native-compaction — Peer generates local summary capsules on compatible routes. Retain Axon native compaction provenance and reconstruction invariants; no plaintext capsule substitute. Review: selected openai_compat_executor.go patch.
- [e6e5708e67](https://github.com/router-for-me/CLIProxyAPI/commit/e6e5708e677adb2463d45accdd24bc9fd028ddd8) feat(claude): persist cloaked session date pins in persistent KV store
  retain-caller-instructions — Do not introduce cloaked date pins or rewrite caller session identity. Review: title and file inventory.
- [f208c90824](https://github.com/router-for-me/CLIProxyAPI/commit/f208c90824f265f3bc61c74544aac29918a2f7a3) fix(handlers): record TTFT for direct plugin executor routes
  different-plugin-executor — CPA direct plugin timing paths do not exist in Axon; no new plugin execution surface added. Review: title and file inventory.
- [7a7d37ecb5](https://github.com/router-for-me/CLIProxyAPI/commit/7a7d37ecb5c61d69413d19d7bce085a82740f5ca) feat(codex): add support for gpt-image-2.6 models
  covered-native-image-endpoint — Axon already recognizes explicit 2.6 image model variants and sends native Images requests through the Codex Images endpoint without the peer SIWC route. Review: selected image executor/routing patches.
- [010de194c2](https://github.com/router-for-me/CLIProxyAPI/commit/010de194c294ad3b562b4c8de71f59740f5bc8c8) Merge pull request #7262 from Nciae-Zyh/feat/codex-gpt-image-26
  merge-record — Merge record for peer changes, screened without re-reviewing its already listed component commits. Review: parents and file inventory.
- [8c9a78518e](https://github.com/router-for-me/CLIProxyAPI/commit/8c9a78518e39f0ca527e188f9efea0193f436a31) fix(openai): bridge client tool_search in Chat Completions Responses translator
  defer-client-tool-search-emulation — Axon preserves native Responses tool_search and rejects unsupported conversion. Peer emulates client tool_search as functions with synthesized IDs; do not claim native passthrough equals that cross-protocol feature. Review: partial selected client-tool-search bridge patch.
- [6044c5d142](https://github.com/router-for-me/CLIProxyAPI/commit/6044c5d14270fa094333b25932ac11ab13282821) fix(openai): translate thinking content-part arrays in Claude response translator
  defer-content-array-extension — The peer recognizes extra reasoning/thinking arrays while converting Chat to Claude. Upstream reasoning_text is merged; this broader nonstandard content-array contract needs dedicated conversion fixtures. Review: partial selected translator patch.
- [be69162478](https://github.com/router-for-me/CLIProxyAPI/commit/be69162478da2a082844e8223962fe035ea92138) fix(executor,logging): preserve Antigravity usage evidence, sparse Claude refusal identity, and Home stream error logs
  retain-native-refusal-and-error-provenance — Do not turn every native stop/refusal into a synthetic HTTP fault or copy peer response-writer buffering. Preserve upstream errors and Axon pre-output retry admission; memory failover follows configured rules. Review: title and changed-file inventory.
- [6897eebd50](https://github.com/router-for-me/CLIProxyAPI/commit/6897eebd50617aa86cf04f1f8d9f38299b1a78cc) Merge pull request #7242 from sususu98/fix/cyber-block-request-fault-and-error-log
  merge-record — Merge record for peer changes, screened without re-reviewing its already listed component commits. Review: parents and file inventory.
- [dfe8b182c5](https://github.com/router-for-me/CLIProxyAPI/commit/dfe8b182c54de4f9c5ccc3d8581ca23b5e1b249b) fix(codex,auth): bypass SIWC for OpenAI image requests and route directly to Codex images endpoint
  covered-native-image-endpoint — Axon already recognizes explicit 2.6 image model variants and sends native Images requests through the Codex Images endpoint without the peer SIWC route. Review: selected image executor/routing patches.
- [b5e6027ce4](https://github.com/router-for-me/CLIProxyAPI/commit/b5e6027ce4d72c88a92d5571d23afc22bbd7bd30) fix(openai): expose terminal 429 error frames on responses websocket
  covered-error-envelope-defer-peer-replay-policy — Axon emits the actual HTTP status in Responses WebSocket errors. Peer replay-selection and trusted retry-deadline additions use a different auth manager; not transplanted. Review: partial selected WebSocket forward/session patches.
- [62c58a7480](https://github.com/router-for-me/CLIProxyAPI/commit/62c58a7480f3544d8cf08e7f3aadb1a1192a8949) Merge pull request #7265 from sususu98/fix/codex-openai-image-bypass-siwc
  merge-record — Merge record for peer changes, screened without re-reviewing its already listed component commits. Review: parents and file inventory.
- [585f97deac](https://github.com/router-for-me/CLIProxyAPI/commit/585f97deac9aa57c700301221fb1a7085e1827cc) fix(auth): pass context and headers to availableAuthsForSelector in HasAlternativeTargetForModel
  covered-scoped-selection — Axon selectors already receive context and final dispatch checks project/key/model/workload restrictions; no peer auth-manager signature to change. Review: selected complete one-line call-site patch.

## OmniProxy — 0 records
`4bcee95bf1831e2f70de29281ef23a207de6e146` → `4bcee95bf1831e2f70de29281ef23a207de6e146`


## codex2api — 4 records
`a4bbb455ba7ae9f4e6e02e6de2cae24fe12e6212` → `9676bfe8983f5a193bda1e97fd502cd27362017e`

- [f22cbf4e4d](https://github.com/james-6-23/codex2api/commit/f22cbf4e4ddcec1f0859211e6621fd8efba76f09) fix(images): omit unsupported reasoning effort in image requests
  covered-native-image-path — Peer removes an injected medium effort in its Responses image-tool builder. Axon native Images requests do not inject this reasoning setting. Review: selected production patch.
- [21b28aa42f](https://github.com/james-6-23/codex2api/commit/21b28aa42fd6328da586f172a7d8b06b83e29fd2) docs(images): document request builder and ingress regression
  covered-native-image-path — Peer removes an injected medium effort in its Responses image-tool builder. Axon native Images requests do not inject this reasoning setting. Review: selected production patch.
- [595687ebd2](https://github.com/james-6-23/codex2api/commit/595687ebd2b6105d6b6c0621b03c4de0b161df19) Merge pull request #854 from cuiks/codex/fix-image-reasoning-effort
  merge-record — Merge record for peer changes, screened without re-reviewing its already listed component commits. Review: parents and file inventory.
- [9676bfe898](https://github.com/james-6-23/codex2api/commit/9676bfe8983f5a193bda1e97fd502cd27362017e) docs(changelog): v3.1.4
  peer-maintenance — Peer tests, documentation, release or repository maintenance; no Axon runtime change to port. Review: title and file inventory.

## gpt-load — 1 records
`5f345352d1191bded33cc3a994f19a66b9a6c8fa` → `f5ed5eb80f92642d7f9e9ba99567b820090e3eea`

- [f5ed5eb80f](https://github.com/tbphp/gpt-load/commit/f5ed5eb80f92642d7f9e9ba99567b820090e3eea) feat(codex): support GPT Image 2.6 models (#947)
  different-sdk-dependency — The peer updates its embedded CPA SDK and clones reasoning strings in that executor. Axon does not embed CPA; explicit image 2.6 models already have native endpoint coverage. Review: selected dependency/runtime patch.

## new-api — 4 records
`706144769e5bed5a47b78721e3bb11da3cf4c0b9` → `2de51a19e35a602a0041d380b29be7c9bcd1e0e5`

- [3374f251e3](https://github.com/QuantumNous/new-api/commit/3374f251e3a84eebfd7f37d4746d7bb017e4f4b1) feat: forward image complexity and markup for gpt-image-2.6 (#8208)
  adapted — Complexity and GIF were already represented. Add the missing separate markup overlay to native JSON/multipart image edits; reject mask/prompt_rewriter conflicts and unsupported conversions. Review: selected production patch.
- [e3e74205fe](https://github.com/QuantumNous/new-api/commit/e3e74205fe57f1226716de6f84cb4f0cf80cee89) fix(pricing): guide users through model pricing issues
  different-pricing-editor — Peer ratio pricing editor and preview workflow differs from Axon model price administration; retain the current UI and billing contract. Review: title and file inventory.
- [72f3dc7daa](https://github.com/QuantumNous/new-api/commit/72f3dc7daaeba86280b01e2d4d5d745df5c7c67b) fix(pricing): review model price changes before saving
  different-pricing-editor — Peer ratio pricing editor and preview workflow differs from Axon model price administration; retain the current UI and billing contract. Review: title and file inventory.
- [2de51a19e3](https://github.com/QuantumNous/new-api/commit/2de51a19e35a602a0041d380b29be7c9bcd1e0e5) fix(cache): 修复用户和令牌缓存的 Redis 跨槽错误 (#8218)
  different-cache-contract — The peer changes multi-key Redis user/token fencing to share a hash slot. Axon xcache has no corresponding user/token Lua fence operation to transplant. Review: selected patch and local cache search.

## magpie — 102 records
`83137bcbe971fced599a9e81d42211e1f552b2e0` → `125fd93ecdd1c7d49268a7ae11e0dced669b40be`

- [c3ba2cc933](https://github.com/yetone/magpie/commit/c3ba2cc933182d8af4da57e2fac76f6de587d792) gateway: TestUnroutedProgressingUploadOutlastsMost gives the handler's time between reads room (#5274)
  peer-maintenance — Peer tests, documentation, release or repository maintenance; no Axon runtime change to port. Review: title and file inventory.
- [9a51d644d6](https://github.com/yetone/magpie/commit/9a51d644d65f26e7e73f72344fd4b3ac63ede856) gateway: a compaction's summary late past 4 minutes is magpie's own, sent before Codex gives up (#5275)
  retain-native-compaction — Peer local summary fallback changes history provenance; do not replace native encrypted compaction with generated plaintext summaries. Review: title and file inventory.
- [e0ee9265ef](https://github.com/yetone/magpie/commit/e0ee9265efa6c07e6fed45c39aefe4bef6ca4de6) gui: TestClosingWindowMarksPageOutOfSight reads hidden as app.js does and waits for the page (#4924)
  peer-maintenance — Peer tests, documentation, release or repository maintenance; no Axon runtime change to port. Review: title and file inventory.
- [d892dd6a22](https://github.com/yetone/magpie/commit/d892dd6a22cb12138781571173e499d8edd15f92) gateway: a Claude conversation keeps the folder it named first when its caller moves
  retain-owned-request-metadata — Peer infers conversation/folder identity from local Claude/dsh transcripts or quoted working-directory text. Preserve explicit structured request ownership and metadata. Review: title and file inventory.
- [8b902e2879](https://github.com/yetone/magpie/commit/8b902e287981a26961dc9e92e06dc09512abf83e) checkin: a ZCode or Qoder captcha stands for the day, not asked again every 30 minutes (plugins#503)
  different-plugin-contract — Peer plugin manifests, agent package settings, movers/check-in and shared library synchronization do not map to an Axon server plugin contract. Review: title and file inventory.
- [099d2b4612](https://github.com/yetone/magpie/commit/099d2b4612d96beede45b8ec0186ab2eb77b97a0) agents: Codex back on ChatGPT after the reset says to restart its app, with a Restart
  different-desktop-lifecycle — Local agent sign-in, restarts, app takeover/disconnect or installer architecture are desktop lifecycle features; Axon remains the gateway server. Review: title and file inventory.
- [f24829636a](https://github.com/yetone/magpie/commit/f24829636af07d270ae902d132dbe5f76563629f) gateway: a plugin's Gemini model is never asked ending with the model's turn (plugins#734)
  peer-maintenance — Peer tests, documentation, release or repository maintenance; no Axon runtime change to port. Review: title and file inventory.
- [5ee9a18747](https://github.com/yetone/magpie/commit/5ee9a187474767e43e34f7361a3982157694ff9b) gateway: magpie's own summary ask keeps its status, so a compaction over the byte cap is summed up locally (#4862)
  retain-native-compaction — Peer local summary fallback changes history provenance; do not replace native encrypted compaction with generated plaintext summaries. Review: title and file inventory.
- [e710160df1](https://github.com/yetone/magpie/commit/e710160df1399dc193c304c8fb02418f46c6fdaa) gateway: a request body a little over a doubling is not copied whole again for its last bytes (#5281)
  different-body-reader — Peer optimizes its custom growth buffer for very large declared bodies. Axon uses bounded body/history paths; no identical readSized implementation exists to patch. Review: complete request_bounds.go patch.
- [24a68018e8](https://github.com/yetone/magpie/commit/24a68018e822f552458c6915ac52bff14c449f87) gateway: a held body's member is not checked again byte by byte when it is decoded alone (#5281)
  defer-independent-runtime-extension — Peer native search/archive/test-generation, account aggregation, JSON-in-SSE or parser internals are independent contracts; retain the current Axon request/usage and parser paths without speculative conversion or extra inference. Review: title and file inventory.
- [1f53537996](https://github.com/yetone/magpie/commit/1f53537996551c497543eadb98b789b8921b3ffd) gateway: the 516 MB generated-images test has largeBodyRoom under -race (#5281)
  peer-maintenance — Peer tests, documentation, release or repository maintenance; no Axon runtime change to port. Review: title and file inventory.
- [b047cfe73c](https://github.com/yetone/magpie/commit/b047cfe73c563f96b32b41a94ba5f04477238930) gateway: TestCRTidyTwentyFourMBLine bounds the bytes searched and allocated, not the wall clock (#5282)
  different-cr-sanitizer — Adds work counters to a peer tool-argument CR normalizer; Axon does not strip tool argument escapes heuristically. Review: complete tool_args_cr.go patch.
- [ed7aeb052f](https://github.com/yetone/magpie/commit/ed7aeb052f1747a4567011781cd85ebc4b02ce80) plugins: a package that declares magpie.agent is an agent, with a main or without (plugins#737)
  different-plugin-contract — Peer plugin manifests, agent package settings, movers/check-in and shared library synchronization do not map to an Axon server plugin contract. Review: title and file inventory.
- [49e382735f](https://github.com/yetone/magpie/commit/49e382735f09a82c8c953933a55b5da353bef525) claude: a side question after a run's first reply of words branches by the reply's id
  retain-owned-request-metadata — Peer infers conversation/folder identity from local Claude/dsh transcripts or quoted working-directory text. Preserve explicit structured request ownership and metadata. Review: title and file inventory.
- [fe6533ad8b](https://github.com/yetone/magpie/commit/fe6533ad8bf797ce9dfc6a9297a997fa4661d7fc) claudetui: Claude Code's exit is recorded once its last words are read (#5283)
  different-administration-ui — Peer display/settings/data model differs from Axon. Retain current UI components and channel configuration workflow. Review: title and file inventory.
- [76f00399c5](https://github.com/yetone/magpie/commit/76f00399c524d62004bfbb44a94012254334f769) redact: a ping between two pieces of a placeholder no longer leaves it in the agent's arguments (#5287)
  different-placeholder-stream — Peer redaction placeholder replacement and ping splitting are its own stream middleware; no matching Axon placeholder-redaction state machine. Review: title and file inventory.
- [a18ff505ba](https://github.com/yetone/magpie/commit/a18ff505ba60bd5520e055a02e493af1c4d752ef) gui tests: redact-words-list measures the drawn controls, not the closed Import box's (acmilan_ follow-up)
  peer-maintenance — Peer tests, documentation, release or repository maintenance; no Axon runtime change to port. Review: title and file inventory.
- [4164edce60](https://github.com/yetone/magpie/commit/4164edce6035835a78939fbb6fa04a039eb2fe16) gui: a one-tone dark or light logo is drawn in the text colour, so Open Design's shows in dark mode (chen2652)
  different-administration-ui — Peer display/settings/data model differs from Axon. Retain current UI components and channel configuration workflow. Review: title and file inventory.
- [dabb5722f8](https://github.com/yetone/magpie/commit/dabb5722f88e5362f511957fe6d169b0d887c8f9) claude: Claude Code routed through magpie stays signed in where the user put it when that account is spent
  different-desktop-lifecycle — Local agent sign-in, restarts, app takeover/disconnect or installer architecture are desktop lifecycle features; Axon remains the gateway server. Review: title and file inventory.
- [3d0d790606](https://github.com/yetone/magpie/commit/3d0d790606eaeffa675065240b5b6659a05e8884) codex: the Codex app left on an account that runs out is restarted onto the one Codex is signed in to (nb5p on Discord)
  different-desktop-lifecycle — Local agent sign-in, restarts, app takeover/disconnect or installer architecture are desktop lifecycle features; Axon remains the gateway server. Review: title and file inventory.
- [8eb2193540](https://github.com/yetone/magpie/commit/8eb2193540e206b6b2076a4ec5a8e37308829c0b) agent: Codex's own models are drawn by their names in its picker, as every other list's are (nb5p on Discord)
  different-desktop-catalog — Peer local agent picker/catalog refresh behavior differs from Axon channel model discovery; preserve exact IDs and explicit model selections. Review: title and file inventory.
- [681a472cab](https://github.com/yetone/magpie/commit/681a472cab47339ab8d01745a91d2bceb8a7cdc8) agent: a Codex thread started during the takeover comes back to OpenAI once it is quiet, not hours later (nb5p on Discord)
  different-desktop-lifecycle — Local agent sign-in, restarts, app takeover/disconnect or installer architecture are desktop lifecycle features; Axon remains the gateway server. Review: title and file inventory.
- [6abacd46c3](https://github.com/yetone/magpie/commit/6abacd46c3433f811b32d72c3bfe7a49e78b3d87) update: an Intel magpie under Rosetta updates to the Apple silicon build
  different-desktop-lifecycle — Local agent sign-in, restarts, app takeover/disconnect or installer architecture are desktop lifecycle features; Axon remains the gateway server. Review: title and file inventory.
- [1ae48d7f4d](https://github.com/yetone/magpie/commit/1ae48d7f4d353060a6aca827ebb16c1e3255feca) gui: Antigravity's accounts say the app keeps its own sign-in
  different-administration-ui — Peer display/settings/data model differs from Axon. Retain current UI components and channel configuration workflow. Review: title and file inventory.
- [c2c991a5b3](https://github.com/yetone/magpie/commit/c2c991a5b34f213bc58b023b78c306fbfb5c1199) remote magpie: two magpies each the other's remote no longer list each other's models back without end
  different-remote-gateway-protocol — Peer self/remote Magpie loop prevention and seat routing require its remote gateway protocol, absent in Axon. Review: title and file inventory.
- [1fd02d33ce](https://github.com/yetone/magpie/commit/1fd02d33ce1b681d3395a8672105f5262ba892c7) agentplug: an agent plugin can pick the model its agent starts on in a second file (str_, PiG)
  different-plugin-contract — Peer plugin manifests, agent package settings, movers/check-in and shared library synchronization do not map to an Axon server plugin contract. Review: title and file inventory.
- [0e55a75936](https://github.com/yetone/magpie/commit/0e55a75936f1b83f0c6c131b0a5904dff9927197) remote magpie: a request that goes round magpies pointing at each other stops after 8, and a remote that is this magpie lists its models once
  different-remote-gateway-protocol — Peer self/remote Magpie loop prevention and seat routing require its remote gateway protocol, absent in Axon. Review: title and file inventory.
- [234903cb23](https://github.com/yetone/magpie/commit/234903cb231aa8290b13637ae9a2c0742715946f) cli: magpie model api sets the API a model is asked on, as the editor's picker does (yew_fence_931206)
  covered-explicit-model-protocol — Axon already supports explicit per-model protocol settings, retained when merging upstream endpoint-family preference. Review: title and file inventory.
- [bf60ea5968](https://github.com/yetone/magpie/commit/bf60ea59686273c6618bfa1c37916f063ca3426c) routing: an earlier account's overload reads overloaded, not quota used up (danzi002)
  different-trace-label-model — Peer distinguishes an earlier told overload error from the last quota error on its Routing page. Retain Axon separate per-execution error records, no peer Try/Earlier trace model. Review: selected gateway patch and partial trace patch.
- [47d5839397](https://github.com/yetone/magpie/commit/47d5839397b1141dbd0394d2080aa6e76753a439) library: sync and backup files carry the hooks
  different-plugin-contract — Peer plugin manifests, agent package settings, movers/check-in and shared library synchronization do not map to an Axon server plugin contract. Review: title and file inventory.
- [2260390a01](https://github.com/yetone/magpie/commit/2260390a01382ca4b2a7b2e03a6ddf62aad24f65) gateway: a Gemini thought signature goes back only to the provider that gave it
  retain-native-identifiers — Do not drop native reasoning/signature/item identity based on origin or length guesses without a matching owned-history fixture. Review: title and file inventory.
- [10f1e97883](https://github.com/yetone/magpie/commit/10f1e9788379d66bbca16c61a3096ac4d4d2fb5d) search: a GLM Coding Plan searches its own models with the plan's web search MCP
  defer-independent-runtime-extension — Peer native search/archive/test-generation, account aggregation, JSON-in-SSE or parser internals are independent contracts; retain the current Axon request/usage and parser paths without speculative conversion or extra inference. Review: title and file inventory.
- [116f23c784](https://github.com/yetone/magpie/commit/116f23c784e9dde3c5fa58102acb6f40c0a3bec8) plugins: an agent plugin whose agent isn't on this computer says so, not "Adds Kimi to Agents" (zzZ)
  different-plugin-contract — Peer plugin manifests, agent package settings, movers/check-in and shared library synchronization do not map to an Axon server plugin contract. Review: title and file inventory.
- [ea3d1c62e7](https://github.com/yetone/magpie/commit/ea3d1c62e7eebc8e080a07b65f97facc3cebcb53) gateway, codexcat: Codex's image detail "original" goes as "high" to a relay's Responses API, and only DeepSeek's own API is told it takes it (qiu_eth)
  retain-explicit-model-and-image-detail — Keep exact model variants and caller image detail instead of forcing high or automatically substituting the latest image model. Review: title and file inventory.
- [3587bf70e2](https://github.com/yetone/magpie/commit/3587bf70e2206840216c137b5b699fd9ae00874b) providers: a plugin account's Select all available ticks its own list; zcode min 0.1.51 (young02036)
  different-plugin-contract — Peer plugin manifests, agent package settings, movers/check-in and shared library synchronization do not map to an Axon server plugin contract. Review: title and file inventory.
- [06808ad308](https://github.com/yetone/magpie/commit/06808ad3084e1880e75120a3f8fc2062fe29fcf6) agent: an agent's npm package that needs its install script gets it run on npm 12, bun and pnpm
  different-desktop-lifecycle — Local agent sign-in, restarts, app takeover/disconnect or installer architecture are desktop lifecycle features; Axon remains the gateway server. Review: title and file inventory.
- [4e85cac01d](https://github.com/yetone/magpie/commit/4e85cac01dfb2fbec48c4cbb9fa8f4d8621e679f) agent: OpenCode installs as OpenCode 2 (@opencode/cli, opencode.ai/v2/install) (math_milian)
  different-desktop-lifecycle — Local agent sign-in, restarts, app takeover/disconnect or installer architecture are desktop lifecycle features; Axon remains the gateway server. Review: title and file inventory.
- [c2b8814e75](https://github.com/yetone/magpie/commit/c2b8814e751cc12e52545febcd4417ed13494a7d) gui: the tray panel keeps the latest height it asked for, its fits sent one at a time (robot_yz on X)
  different-administration-ui — Peer display/settings/data model differs from Axon. Retain current UI components and channel configuration workflow. Review: title and file inventory.
- [5b9bfe407c](https://github.com/yetone/magpie/commit/5b9bfe407c0fa49356df8fb959681c0e5af8bed7) gui: the tray panel's Allowances tab folds what has nothing left behind "{n} at 0" (runking on Discord)
  different-administration-ui — Peer display/settings/data model differs from Axon. Retain current UI components and channel configuration workflow. Review: title and file inventory.
- [583fe70751](https://github.com/yetone/magpie/commit/583fe707516ceeb4093c9b2dd29fc695892ab3d4) provider: WorkBuddy's mover min is 0.2.21, whose sign-in page shows 企业微信 (shenyebuwu)
  different-administration-ui — Peer display/settings/data model differs from Axon. Retain current UI components and channel configuration workflow. Review: title and file inventory.
- [1826c4bd99](https://github.com/yetone/magpie/commit/1826c4bd99ea7ec192c76d64c8965035a10dc493) providers: a Remote magpie that is this magpie says so and is left out, unless kept (hanslin1666)
  different-remote-gateway-protocol — Peer self/remote Magpie loop prevention and seat routing require its remote gateway protocol, absent in Axon. Review: title and file inventory.
- [73c98ebc20](https://github.com/yetone/magpie/commit/73c98ebc209369bff5f8f55d934814185c990511) gateway: a search API's or plan service's ask is routed and archived (axelburks)
  defer-independent-runtime-extension — Peer native search/archive/test-generation, account aggregation, JSON-in-SSE or parser internals are independent contracts; retain the current Axon request/usage and parser paths without speculative conversion or extra inference. Review: title and file inventory.
- [9d143eacab](https://github.com/yetone/magpie/commit/9d143eacab760ab70f5e6194a7da21400b79b6ea) uninstall: install.sh --uninstall disconnects every agent first, a deleted magpie too
  different-desktop-lifecycle — Local agent sign-in, restarts, app takeover/disconnect or installer architecture are desktop lifecycle features; Axon remains the gateway server. Review: title and file inventory.
- [87c51cd6bb](https://github.com/yetone/magpie/commit/87c51cd6bbdc994f3f4748b98d754c42d2baf831) gui: a tap that comes let go before it is pressed clicks its control once (refeccd)
  different-administration-ui — Peer display/settings/data model differs from Axon. Retain current UI components and channel configuration workflow. Review: title and file inventory.
- [4cb7159f18](https://github.com/yetone/magpie/commit/4cb7159f18a80e28166e5761acacfb3ba3a4cac4) routing: a ChatGPT account whose usage can't be read is asked, not starved
  defer-passive-usage-learning — Peer learns unknown account windows from response headers and changes account ordering. Current quota reserve deliberately consumes existing provider quota collection and fails open on unknown windows; passive header collection is not claimed covered. Review: partial selected routing/provider patches.
- [3518445947](https://github.com/yetone/magpie/commit/3518445947531badf51e4ceba64d1f6b43d5f452) gui: the self-remote notes are shorter, the core back under its budget
  different-administration-ui — Peer display/settings/data model differs from Axon. Retain current UI components and channel configuration workflow. Review: title and file inventory.
- [2947685999](https://github.com/yetone/magpie/commit/2947685999303b33210103410e3ef943455ffc8d) claude: an interactive run's calls keep the conversation they answered when their line comes late
  retain-owned-request-metadata — Peer infers conversation/folder identity from local Claude/dsh transcripts or quoted working-directory text. Preserve explicit structured request ownership and metadata. Review: title and file inventory.
- [373cab2b08](https://github.com/yetone/magpie/commit/373cab2b088da999a0a50aa3d3333487a69d69c5) provider: the Cursor mover's min is 0.2.41 (plugins#706)
  different-plugin-contract — Peer plugin manifests, agent package settings, movers/check-in and shared library synchronization do not map to an Axon server plugin contract. Review: title and file inventory.
- [e2aaf21f73](https://github.com/yetone/magpie/commit/e2aaf21f73fa22fa42bd6a5b02766d9b91a6eda5) gui: a custom provider's Balance URL is asked above its balance token
  different-administration-ui — Peer display/settings/data model differs from Axon. Retain current UI components and channel configuration workflow. Review: title and file inventory.
- [2488532257](https://github.com/yetone/magpie/commit/2488532257daeb79605567d44898925bf47942f2) provider: a relay added by hand finds its balance where relays tell it
  defer-relay-balance-discovery — Peer auto-detects a relay balance endpoint and refreshes that discovery daily. Axon uses explicit provider quota adapters; do not probe arbitrary balance endpoints. Review: title and file inventory.
- [464cc30f91](https://github.com/yetone/magpie/commit/464cc30f9152dcb47be2bcce00da76108d23bf66) provider: a relay's found balance query is looked for again each day
  defer-relay-balance-discovery — Peer auto-detects a relay balance endpoint and refreshes that discovery daily. Axon uses explicit provider quota adapters; do not probe arbitrary balance endpoints. Review: title and file inventory.
- [b95a78d43c](https://github.com/yetone/magpie/commit/b95a78d43c5625cf3fd341c59d75bc298b5ec3c3) gui: the editor says where magpie found a relay's balance, with Use
  different-administration-ui — Peer display/settings/data model differs from Axon. Retain current UI components and channel configuration workflow. Review: title and file inventory.
- [5fe4c48df9](https://github.com/yetone/magpie/commit/5fe4c48df92b317e3ed3055ce7bd92a2b1da9287) gateway: a route names the gateway key it came with, by id and name
  different-administration-ui — Key-grouped request tables, route rules, session title polling, backup notices and provider-icon choice use peer-specific UI/data. Keep Axon layout and request-key attribution. Review: title and file inventory.
- [24d315fa85](https://github.com/yetone/magpie/commit/24d315fa856e25b858adb5a311ec1a67c5483c70) routing: the request list can be grouped By key, each gateway key's calls under its name
  different-administration-ui — Key-grouped request tables, route rules, session title polling, backup notices and provider-icon choice use peer-specific UI/data. Keep Axon layout and request-key attribution. Review: title and file inventory.
- [06508f2bf5](https://github.com/yetone/magpie/commit/06508f2bf5f027bc3cd0c51f321a5715604b3494) library: a stdio server that refuses home is checked again in a project folder (fff)
  different-plugin-contract — Peer plugin manifests, agent package settings, movers/check-in and shared library synchronization do not map to an Axon server plugin contract. Review: title and file inventory.
- [2891ec7a7b](https://github.com/yetone/magpie/commit/2891ec7a7b0f76f2b5f4894299f3d9a1767795bd) docs: Trae / Trae CN through magpie, in the start page's FAQ (en, zh, ja)
  peer-maintenance — Peer tests, documentation, release or repository maintenance; no Axon runtime change to port. Review: title and file inventory.
- [a2f7ed2e81](https://github.com/yetone/magpie/commit/a2f7ed2e81665c26f017385b7636866c189ace45) gateway: a hand-made group of embedding, rerank, speech, transcription or video models is served when its providers have picks
  different-model-group-ui — Peer group membership/listing paths differ from Axon model associations, key profiles and dedicated tool selectors; no extra group UI added. Review: title and file inventory.
- [5db1b07ed9](https://github.com/yetone/magpie/commit/5db1b07ed90f6e324d7183b136b941edd32a0bc1) gui: the tray panel's Usage tab is drawn again in Settings' currency picked alone, and when its read came before the state (dLdgKyRd)
  different-administration-ui — Peer display/settings/data model differs from Axon. Retain current UI components and channel configuration workflow. Review: title and file inventory.
- [01a260af46](https://github.com/yetone/magpie/commit/01a260af462cf7d46ffe3a8d537696348e49f090) routing: a group with no rule says what a rule is for, with an example rule (mr_coffee0083)
  different-administration-ui — Key-grouped request tables, route rules, session title polling, backup notices and provider-icon choice use peer-specific UI/data. Keep Axon layout and request-key attribution. Review: title and file inventory.
- [435f041514](https://github.com/yetone/magpie/commit/435f0415140ca58a8af49eab46754d4903d35da9) gateway: an account near its usage cap takes one turn at a time (dyzdyz010)
  adapted-window-threshold-concept — Implement the accepted independent 5h/7d reserve and retained-workload policy. Do not copy the peer fixed near-cap margin, synchronous usage refresh and serial queue; current threshold uses the latest collected snapshot and permits in-flight overshoot. Review: complete cap_near.go production patch.
- [465928bcce](https://github.com/yetone/magpie/commit/465928bccea4fec3537edd6280f7cabb9a0c5bad) backup: the default backup folder says a reinstall takes it too; docs say how to keep magpie on another disk (two_2025.)
  different-administration-ui — Key-grouped request tables, route rules, session title polling, backup notices and provider-icon choice use peer-specific UI/data. Keep Axon layout and request-key attribution. Review: title and file inventory.
- [bd0f338282](https://github.com/yetone/magpie/commit/bd0f3382827e4eedb2478773d5f67107ec0eac0a) hermes: a Claude model goes on anthropic_messages, so cache_ttl 1h works; the user's api_mode stays (#5315)
  covered-explicit-model-protocol — Axon already supports explicit per-model protocol settings, retained when merging upstream endpoint-family preference. Review: title and file inventory.
- [d620ff6b54](https://github.com/yetone/magpie/commit/d620ff6b54b8959f9e8a8e5309e9ab78ab006da6) codex: an old OpenAI thread follows Codex onto magpie by its row, and back at the Disconnect (Parry_Moody, Cyrus)
  different-desktop-lifecycle — Local agent sign-in, restarts, app takeover/disconnect or installer architecture are desktop lifecycle features; Axon remains the gateway server. Review: title and file inventory.
- [b63b289589](https://github.com/yetone/magpie/commit/b63b289589b16ccad103e403f6b9192c315b53ed) provider: WorkBuddy's mover min is 0.2.22, extra credits keep the account going once the plan's are spent (ferock)
  different-plugin-contract — Peer plugin manifests, agent package settings, movers/check-in and shared library synchronization do not map to an Axon server plugin contract. Review: title and file inventory.
- [a732f3c493](https://github.com/yetone/magpie/commit/a732f3c493a95d83650522ae4b59729d88241484) gateway: models of one API other than chat are grouped by id on their route (#5339)
  different-model-group-ui — Peer group membership/listing paths differ from Axon model associations, key profiles and dedicated tool selectors; no extra group UI added. Review: title and file inventory.
- [97ccb90e88](https://github.com/yetone/magpie/commit/97ccb90e88415ae414491589a0a9c0722fb5ac31) routing: a group with no rule shows only the example rule's button, no paragraph under Rules (mr_coffee0083)
  different-administration-ui — Peer display/settings/data model differs from Axon. Retain current UI components and channel configuration workflow. Review: title and file inventory.
- [331f201faf](https://github.com/yetone/magpie/commit/331f201faf60745f685113f1ae24331cea2e8ded) plugin: a cross-signed root copy in Windows' ROOT store no longer shadows Bun's own root (youyo.p_10001)
  different-plugin-contract — Peer plugin manifests, agent package settings, movers/check-in and shared library synchronization do not map to an Axon server plugin contract. Review: title and file inventory.
- [965ae78e1e](https://github.com/yetone/magpie/commit/965ae78e1e28b39c9946bd6cc50d8c97e91d8e78) gateway: a routing group draws on its own image member, Codex's tool too
  different-model-group-ui — Peer group membership/listing paths differ from Axon model associations, key profiles and dedicated tool selectors; no extra group UI added. Review: title and file inventory.
- [08982805eb](https://github.com/yetone/magpie/commit/08982805ebcbab069657c791fdd0bbb7e8f3630c) gui: spinners stand still under reduced motion instead of flickering
  different-administration-ui — Peer display/settings/data model differs from Axon. Retain current UI components and channel configuration workflow. Review: title and file inventory.
- [4f624241f7](https://github.com/yetone/magpie/commit/4f624241f7c5000b6b59fe53a1c1c6de052dfa87) codex: the ChatGPT app left on "hit a snag" after an app-server restart is reopened when idle (#4550)
  different-desktop-lifecycle — Local agent sign-in, restarts, app takeover/disconnect or installer architecture are desktop lifecycle features; Axon remains the gateway server. Review: title and file inventory.
- [6d9fb7ef0f](https://github.com/yetone/magpie/commit/6d9fb7ef0f844e79e0000e4183c7b2fc8126ea89) routing: a dsh request names the dsh session its first prompt was read from
  retain-owned-request-metadata — Peer infers conversation/folder identity from local Claude/dsh transcripts or quoted working-directory text. Preserve explicit structured request ownership and metadata. Review: title and file inventory.
- [1b0340fdbc](https://github.com/yetone/magpie/commit/1b0340fdbca37d4d202f0cecbb7b9d463942ae2b) provider: WorkBuddy's mover min is 0.2.23, which hides DeepSeek Harness's name
  different-plugin-contract — Peer plugin manifests, agent package settings, movers/check-in and shared library synchronization do not map to an Axon server plugin contract. Review: title and file inventory.
- [d3a0be6dda](https://github.com/yetone/magpie/commit/d3a0be6dda5577800860423e34159c84afd0560c) provider: Google's verify link is the one Gemini CLI opens, read whole from any body (pppuuu)
  peer-maintenance — Peer tests, documentation, release or repository maintenance; no Axon runtime change to port. Review: title and file inventory.
- [24e1a21095](https://github.com/yetone/magpie/commit/24e1a21095a420a0ccf2cd3edbdabad4a84f9b4e) sessions: list ZCode's rows, not its messages, for a title poll
  different-administration-ui — Key-grouped request tables, route rules, session title polling, backup notices and provider-icon choice use peer-specific UI/data. Keep Axon layout and request-key attribution. Review: title and file inventory.
- [52563d1cdf](https://github.com/yetone/magpie/commit/52563d1cdfcb2ee3fb5c421c163408afcc00d8d4) sessions: a ZCode name poll reads its sessions' messages for their stamp, as the listing does (#5365)
  different-administration-ui — Key-grouped request tables, route rules, session title polling, backup notices and provider-icon choice use peer-specific UI/data. Keep Axon layout and request-key attribution. Review: title and file inventory.
- [a9b1877cfc](https://github.com/yetone/magpie/commit/a9b1877cfc412e212d7ee049f0937804ccad9379) routing: a session's name stays when a later poll answers without it (#5365)
  different-administration-ui — Key-grouped request tables, route rules, session title polling, backup notices and provider-icon choice use peer-specific UI/data. Keep Axon layout and request-key attribution. Review: title and file inventory.
- [51bdc985bc](https://github.com/yetone/magpie/commit/51bdc985bc413c9e87a976ba08bcfa1054a0b699) provider: a cap set on a window a plugin later sets aside still holds; WorkBuddy min 0.2.24 (ferock on Discord)
  adapted-explicit-threshold-precedence — Explicit channel thresholds apply even to sticky/phase-two or ignore-quota candidates; no peer plugin-aside mechanism is introduced. Review: selected account cap/routing patches.
- [2f59545d29](https://github.com/yetone/magpie/commit/2f59545d29f9f951459dfaf9c629fbb84ee04157) gateway: a key not given a group listing alone is listed the members it may use (luckymanjc)
  different-model-group-ui — Peer group membership/listing paths differ from Axon model associations, key profiles and dedicated tool selectors; no extra group UI added. Review: title and file inventory.
- [f76b48ea09](https://github.com/yetone/magpie/commit/f76b48ea09df7b0dad82fac0f909fb40dd4e8eb0) gateway: a Chat finish of tool_calls with no call in the stream ends stop (#4944)
  retain-honest-terminal-state — Do not fabricate successful tool completion or hand over after committed output. Axon keeps incomplete/length/refusal terminal evidence. Review: title and file inventory.
- [ae3c2ec4e9](https://github.com/yetone/magpie/commit/ae3c2ec4e90da14caef83f52894c510e9ffc1ea4) fix(netproxy): the health check pings a silent connection after 10s, not 20 (#4936)
  different-health-probe — Peer netproxy health ping changes from 20s to 10s; retain Axon established transport heartbeat/timeouts without copying a desktop probe interval. Review: title and file inventory.
- [d45e74d89f](https://github.com/yetone/magpie/commit/d45e74d89f4dc69e1096cef1a2b6b12649d3f113) gateway: a length cut that said nothing is not a completed reply (#2972)
  retain-honest-terminal-state — Do not fabricate successful tool completion or hand over after committed output. Axon keeps incomplete/length/refusal terminal evidence. Review: title and file inventory.
- [2027bb5a6d](https://github.com/yetone/magpie/commit/2027bb5a6d2d42f4092fe781af99c6dd6aa05bc0) gateway: a filter's finish code after the reply began asks the group's next member (#5075)
  retain-honest-terminal-state — Do not fabricate successful tool completion or hand over after committed output. Axon keeps incomplete/length/refusal terminal evidence. Review: title and file inventory.
- [c2dd693dac](https://github.com/yetone/magpie/commit/c2dd693dac8dbd3f9f6aa5301b292d46aa0c93a5) gateway: a 451 censorship_blocked refusal asks the group's next member (#5089)
  adapted-configured-pre-output-failover — Memory refusals follow existing retry status/pattern settings and the shared three-attempt cap, with no repeat of a refused physical channel. Do not introduce post-output handover or automatic universal 451 behavior. Review: selected production patch.
- [cfadcde168](https://github.com/yetone/magpie/commit/cfadcde168184bc91d2f14a4ba34b0beba832a38) provider: a model list OpenCode's key is refused is asked again bare, so the picker says what the vendor serves (#4900)
  retain-authenticated-discovery — Do not retry model discovery without the configured credential merely because the authenticated endpoint refuses it; avoid showing models unrelated to the channel account. Review: title and file inventory.
- [72e09cc019](https://github.com/yetone/magpie/commit/72e09cc0196257e57712e98607e054041cd6a934) catalog: a list's sora ids are video models, made at its OpenAI-shaped videos API (#4954)
  defer-video-api-surface — Sora video catalog/API support is a separate asynchronous media surface, not a patch to existing image/GIF handling. Review: title and file inventory.
- [8daab00a2e](https://github.com/yetone/magpie/commit/8daab00a2e85b2896109a34ce7d6961b942b5106) gateway: a completion call Antigravity's model wrote into its text goes out of the history, the answer it repeated staying (#5091)
  retain-native-history — Do not delete Antigravity completion-like text from history heuristically; native content and call structure remain intact. Review: title and file inventory.
- [8d94e3a38f](https://github.com/yetone/magpie/commit/8d94e3a38f6b244839eb45ea42abed2a8c819a12) codex: Ultra on GPT-6.1 Terra wherever it is served (#4909)
  retain-explicit-catalog-models — Preserve exact fetched/configured model IDs. Do not equate a peer Terra ultra catalog preset with the independent fast/ultrafast service-speed setting, or claim that preset is fully covered without a matching upstream contract. Review: selected production patch.
- [0d14aa4d08](https://github.com/yetone/magpie/commit/0d14aa4d082f79daa9f0639f3c22039abcc9728c) gateway: a model the vendor lists after the codex picks were saved still reaches the desktop catalog (#5062)
  different-desktop-catalog — Peer local agent picker/catalog refresh behavior differs from Axon channel model discovery; preserve exact IDs and explicit model selections. Review: title and file inventory.
- [e7948e2484](https://github.com/yetone/magpie/commit/e7948e2484468caa8ed4a0485408220edd009e24) agent: a routing group with a Claude member tells Pi its cache keeps five minutes (#5037)
  different-client-cache-hint — Peer Pi routing-group cache TTL advertisement belongs to its agent configuration contract; keep explicit Axon cache controls. Review: title and file inventory.
- [db9d6a6107](https://github.com/yetone/magpie/commit/db9d6a6107a357df982671e7a4173eb7de71a2b8) gateway: a working-directory line a turn quotes is no caller's word, and a --bare run's CWD: is read (#4940)
  retain-owned-request-metadata — Peer infers conversation/folder identity from local Claude/dsh transcripts or quoted working-directory text. Preserve explicit structured request ownership and metadata. Review: title and file inventory.
- [38c3ba846f](https://github.com/yetone/magpie/commit/38c3ba846fc9490dcff2b114eeae7e26afb58d4b) library: an edit made in the shared copy of a skill is taken into the library (#4958)
  different-plugin-contract — Peer plugin manifests, agent package settings, movers/check-in and shared library synchronization do not map to an Axon server plugin contract. Review: title and file inventory.
- [72abde0e0d](https://github.com/yetone/magpie/commit/72abde0e0db1f56e8cc45a7ef1cd37c4cbfe207e) steady: a served write reaps the stale temps earlier writes left beside the file (#4938)
  different-file-writer — Peer steady-file writer temp-file reclamation has no matching Axon implementation; retain existing atomic storage lifecycle. Review: title and file inventory.
- [6b4a6dcd41](https://github.com/yetone/magpie/commit/6b4a6dcd417aa83684c4867ddd7e607c52b7d465) claudetui: the Bypass Permissions disclaimer is answered as the trust question is (#5042, luzhuang)
  different-administration-ui — Peer display/settings/data model differs from Axon. Retain current UI components and channel configuration workflow. Review: title and file inventory.
- [7c5c90f958](https://github.com/yetone/magpie/commit/7c5c90f958a4a7db986e486895733447fb374569) provider: a ChatGPT account's GPT Image is tested by drawing, as the gateway draws it (ChangQiu on Discord)
  defer-independent-runtime-extension — Peer native search/archive/test-generation, account aggregation, JSON-in-SSE or parser internals are independent contracts; retain the current Axon request/usage and parser paths without speculative conversion or extra inference. Review: title and file inventory.
- [8c733bb362](https://github.com/yetone/magpie/commit/8c733bb36232e04d74a47ff2b3e05f71c3c6fa7b) gateway: a bare JSON error line in an event stream is the vendor's error
  defer-independent-runtime-extension — Peer native search/archive/test-generation, account aggregation, JSON-in-SSE or parser internals are independent contracts; retain the current Axon request/usage and parser paths without speculative conversion or extra inference. Review: title and file inventory.
- [2d6939ec64](https://github.com/yetone/magpie/commit/2d6939ec640e9292318e01f6fa0e649ca4e87cd8) gateway: Automatic draws with the newest GPT Image, and a ChatGPT account's shows as GPT Image (StarBits on X)
  retain-explicit-model-and-image-detail — Keep exact model variants and caller image detail instead of forcing high or automatically substituting the latest image model. Review: title and file inventory.
- [d00e41604c](https://github.com/yetone/magpie/commit/d00e41604cb6335bc8f492a46cf54b47bc47df70) settings: the image model error's example is gpt-image-2, not the shut-down gpt-image-1
  different-administration-ui — Peer display/settings/data model differs from Axon. Retain current UI components and channel configuration workflow. Review: title and file inventory.
- [055a81e9ac](https://github.com/yetone/magpie/commit/055a81e9ace745b90972a5aa126b499663149334) gui: the image chips' names fit the core's script budget, no raise
  different-administration-ui — Peer display/settings/data model differs from Axon. Retain current UI components and channel configuration workflow. Review: title and file inventory.
- [8bfc551ee2](https://github.com/yetone/magpie/commit/8bfc551ee2fb2a1625d8bcc865096f6a27d58f06) providers: a built-in icon picked over one found from the base URL is the one saved (Refeccd on Discord)
  different-administration-ui — Key-grouped request tables, route rules, session title polling, backup notices and provider-icon choice use peer-specific UI/data. Keep Axon layout and request-key attribution. Review: title and file inventory.
- [326e72f4c5](https://github.com/yetone/magpie/commit/326e72f4c5ac23c650112014150602b38ceff0e7) routing: the "No session ID" group folds like the others, and one button folds or opens every group (zzzkl on Discord)
  different-administration-ui — Peer display/settings/data model differs from Axon. Retain current UI components and channel configuration workflow. Review: title and file inventory.
- [2df5d40ee3](https://github.com/yetone/magpie/commit/2df5d40ee3ef8de347a01c850ea5f43f61c1b436) usage: a ChatGPT API account's calls count in the same ChatGPT account's Codex window estimate (jctaoo on Discord)
  defer-independent-runtime-extension — Peer native search/archive/test-generation, account aggregation, JSON-in-SSE or parser internals are independent contracts; retain the current Axon request/usage and parser paths without speculative conversion or extra inference. Review: title and file inventory.
- [125fd93ecd](https://github.com/yetone/magpie/commit/125fd93ecdd1c7d49268a7ae11e0dced669b40be) gateway: an overlong message or call id goes left out, and any other is bound under its kind's prefix, which Copilot checks (echo_ts, #5266, #4972)
  retain-native-identifiers — Do not drop native reasoning/signature/item identity based on origin or length guesses without a matching owned-history fixture. Review: title and file inventory.

## cc-switch — 30 records
`ca58c0313360eae7c851fbd7553c1614c7af03e7` → `4e13413ff236860b445e6c7f3f7590e999de0eea`

- [50a51b867b](https://github.com/farion1231/cc-switch/commit/50a51b867b2365a1aae60f565c346e55e8953e89) perf(runtime): defer maintenance and pause hidden-window work (#9182)
  desktop-specific — Desktop provider switching, process/session/UI or application installation paths differ from Axon server channels; preserve existing Axon UI. Review: title and file inventory.
- [2186b30525](https://github.com/farion1231/cc-switch/commit/2186b3052549710e947a865e70927023d3f6100e) fix(apps): append actionable hint when npm install hits a bin-path collision (#9196)
  desktop-specific — Desktop provider switching, process/session/UI or application installation paths differ from Axon server channels; preserve existing Axon UI. Review: title and file inventory.
- [cc4ce03860](https://github.com/farion1231/cc-switch/commit/cc4ce03860b5987e4928dcbee76f17b29eec20e6) perf(sessions): avoid transcript copies and release lightweight caches (#9183)
  desktop-specific — Desktop provider switching, process/session/UI or application installation paths differ from Axon server channels; preserve existing Axon UI. Review: title and file inventory.
- [1adb3cbc46](https://github.com/farion1231/cc-switch/commit/1adb3cbc466d15d759220785f120055d387d4729) fix(codex): stop GitHub Copilot Responses replies from showing twice (#8025)
  defer-provider-id-rewrite — Peer stabilizes Copilot item IDs by output index. No matching Axon failing Copilot fixture establishes when an opaque ID may be rewritten; retain upstream IDs. Review: complete new production file.
- [02a136413b](https://github.com/farion1231/cc-switch/commit/02a136413bacb7ba33a94c89ecbed527666d4e23) fix(codex): reuse the Grok compat layer for GitHub Copilot Responses (#7989)
  different-copilot-sanitizer — Copilot-specific Grok sanitization needs its own protocol fixture; do not globally strip request fields. Review: inventory and selected comment hunk.
- [df3a9072a2](https://github.com/farion1231/cc-switch/commit/df3a9072a280d2ad54427a132c5a0b334b5d54a4) fix(webdav-sync): preserve file symlink names in skills archives (#9197)
  desktop-specific — Desktop provider switching, process/session/UI or application installation paths differ from Axon server channels; preserve existing Axon UI. Review: title and file inventory.
- [78971d563f](https://github.com/farion1231/cc-switch/commit/78971d563f7819e8a77bfe26a51cb159ba3603c7) feat(copilot): show every capped quota and label premium as Credits
  desktop-specific — Desktop provider switching, process/session/UI or application installation paths differ from Axon server channels; preserve existing Axon UI. Review: title and file inventory.
- [cf3c86fe53](https://github.com/farion1231/cc-switch/commit/cf3c86fe53e7905c3b7f4062141209213bdbaaf2) feat(auth): show each account's subscription plan in the Auth Center
  desktop-specific — Desktop provider switching, process/session/UI or application installation paths differ from Axon server channels; preserve existing Axon UI. Review: title and file inventory.
- [626f0226f7](https://github.com/farion1231/cc-switch/commit/626f0226f7f9644181a4a0a563a40e2b2a4e10bd) feat(providers): show the plan on official provider cards
  desktop-specific — Desktop provider switching, process/session/UI or application installation paths differ from Axon server channels; preserve existing Axon UI. Review: title and file inventory.
- [3cf79926d5](https://github.com/farion1231/cc-switch/commit/3cf79926d5f66136573ca2ef0f283a3d0d068cc2) fix(claude-desktop): point the official card at claude.com/download
  desktop-specific — Desktop provider switching, process/session/UI or application installation paths differ from Axon server channels; preserve existing Axon UI. Review: title and file inventory.
- [51b464bd3b](https://github.com/farion1231/cc-switch/commit/51b464bd3bde18c68adccfe97303cb730f391885) feat(deeplink): import providers for Claude Desktop and the Pi-family apps
  desktop-specific — Desktop provider switching, process/session/UI or application installation paths differ from Axon server channels; preserve existing Axon UI. Review: title and file inventory.
- [420646d8a5](https://github.com/farion1231/cc-switch/commit/420646d8a5423bfc12f6d53f245a898204b5078d) fix(dsh): let headless profiles read keys CC Switch stores
  desktop-specific — Desktop provider switching, process/session/UI or application installation paths differ from Axon server channels; preserve existing Axon UI. Review: title and file inventory.
- [1c2ed06690](https://github.com/farion1231/cc-switch/commit/1c2ed06690fe4a474346c59328b54521260f4e17) fix(apps): explain npm bin collisions instead of echoing --force
  desktop-specific — Desktop provider switching, process/session/UI or application installation paths differ from Axon server channels; preserve existing Axon UI. Review: title and file inventory.
- [65f6e682ad](https://github.com/farion1231/cc-switch/commit/65f6e682ad9c8262eb7a0ac2228b588079237cf7) fix(mcp): show sign-in required instead of failed for 401 MCP endpoints
  desktop-specific — Desktop provider switching, process/session/UI or application installation paths differ from Axon server channels; preserve existing Axon UI. Review: title and file inventory.
- [e6e7831e83](https://github.com/farion1231/cc-switch/commit/e6e7831e83d3f339922f198921974c3c818dc595) fix(claude-desktop): let auto-filled menu names follow model edits
  desktop-specific — Desktop provider switching, process/session/UI or application installation paths differ from Axon server channels; preserve existing Axon UI. Review: title and file inventory.
- [a990c81a18](https://github.com/farion1231/cc-switch/commit/a990c81a18d24324c34daf6b406d8c97f6fc8e7c) fix(proxy): race IPv4 when IPv6 upstream addresses are unreachable
  covered-go-dialer — Axon uses net.Dialer with the default address-family fallback enabled; do not copy Tauri/reqwest Happy Eyeballs configuration. Review: selected production patch.
- [7c250c4234](https://github.com/farion1231/cc-switch/commit/7c250c4234c49627e0087f7fb92a2f1d4b195ef1) feat(proxy): warn when the system proxy hijacks the local route
  desktop-specific — Desktop provider switching, process/session/UI or application installation paths differ from Axon server channels; preserve existing Axon UI. Review: title and file inventory.
- [62688ab38f](https://github.com/farion1231/cc-switch/commit/62688ab38f9f79a44d4b6c21e992393bc27d55e8) feat(codex): let a card keep the official Fast speed tier on GPT rows
  covered-speed-policy — Axon already has explicit native/fast/ultrafast channel speed policy and model suffix resolution; no desktop optional-model toggle transplanted. Review: partial selected catalog patch.
- [80ad02c5f4](https://github.com/farion1231/cc-switch/commit/80ad02c5f49bfb81a1c5b690bbc1e2ccfe5534a1) fix(proxy): log the mapped upstream model on failed requests
  covered-execution-model-record — Axon stores per-attempt model identity in RequestExecution; no desktop proxy log field migration needed. Review: selected production patch.
- [38efe10995](https://github.com/farion1231/cc-switch/commit/38efe1099502bdee9d10ef201d45a69a5c9b3fad) fix(proxy): scope the Codex chat call_id cache to the client session
  retain-owned-call-history — Peer scopes its call-ID cache. Retain Axon project/API-key/thread-owned session history; no global call-ID cache introduced. Review: selected cache methods.
- [abf441fe2e](https://github.com/farion1231/cc-switch/commit/abf441fe2e6936f48077126e169da22ab93f8ce8) fix(proxy): keep turn-start developer messages in place for Codex chat
  retain-developer-role — Do not downgrade mid-turn developer messages to user text to fit a peer bridge. Preserve native roles and history. Review: partial selected role bridge patch.
- [1d9b448efb](https://github.com/farion1231/cc-switch/commit/1d9b448efb58be17cf24ea8ca6efbd31b6b979f3) fix(subscription): refresh expired Claude login for follow-login cards
  different-desktop-credential-lifecycle — Local Claude CLI credential refresh is a desktop application responsibility, not Axon channel OAuth refresh. Review: partial selected credential patch.
- [7a0718e154](https://github.com/farion1231/cc-switch/commit/7a0718e1543b104f1143e582395b2a96d7fca92f) fix(ui): stop macOS from capitalizing text typed into raw inputs
  desktop-specific — Desktop provider switching, process/session/UI or application installation paths differ from Axon server channels; preserve existing Axon UI. Review: title and file inventory.
- [0f06fed26e](https://github.com/farion1231/cc-switch/commit/0f06fed26e7f5ff68a7a4063592bc1643cdbf905) fix(ui): center dialogs without translate so they stay sharp on 1x displays
  desktop-specific — Desktop provider switching, process/session/UI or application installation paths differ from Axon server channels; preserve existing Axon UI. Review: title and file inventory.
- [81a9fa79d3](https://github.com/farion1231/cc-switch/commit/81a9fa79d3368db6e58f9a99edd2fa4bee61175b) feat(dsh): allow overriding the DeepSeek Harness home directory
  desktop-specific — Desktop provider switching, process/session/UI or application installation paths differ from Axon server channels; preserve existing Axon UI. Review: title and file inventory.
- [a5100e0e63](https://github.com/farion1231/cc-switch/commit/a5100e0e63a3256d5b77fa22003fe0c220b9dab0) chore(presets): switch TeamoRouter domain from .cn to .com
  desktop-specific — Desktop provider switching, process/session/UI or application installation paths differ from Axon server channels; preserve existing Axon UI. Review: title and file inventory.
- [44c7fa9bc9](https://github.com/farion1231/cc-switch/commit/44c7fa9bc94e648d44c375ef4fe5d4b3b27cb033) fix(proxy): fill outbound_model on the classifier-loop early return
  covered-execution-model-record — Axon stores per-attempt model identity in RequestExecution; no desktop proxy log field migration needed. Review: selected production patch.
- [8c05b62264](https://github.com/farion1231/cc-switch/commit/8c05b6226489ab5727a9af7482826350d821cef1) chore(release): v4.0.26
  desktop-specific — Desktop provider switching, process/session/UI or application installation paths differ from Axon server channels; preserve existing Axon UI. Review: title and file inventory.
- [f621d42161](https://github.com/farion1231/cc-switch/commit/f621d4216189ef06bcc6acd3588c84937b9794e1) docs(release): add v4.0.26 release notes
  desktop-specific — Desktop provider switching, process/session/UI or application installation paths differ from Axon server channels; preserve existing Axon UI. Review: title and file inventory.
- [4e13413ff2](https://github.com/farion1231/cc-switch/commit/4e13413ff236860b445e6c7f3f7590e999de0eea) docs(manual): add the Japanese FAQ for Copilot replies showing twice
  peer-maintenance — Peer tests, documentation, release or repository maintenance; no Axon runtime change to port. Review: title and file inventory.
