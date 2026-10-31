# Incremental integration — 2026-11-01

Two confirmed protocol conversion gaps are addressed. The current dev already contains frozen `upstream/unstable`; no upstream conflict or merge is needed.

## Changes

- Anthropic `tool_result.is_error: true` now becomes Gemini `functionResponse.response.error`, including the shared Antigravity path. Parsed JSON objects remain structured error details; text and literal `$ref` data remain intact. Successful tool outputs, call IDs/names and Gemini-version-specific media placement retain their encoding. The error key is documented by [Google FunctionResponse](https://ai.google.dev/api/generate-content#FunctionResponse). Sources: CLIProxyAPI [81de4b9c48](https://github.com/router-for-me/CLIProxyAPI/commit/81de4b9c48e8d823fa04d68cda09f23059f2964a) and its Antigravity follow-up `068fe153ef`.
- Chat providers reporting `finish_reason: "error"` in an HTTP200 response now produce a typed502 upstream failure during response/stream conversion, rather than a normal completion. Existing explicit provider errors take precedence; standard and unknown other finish reasons retain their handling. The pipeline uses its existing retry budget before output and stops on failures after output. Source: Magpie [86fef947e8](https://github.com/yetone/magpie/commit/86fef947e85f603b5530111ff427e7e2c0c330d3). Peer speculative buffering and post-output fallback are not imported.

These are adaptations to AxonHub's shared transformers, not foreign repository cherry-picks.

## Frozen review

| Repository | New records | Frozen head |
| --- | ---: | --- |
| upstream/unstable | 0 | b941556253f8 |
| sub2api | 0 | d851a3847759 |
| CLIProxyAPI | 27 | dbb706215a3a |
| OmniProxy | 0 | 4bcee95bf183 |
| codex2api | 0 | dfab070b3229 |
| gpt-load | 0 | 5d1d71c246d2 |
| new-api | 5 | ea343c4a8115 |
| magpie | 328 | 019bfc951b62 |
| cc-switch | 29 | 8be840557bac |

All389 records have titles, parents, files, decisions and inspection depth in `peers-ledger.json`; `PEERS.md` provides a readable list. Four peer projects have no new commits. Heads are not refreshed during review.

Important existing behavior retained: per-call Responses argument mapping, later developer/system message order, explicit Anthropic cache marks, permanent OAuth grant rejection suppression, bounded local schema reference expansion, thinking/effort mapping and the Oct31 owner/session/credential-scoped recovery. Partial coverage is labeled as such. Native Responses additional_tools already round-trips; the peer's Claude system-message tool_addition lowering is deferred because a cross-protocol ordered declaration contract is not established. No equivalence is claimed for peer-private plugins, billing, desktop clients or provider-wide metadata deletion.

## Validation and delivery contract

New CI regressions exercise true/false/omitted error flags with text/JSON/reference/media content, Antigravity patching, positive and negative finish-reason classification, and retry counts for non-stream, stream-before-output and stream-after-output failures. Existing GitHub workflows own the full LLM/backend/database/Windows/concurrency/CLI checks and formatting. No local tests or build tools run.

This tracked report states the implementation and required validation; exact-SHA workflow outcomes and release milestone are recorded separately in local ignored `DELIVERY.md`/`DELIVERY.json` after CI. A successful fixture run does not establish real-provider or production acceptance.
