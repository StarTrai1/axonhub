# Codex standalone tools and speed aliases

Baseline: `ef2fca828272ac8ee6e36ab0871681f0fd942574`.

## Evidence and scope

- The linked conversation `01a1aa82-d062-7350-b580-00db6ffd26ce` ended twice with no callable `image_gen` tool. It did not contain an upstream image request or rejection. This is evidence of missing client tool availability, not failed channel inference.
- OpenAI's [GPT Image 2.6 Sunburst model page](https://developers.openai.com/api/docs/models/gpt-image-2.6-sunburst) identifies the exact model and Images generation/edit endpoints. The [image guide](https://developers.openai.com/api/docs/guides/image-generation#gpt-image-26) documents independent complexity and quality controls. No assumption that `gpt-image-2` selects 2.6.
- [Codex speed documentation](https://developers.openai.com/codex/speed) and [API Ultrafast guide](https://developers.openai.com/api/docs/guides/ultrafast-mode) distinguish client controls from API tiers. Fast uses priority; Ultrafast is a distinct tier, including GPT-6 Astra and GPT-6.1 Sol.
- Codex [0.166 image extension](https://github.com/openai/codex/blob/rust-v0.166.0/codex-rs/ext/image-generation/src/extension.rs) creates a local tool based on provider configuration; the [tool implementation](https://github.com/openai/codex/blob/rust-v0.166.0/codex-rs/ext/image-generation/src/tool.rs) hard-codes `gpt-image-2`. A gateway declaration cannot install that extension or make its tool accept a model argument.
- AxonHub's official Codex model parser supplements `gpt-image-2`; its relay model discovery follows the relay response. Preserve this distinction: the UI offers explicit manual 2.6 additions, not fabricated upstream discovery.
- [CC Switch issue 6997](https://github.com/farion1231/cc-switch/issues/6997) reports missing service-tier metadata suppressing Fast controls. Its HTTP/WS timing claims are user reports, not an Axon acceptance result. Codex [0.166 service-tier selection](https://github.com/openai/codex/blob/rust-v0.166.0/codex-rs/tui/src/chatwidget/service_tiers.rs) confirms catalog/feature gating.
- CLIProxyAPI [disable-image-generation configuration](https://github.com/router-for-me/CLIProxyAPI/blob/78a9cc9aaa8ae90a66300d9cbddfe7c894307622/internal/config/disable_image_generation_mode.go) separates Images endpoints from conversation requests. Sub2api [image bridge policy](https://github.com/Wei-Shaw/sub2api/blob/8e92c892b47f3f934f18ce63da4c4dcb75f0b6bd/backend/internal/service/codex_image_generation_bridge.go) has explicit per-channel/account overrides. These support explicit configuration; neither justifies injecting hosted image tools or stripping history here.
- Magpie [Codex catalog](https://github.com/yetone/magpie/blob/435f0415140ca58a8af49eab46754d4903d35da9/internal/gateway/codex_catalog.go) preserves native catalog/service-tier metadata and filters by caller permissions. Axon keeps its selected source, originator isolation and permission intersection.

Focused source review only; prior whole-peer commit review is not repeated. Source snapshots are in `/tmp/axonhub-tools-20261105`. YCE gate passed, result `/tmp/yce-results/yce-search-20261105T071645-2370440-04e90a.xml`, SHA-256 `2948632eb48d81edf3e23c99e87bab4633028b2cc742494d6f5e8bef7d8eaf69`. Its broad web lane was partial and contained irrelevant results; implementation relies on fetched primary docs/source above.

## Resulting behavior

- Independent `codexSearchOnly` and `codexImageOnly` flags. Either reserves a Codex channel; only the selected standalone tools are admitted. Ordinary HTTP/WS Responses, Chat and continuation cannot use reserved channels. Existing search model pinning and profile/key restrictions remain in force.
- Native image generation/edit keeps exact 2.6 model IDs and carries `complexity` through JSON/multipart conversion. Explicit UI additions become manual models, surviving model sync.
- Enabled known speed aliases can reuse native base catalog instructions and tool metadata. Exact upstream aliases win. No invisible model ID, invented subscription access, or invented base metadata is added. Base entries and the shared source are unchanged.
- Known `-ultrafast` aliases for Astra and Sol 6.1 map to the distinct tier. Existing explicit request tiers take precedence. Optional relay-only name preservation sends the actual alias without injecting a tier. Official Codex ignores name preservation.
- Alias selection is an alternative to client speed controls; this change does not promise to activate a desktop control gated by the client, workspace or account.

## Validation and delivery

All executable verification and generation run in GitHub Actions. Regression coverage targets tool routing combinations/profile boundaries, native Images dispatch, exact model/complexity conversion, native catalog alias isolation, explicit tier precedence and UI save/reopen at desktop/tablet/mobile widths. Delivery follows the existing passing-SHA five-workflow gate and GoReleaser-start boundary.
