# Upstream and peer integration — 2026-11-05

The previous review cutoff is the committed November 3 ledger. Current base is `cff18fb9`, retaining the November 4 canonical checkpoint ancestry repair. The fetched upstream/unstable head is still `8a9df52283d9d9c3909c839267ce890a2913231e`, already an ancestor of dev; merge reports `Already up to date`.

## Frozen incremental review

| Repository | New records | Frozen head |
| --- | ---: | --- |
| upstream/unstable | 0 | 8a9df52283d9 |
| sub2api | 19 | 876b96d20485 |
| CLIProxyAPI | 28 | 78a9cc9aaa8a |
| OmniProxy | 0 | 4bcee95bf183 |
| codex2api | 60 | a4bbb455ba7a |
| gpt-load | 3 | 5f345352d119 |
| new-api | 5 | 706144769e5b |
| magpie | 183 | 83137bcbe971 |
| cc-switch | 9 | ca58c0313360 |

All 307 new records were screened by title, parents and changed-file inventory; selected runtime patches were inspected and compared with Axon source. The ledger records that depth per commit, including partial inspection, instead of claiming every patch was fully reviewed. Heads stayed fixed and old decided commits were not reopened.

## Adopted change

[new-api ba5f736779](https://github.com/QuantumNous/new-api/commit/ba5f7367797566175a6cd97a748d430af3602a01) exposes a matching Axon defect: the Gemini embedding response structs omit usageMetadata, so reported usage is lost during conversion. [Google's EmbeddingUsageMetadata contract](https://aistudio.google.com/api-reference/embeddings#Resource:EmbeddingUsageMetadata) defines promptTokenCount and promptTokenDetails (singular Token, unlike generation usage).

Axon now reads usage on single and batch embedding responses into unified prompt/total usage and modality details. Reported batch usage is counted once; generation-only output counts are not imported. Missing/null/invalid negative counts remain unknown, explicit zero remains known, and null/invalid modality entries cannot panic the parser. No estimated token counts, new prices or additional requests are introduced. Existing OpenAI embedding response and gateway usage paths consume the unified usage.

The wire-level fixture covers both response shapes and ten usage cases each, including reported multimodal counts, missing/null/zero/negative values, detail-only metadata and stray generation fields. It checks embedding values, unified usage and the resulting OpenAI wire response.

## Existing coverage and boundaries

- CLIProxyAPI's inline tool-result image nesting is already covered by Gemini 3+ media placement, older-model fallback and parallel result grouping. Existing Gemini refusal/malformed-tool handling is retained.
- Magpie's GPT effort-to-Messages behavior is covered by Axon's platform-based output_config conversion. Responses errors already preserve numeric status/status_code with precedence tests. SSE parsing already operates on complete events. These do not imply parity with all Magpie heuristics.
- OAuth rejection handling already binds failures to the exact refresh credential/client. x/net v0.62.0 security updates are already present.
- Keep native item IDs, encrypted history, system instructions and citations. Do not import speculative rewriting, a new classifier/repetition cutoff, after-output failover, or peer cooldown/region heuristics.
- Explicitly defer cohesive Argon continuation/media support, beta mid-conversation effort conversion, expanded cached modality accounting and Codex max_output_tokens policy changes until their endpoint/roundtrip contract is established. They are not labeled covered. The public Responses API does not establish the private Codex backend contract.
- Desktop/Tauri/process/plugin installation, local transcript migration, new administrative registration policy and peer-specific DB/cache changes have no directly portable implementation. Keep the existing frontend and channel/session ownership boundaries.

## Validation and delivery

All tests, builds, lint, formatting, dependency resolution and source generation run in GitHub Actions. The final SHA must pass Test, Build, Lint, Docker build (PR) and Govulncheck. Backend/database, LLM, Windows, concurrency and the existing packaged Codex 0.166.0/gpt-6.1-sol loopback matrix remain in force. CI artifact corrections are applied and revalidated if needed.

CI also exposed a pre-existing session-binding test race: the test started a follow-up request before the background pass-through observer recorded successful reasoning recovery. Synchronize that fixture on the learned rule, as the existing reasoning/agent fixtures already do, before asserting old-ciphertext avoidance and fresh-item preservation. This changes only test synchronization, not the recovery runtime or retry policy.

Exact final CI/ref results and the Run GoReleaser start receipt belong in ignored local DELIVERY.md/DELIVERY.json so no post-validation commit changes the released SHA. No production installation or live provider acceptance is inferred.
