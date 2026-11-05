# Workload composition, quota reserve and upstream integration — 2026-11-06

Base: `f78b4b185cb9afa9a3eb86b2cacdecf7d0b0653b` (PR31). Upstream unstable is merged through `f85d7173e655bf2f90ad0fd00a19ea55fe21e9e8`.

## User-visible behavior

Memory-only, standalone search and standalone image flags compose instead of clearing each other. Selecting any dedicated workload excludes ordinary conversation; selecting several admits their union. Memory exclude remains authoritative. Ordinary inline tool-bearing chat is still conversation, not a standalone tool request.

Previously, memory content-policy rejection unconditionally stopped retries. Now the existing retry enablement and channel retryable HTTP status/error-pattern settings apply before any content is committed. Eligible normal and memory-only channels participate subject to project/key profiles, model mappings and selected workloads. A refused physical channel is skipped for the remainder of that request, and memory has a shared three-attempt maximum. This does not synthesize successful memory, edit its input, or retry after committed output.

Codex/Claude Code channel editing now includes **Quota reserve / 额度保留** beside quota routing. Optional 5h and 7d integer percentages are independent; either threshold invokes Pause all or Keep selected tools. Retained search, image and memory choices are independent of always-on dedicated flags and do not grant capability or bypass other routing constraints. The normal policy applies below threshold and resumes after the corresponding window resets.

Limits use the existing provider quota snapshot, not inferred dollars/tokens. Unknown windows do not trigger a reserve. In-flight/concurrent requests may overshoot a threshold, so this is allocation control rather than a strict prepaid budget. Model-specific/image/alternative-capacity buckets are not confused with the account-wide windows. Selection, sticky fallback and final dispatch/retry checks enforce explicit reserve thresholds.

The UI uses existing Label, Switch, Input, Select, Checkbox and icon-only Tooltip components with no always-visible explanatory paragraphs. Disabled reserve collapses to one compact row. Chinese/English, light/dark, and 1280/768/390px save/reopen scenarios run in CI.

## Upstream conflict decisions

- Preserve the fork's existing change filters, source-generation checks, Windows/SQLite/Responses transport coverage and CI concurrency groups. The newly upstream `changes.yml` is redundant and omitted.
- Keep SWC and fork-only frontend packages/security overrides. Update matching dependency versions; generate the lockfile in CI rather than resurrecting unused react-plugin dependencies.
- Preserve module replacement directives and fork transport tests. Reconcile Go module graph and formatting through CI artifacts.
- Merge Copilot `reasoning_text` and the updated Anthropic catalog. Existing non-stream/stream content-filter mappings already cover the upstream refusal behavior; remove duplicate merged switch cases and retain that behavior.
- Merge endpoint family preference while retaining explicit per-model protocol precedence and dedicated search/image routing.

## Frozen peer review

The previous cutoff was recovered from `.agent/summary/2026-11-05-upstream-peers/peers-ledger.json`; each new remote head was read once at approximately 2026-11-06 00:19 Beijing. No repeated head polling or old-commit review was used.

| Repository | New records | Frozen head |
| --- | ---: | --- |
| upstream unstable | 15 | f85d7173e655 |
| sub2api | 5 | 25608d216bb7 |
| CLIProxyAPI | 19 | 585f97deac9a |
| OmniProxy | 0 | 4bcee95bf183 |
| codex2api | 4 | 9676bfe8983f |
| gpt-load | 1 | f5ed5eb80f92 |
| new-api | 4 | 2de51a19e35a |
| magpie | 102 | 125fd93ecdd1 |
| cc-switch | 30 | 4e13413ff236 |

See `PEERS.md` and `peers-ledger.json` for all 180 records, including merges, scope decisions and actual review depth. Inventory-only and partial reads are explicitly identified; they are not claims of full patch audit or feature parity.

[new-api 3374f251e3](https://github.com/QuantumNous/new-api/commit/3374f251e3) exposed the missing `markup` field in native image edits. The existing complexity/GIF implementation already covered the other relevant fields. [OpenAI's markup contract](https://developers.openai.com/api/docs/guides/image-generation#guide-an-edit-with-markup) confirms a separate transparent PNG overlay for Images edits, via multipart file or JSON image_url/file_id reference. Axon now preserves that overlay across native paths and refuses mask/prompt_rewriter conflicts or unsupported provider conversion instead of silently dropping the overlay. The provider still validates image dimensions and markup content.

Magpie account-cap and refusal-routing work informed the accepted window thresholds and configured pre-output memory failover. Its fixed near-cap margin/serialized refreshing queue, plugin-aside mechanics and post-output handover are not copied. Existing native Codex Images, exact 2.6 model selection, fast/ultrafast policy, scoped session ownership and Go address-family fallback cover the matching CPA/cc-switch changes. Other desktop/plugin/transcript changes do not belong in the server gateway; speculative identity rewriting and synthetic compaction fallback are excluded.

## Validation and delivery

All generation, dependency resolution, formatting, tests, builds and lint run only on GitHub Actions. The Test source-contract artifact supplied GraphQL code, module and lockfile updates. Quota partial-patch handling explicitly distinguishes an omitted field from a null clear and retains existing settings when omitted.

Required final checks: Test, Build, Lint, Docker build (PR), Govulncheck on the same SHA. Final CI, dev/tag and Run GoReleaser start receipts are saved in ignored local DELIVERY.md/DELIVERY.json so release metadata does not create a post-validation commit. No production account settings or live inference probes are changed by this work.
