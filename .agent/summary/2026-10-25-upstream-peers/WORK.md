# Work record

- Base: dev 7cd6b4f1f765fadb54d76e9bbf763a9af0430599 (PR13, prior Codex/probe/retry repairs).
- Branch: sync/upstream-peers-20261025.
- YCE search receipt: exit 0; gate.may_analyze_or_edit_code=true; `/tmp/yce-results/yce-search-20261024T160853-2624150-ffb547.xml`, 34138 bytes, SHA256 236849238b49c42233b85145b93df5975b450883e85850ac7103e68334450992; complete EOF read, no task anchor.
- Merged upstream a7ad1f9b7a49fcb7bf305d5e25ae266cb1d97580; resolved 13 textual conflicts while retaining native cache-key omission, turn-state tracking, capacity retries and existing channel presentation.
- First CI found dependency/generated-source drift and duplicate MessageContentBlock.UnmarshalJSON from upstream. Applied CI-generated artifact from Test 40114264796, then integrated special-result decoding into the existing document-aware decoder.
- Peer adaptations: cross-origin HTTP credential stripping; Chat-to-Gemini imageConfig; Vite 8.3.5 minimum. Details and all skip/coverage boundaries are frozen in PEERS.md / peers-ledger.json.
- Validation, formatting, dependency and code generation run only in GitHub Actions. MySQL and PostgreSQL temporary service tests added for retired-quota migration.
- No local build/test/lint/format/generation, production database/config modification, service restart, binary install or paid upstream probe.
- Test 40115042390: LLM, all three real CLI loopback versions, offline probes and concurrency checks passed. Lint 40115042418 passed. Backend identified an old test asserting the previously known default-client redirect leak; inverted it to require credential stripping and synchronized its observation. Applied the second CI-generated artifact (lockfile/formatting).
- Frontend contract adjusted to assert Synthetic search labeling in the existing shared provider popover; the upstream table-column assertion targeted a duplicate column intentionally absent in this fork. Existing SSR tests still verify search count, percentage and renewal.
- Test 40115691971: backend, SQLite/MySQL/PostgreSQL cleanup, source contracts, LLM, all CLI versions and concurrency passed; Build 40115691999 and native Docker 40115691959 passed. Windows one loopback root request hit the existing 10s deadline while three heavy test packages ran concurrently. Serialize those packages with -p 1; keep all deadlines, goroutine tests, recovery assertions and Linux race checks unchanged. Final CI will verify the mitigation; exact root cause is not established by this single timeout.
- Delivery follows prior exact-tested-SHA dev fast-forward/tag/GoReleaser-start contract. Pending final CI completion.
