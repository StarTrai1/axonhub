# Integration scope — 2026-11-03

Base: `bc3d9c1af9340d6f56adc668434b95b76bee5763`. Branch: `sync/upstream-peers-20261103`. Previous peer ledger: `.agent/summary/2026-11-01-upstream-peers/peers-ledger.json`.

Frozen upstream: `8a9df52283d9d9c3909c839267ce890a2913231e`. New records: 34 upstream and713 peers. Frozen snapshots/patches: `/tmp/axonhub-sync-20261103`. Screen each title/parent/file inventory once, inspect selected runtime patches, record depth and rationale, do not refresh heads or repeat old decided commits.

All project tests, builds, lint, formatting, dependency resolution and code generation run only in GitHub CI. Delivery is the exact SHA that passes all five required workflows, fast-forward dev, annotated unstable tag, stop monitoring at Run GoReleaser start. No production install/process/configuration changes or inference. Preserve native Responses history, cache boundaries, owner/session/credential isolation, frontend style and current Codex0.166.0/gpt-6.1-sol loopback matrix.

YCE search completed with exit0, gate.may_analyze_or_edit_code=true, full result through final EOF read. Result: `/tmp/yce-results/yce-search-20261103T095210-974227-ae9492.xml`; bytes33813; SHA256 `29ae879f56a6f44c6f783829efa564c17b23f887ba471f232f687279ff650bc0`. No task anchor and no subagents. Relevant Go, cache, service, GraphQL and frontend repository rules loaded. No candidate Markdown rules exist in ~/.codex/rules, so no Jev rule routing was needed.

CI first found duplicate dashboard helpers automatically introduced by the upstream move. The fork already owns these methods in dashboard_queries.go, with additional behavior, so retain the fork implementations. CI generation subsequently succeeded and exported only the regenerated frontend lockfile plus one gofmt alignment; these artifacts were applied locally without running project tools.

The existing catalog regression caught duplicate upstream Terra/Luna entries; preserve the richer fork entries and enforce unique IDs. The existing Markdown browser regression then caught Streamdown 2's removed bundled KaTeX API. Migrate to its separate code/math/Mermaid/CJK plugins, retaining sanitizer ordering and themes. Resolve the new dependency lockfile in GitHub CI and apply its artifact before final verification. These corrections follow concrete CI failures and do not reopen the frozen peer review.

CI at 51311213 passed the Markdown browser regression, but full backend tests exposed an existing Kimi fixture whose November 1 reset timestamp had expired. The unchanged normalizer correctly drops past reset times. Make that fallback test's timestamps relative to the test clock and assert the exact parsed reset time for RFC3339 and numeric strings; runtime quota behavior is unchanged.
