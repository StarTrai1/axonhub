# Integration scope — 2026-11-01

Base: `2293096fb40aa41b89ce429101a73f230928c499`. Branch: `sync/upstream-peers-20261101`.

Previous peer cutoff recovered from `.agent/summary/2026-10-30-upstream-peers/peers-ledger.json`. Frozen upstream: `b941556253f8be592c25d046a01247706f86885a`, already an ancestor of base; no new upstream merge. 389 new peer records, reviewed once at frozen heads. Metadata screening and selected runtime inspections are distinguished in the ledger.

All tests/builds/lint/formatters/tidy/generation run only in GitHub CI. Delivery: all five required workflows on the same SHA, fast-forward dev to that SHA, annotated unstable tag, stop monitoring when Run GoReleaser starts. No production configuration/process changes or inference. Preserve native Responses history, caching, session isolation and frontend style. Keep the existing latest-only Codex0.166.0/gpt-6.1-sol loopback coverage.

YCE search exit0; gate.may_analyze_or_edit_code=true; full EOF read. Result `/tmp/yce-results/yce-search-20261031T195113-58874-2d821b.xml`, 27824 bytes, SHA256 `8f1ca63552b260f5ee771d2887b6cf4ac14a3f3905b77d7e521cbb6d4d4d64f8`. No task anchor. Cached frozen snapshots/patches: `/tmp/axonhub-sync-20261101`. No subagents.
