# Upstream / peer integration, 2026-10-25

## Scope and review boundary

Start from PR13 / dev `7cd6b4f1`, retaining the four requested Codex 0.164, keepalive, native probe and capacity retry fixes. Merge upstream/unstable through `a7ad1f9b7a49fcb7bf305d5e25ae266cb1d97580`. The previous peer snapshot is 2026-10-24 02:32:29 Beijing; the new per-repository frozen heads and every commit decision are in `peers-ledger.json` and `PEERS.md`. No second peer review window was opened.

## Resulting behavior

- Synthetic search counters and weekly credits use upstream normalization. Existing channel health/usage presentation remains without a duplicate quota column.
- Claude Code WebSearch receives progress and terminal source lists even when sources arrive late. Repeated snapshots emit one search/result pair. Sparse snapshots retain authentic encrypted results, explicit errors and action fields; gateway-issued IDs remain reversible. Link-only cross-protocol history is completed assistant text rather than pending function calls. Real encrypted results remain typed hosted history.
- Anthropic initial input usage is a client display estimate calibrated by completed authoritative upstream usage. Route/user isolation and bounded cache remain upstream contracts; billing and stored totals are not replaced by the estimate.
- Native Responses retains omitted/custom/empty prompt_cache_key policy. Codex successful-turn tracking remains alongside the new final-route snapshot. Existing metadata rejection recovery, probe requests and bounded overload backoff remain intact.
- HTTP clients strip credential headers when a redirect leaves the original scheme/host/port, and continue stripping if it returns. Same-origin credentials, tracing and explicit redirect refusal callbacks are retained.
- Top-level Chat imageConfig survives Gemini conversion and Chat replay, including unknown object fields and persisted metadata. Wrong scalar/array shapes fail with an explicit request error.
- Node 24 Corepack/Docker native build checks, Zod 4.6.6 and Vite 8.3.5 minimum are integrated. Dependency locks and generated sources are reconciled by CI.

## Upgrade behavior

Upstream removes Wafer quota support and deletes its retired provider_quota_status rows before enum migration, including soft-deleted quota snapshots. Other providers, channels and request data are outside this cleanup. SQLite tests are retained; temporary MySQL/PostgreSQL service tests are wired into CI. TiDB remains opt-in and is not claimed tested. No production migration has been run in this task.

## Validation and delivery

Compilation, tests, formatting, module/lockfile reconciliation and code generation run only in GitHub Actions. The initial merge exposed a duplicate JSON decoder plus expected generated-source drift; those were resolved using the existing document-aware decoder and CI artifacts. Regression coverage includes search replay/late metadata, redirect return-hop credential isolation, imageConfig round trips, real Codex loopback compatibility, offline probe scripts and quota migrations.

Final passing SHA, action runs, tag and GoReleaser observation belong to the local delivery receipt. Release proceeds only after required checks pass. No local binary installation, live service restart, production database/config edits or paid upstream probes are part of delivery.
