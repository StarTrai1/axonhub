# Upstream, peer gateways and agent compaction — 2026-10-28

Base: dev `f198fe95b0652727a6b3e1cbb6b2c6d0b03e9af0` (PR18). Branch: `sync/upstream-peers-20261028`. PR: https://github.com/StarTrai1/axonhub/pull/19. Final CI and release identity are recorded separately in DELIVERY.md/DELIVERY.json, so the release can use the exact tested commit.

## Frozen incremental scope

Previous boundaries were recovered from `../2026-10-27-upstream-ccswitch/peers-ledger.json`, frozen on October27 around12:24UTC. This round has302 records including21 merges: upstream12, sub2api38, CLIProxyAPI21, OmniProxy0, codex2api4, gpt-load3, new-api9, magpie210, cc-switch5. Each SHA has a decision, reason, evidence and review basis in peers-ledger.json and PEERS.md. Relevant protocol patches were compared with owning code; unrelated desktop, UI, documentation and unsupported subsystems were classified from titles and changed-file inventories. No remote heads were refreshed during review; no full patch audit of unrelated changes is claimed.

## Changes

- Merged upstream unstable `6c181e01fc2a49a536bed09bc96e16599ac682ad` (12 dependency commits). Resolved frontend package/lock conflicts while preserving fork sanitization dependencies, newer fork tooling and removed development packages. Retained go-sse and gin-sse fork replacements.
- From sub2api0a2a29b32e, update production/root, LLM and schema graphs to x/net0.62.0. [GO-2026-6860](https://pkg.go.dev/vuln/GO-2026-6860) describes quadratic allocations/copies in HTML text-node coalescing. CI owns transitive dependency reconciliation and vulnerability checks.
- From cc-switch32db0e5763, keep MiniMax weekly status2 visible as exhausted, including reset and boosted total. It is unavailable for routing even when the reported boosted ratio is below1. Status3 still means no weekly limit.
- From CLIProxyAPIad8bdd3f20, bound zstd decoder memory/window to64MiB with one worker, in addition to the existing decompressed request-body limit.
- From new-api01da3a55f8, cache retry regex compilation in a bounded1024-entry LRU. Keep standard regexp substring matching, literal rules and invalid-pattern behavior unchanged.
- From sub2api917dbdc705, retain fileData image outputs in Gemini chat/stream and image endpoint conversion; ignore empty image parts and avoid counting the same inline/file part twice. Provider token accounting is retained; peer model-name billing heuristics are not adopted.
- From sub2apiba5b218794, normalize developer roles after pass-through for mapped DeepSeek models on OpenCode Go/exact opencode.ai destinations. Other models/destinations and original request bodies retain their roles.
- From magpied8938d2f00, remove only null content on reasoning items sent to the exact api.x.ai Responses destination. Preserve opaque checkpoints, IDs, call data, numeric precision and prompt_cache_key. Existing schema repair already covers nested required:null without modifying business values. Do not relocate later system messages into an earlier global prompt.

## Agent requests70000 and70002: evidence before repair

A Windows-native online SQLite backup used a read-only source and passed quick_check. Both requests failed before any upstream execution, with compaction_source_invalid and the same retained-window mismatch message. The authenticated checkpoint source contains224 items. Retained prefix alignment succeeds and tool pairs are complete. Each current request carries the same agents.spawn_agent call at input263, with encrypted_function_args=[message]; it becomes item480 after expansion. This is the sole identified completeness blocker.

The message uses the gateway's axonhub-agent-v1 capsule. Authentication with the original API key/project scope recovered the complete4681-byte message with matching hashes for both requests. The deployed EXE metadata names f198fe95 and the October27 release tag, with vcs.modified=true; the version marker alone is not proof of an unmodified build. Shape, authentication and boundary evidence is in request-evidence.json; prompts, secrets, real ciphertext and tool outputs are not committed.

The existing outbound recovery runs after local checkpoint preparation. Therefore the completeness check sees encrypted_function_args before the gateway opens its own authenticated message, and reports a misleading source mismatch. Share that exact recovery routine with pre-routing checkpoint migration; recover copies of both the current window and retained source before the strict check. Unknown provider ciphertext, forged capsules, wrong project/API key, unsupported encrypted fields and orphan tool outputs remain rejected. Original request bodies and immutable archives remain sealed. No history is truncated, guessed or replaced with a placeholder.

Regression cases cover current-window and archived agent messages, first compaction without an older checkpoint, exact plaintext/call ID/numeric preservation, unchanged cache key and effort, and all rejection cases above. Existing pipeline tests retain one-generation summary behavior, retries, owner isolation and HTTP/WebSocket continuation coverage.

## Existing coverage and deliberate exclusions

The [official GPT-6 guide](https://developers.openai.com/api/docs/guides/latest-model) confirms the already implemented Terra/Sol6.1 sampling and Responses tool requirements. Raw Codex catalogs preserve new fields. Transient turn_rate_limit_exceeded already maps to429 and uses configured retries/Retry-After without overriding real quota exhaustion. Existing implementations cover DeepSeek cache reads, malformed Gemini tool calls, Anthropic safeguards, model singleflight rechecks, exact SSE data completion and request-scoped WebSocket failure handling.

No peer private signature key-ID heuristics, lossy fallback summaries, additional paid retries, persistent thinking/cache rewrites, external scheduler admission bypass, universal tool-capability claims or desktop/plugin lifecycle were imported. Vertex partialArgs is an optional protocol extension requiring separate verified integration; converted requests do not enable streaming function arguments in this round.

## Validation and delivery boundary

All tests, compilation, lint, formatting, lockfile/tidy and generation run only in GitHub CI. Local checks are source review and git diff --check. Finish every required CI check on the final SHA, fast-forward that SHA to dev, create the annotated unstable tag and stop monitoring when Run GoReleaser starts. No production executable, process, database or configuration change, and no paid inference probe, is part of this delivery.
