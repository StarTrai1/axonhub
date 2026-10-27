# Upstream and peer integration — 2026-10-27

Base: `e551d4669fbe38da89362eb0e85c5f511fe8cc6f`. PR: https://github.com/StarTrai1/axonhub/pull/18. Final validation/release receipt is recorded separately in local DELIVERY.md/DELIVERY.json to preserve the exact tested release SHA.

## Reviewed scope

171 frozen records: upstream 25, sub2api 24 (two merge records), magpie 112, gpt-load one, cc-switch eight incremental plus the explicitly requested older Azure commit. CLIProxyAPI, OmniProxy, codex2api and new-api have no new commits. Previous boundaries come from ../2026-10-27-upstream-terra/peers-ledger.json. Each SHA has a decision and evidence in PEERS.md and peers-ledger.json. Relevant protocol patches were compared with owning code; unrelated desktop/plugin/docs changes were classified from metadata and changed-file inventory. This is not a full lifetime audit of cc-switch or a full code audit of unrelated patches.

## Changes and preserved behavior

- Merged upstream `0896305eeee89435049575d55a813f72901f703b`. PostgreSQL fastest rankings use DISTINCT ON; SQLite/MySQL retain ROW_NUMBER. Existing daily query APIs and helpers outside generated resolvers are retained. Added a real PostgreSQL parity CI step covering request deduplication and aggregates.
- PostgreSQL rejects JSON U+0000 escapes. Preserve the complete payload in the existing version1 compressed envelope even for small inputs, instead of deleting text. Read envelopes/references after jsonb normalizes key ordering and spaces. Existing compression, checksums, references and external storage remain compatible. A real PostgreSQL test checks exact round trips and duplicate references.
- Merged second-precision analytics across GraphQL and the shared date picker; MiniMax Flash/M3 suggestions preserve saved IDs; Ollama balance API uses the account key with cookie fallback; administrator Claude Code settings retain scope checks and caller model restrictions. Dependency updates retain fork packages and frontend style.
- Native Anthropic between_tools keeps native-only effort, display, block binding and future fields. Split Gemini finish chunks retain tool_calls while preserving existing malformed-tool detection and cumulative usage. Explicit 429 retry configuration extends transient retry with alternatives; quota/local limits remain excluded and retry budgets remain enforced.
- From [cc-switch 96f01af](https://github.com/farion1231/cc-switch/commit/96f01af07fddc80790c2fe4fd80c4a073a06074a), accept Azure resource mismatch errors pointing at the whole input array. Existing one-shot request/destination recovery already detaches protocol item IDs. Keep call IDs, tool arguments/results, visible summaries and prompt cache keys; require complete readable history. Do not copy placeholder replacements or broaden recovery to arbitrary failures. HTTP/WebSocket regression cases cover empty, input and indexed-id parameters, repeated rejection and unchanged first attempts.
- From magpie 13f658a, before Anthropic message_start send SSE comments; afterward keep native ping. This prevents an out-of-order initial event in clients with strict stream ordering. Keep configured heartbeat intervals/timeouts and serialized delivery.

## GPT-6.1 Terra

The [official model page](https://developers.openai.com/api/docs/models/gpt-6.1-terra) and [GPT-6 guide](https://developers.openai.com/api/docs/guides/latest-model#gpt-61-terra) match the existing public-API adaptation: none/low/medium/high/xhigh/max, 1,050,000 context, 922,000 input, 128,000 output, Responses required for tools, and existing Standard/Fast/272K cost tiers. The existing Terra catalog entry is byte-semantically unchanged by upstream sync.

sub2api's new Terra implementation is covered by existing model/capability/pricing behavior. magpie's relisting and raw Codex catalog insights are already covered by on-demand raw catalog fetching and configured hourly channel model sync. API model-card limits and Codex account catalog fields are separate: preserve upstream Codex entries rather than synthesize a public-API context window or ultra capability. No change to disabled auto-sync settings or active default models. Live account availability, paid inference and real cache-hit rates were not probed.

## Verification and release boundary

All tests/builds/lint/format/tidy/generation run only in GitHub CI. The upstream Claude Code settings browser test is explicitly included via playwright.mocked.config.ts, with test-owned API mocks and no backend initialization; the existing interactions harness remains enabled. The release is fast-forward of the exact passing commit to dev, annotated tag, monitor until GoReleaser starts. Production binaries, processes, database/configuration and paid inference are outside this delivery. Check DELIVERY.md for final run URLs and release identity.
