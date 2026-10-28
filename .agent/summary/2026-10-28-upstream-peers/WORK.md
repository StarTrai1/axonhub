# Incremental upstream and peer integration

- Base: dev f198fe95b0652727a6b3e1cbb6b2c6d0b03e9af0 (PR18); branch sync/upstream-peers-20261028.
- Prior heads: ../2026-10-27-upstream-ccswitch/peers-ledger.json. Nine repositories frozen once to /tmp/axonhub-sync-20261028/*/snapshot.json; 302 records including merges. No refreshing heads or re-review of decided commits.
- Same authorized procedure: CI-only tests/build/lint/format/tidy/generation; preserve frontend style; fast-forward exact passing SHA to dev, annotated tag, monitor until GoReleaser starts. No production changes or paid inference.
- YCE search gate passed, exit0: /tmp/yce-results/yce-search-20261028T105940-893956-b001de.xml; 35805 bytes; d22a9119f33cec81589ef85b21bb32f380fed5afa5e67dc91bbe2429da9dd8f3. Full result read through EOF, no task anchor.
- Repo AGENTS and Go/cache/biz/frontend/GraphQL/docs/e2e rules read. No Markdown in ~/.codex/rules; no Jev routing or agents.
- Frozen incremental review complete: 302 records; seven peer adaptations. Upstream merged at a2ff7cd5. GitHub CI validation and exact-SHA release pending.

- Added user request: diagnose agent #70000/#70002; require direct evidence before repair. Read-only Windows online SQLite backup quick_check=ok, 1483264000 bytes, max request70034. No upstream executions/usage rows for either failure.
- Both fail with compaction_source_invalid at checkpoint_summary.go completeness boundary. Authenticated retained checkpoint has224 items. Prefix matches; tool pairs complete. Only blocker is the same agents.spawn_agent call at input263 (expanded480), with encrypted_function_args=[message]. Its local capsule authenticates under original project1/key1 and yields4681 original bytes. No real prompt/cipher/key committed.
- New YCE gate exit0 and true; full EOF read: /tmp/yce-results/yce-search-20261028T113538-916369-3e99d0.xml, bytes35876, sha256 dbd2fe6513350b8cc7d6119cd195c02cf284f16eb4066503c095a59b2fbd7f89.
- Repair shares the authenticated agent body recovery between outbound transport and pre-routing local checkpoint migration. Recover current and retained source copies before strict completeness/portability checks; keep immutable records, caller cache key, exact message text and tool pairing. Unknown/tampered/cross-owner seals remain errors.
