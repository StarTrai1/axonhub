# Upstream and peer integration — 2026-11-03

The fork merges34 upstream commits through `8a9df522` and adapts one confirmed peer conversion fix. The previous cutoff is the committed November1 ledger; `peers-ledger.json` records747 incremental records, including713 peers, with explicit inspection depth and decisions.

## Upstream behavior and overlap resolution

- Provider quota transition webhooks: warning, exhausted and recovered events, including last-known state across unknown checks and reset/reload races. Preserve the fork's explicit resetID API and adapt upstream tests accordingly.
- Request overrides can delete fields across arrays with `[*]`; existing explicit operator configuration controls this operation.
- Preserve explicit image response format; accumulate multipart Gemini thought summaries; accept Gemini field aliases; retain AI SDK tool call IDs across index-only deltas; persist repaired Anthropic tool input in stream aggregation; use short request-local IDs for the primary safeguard reviewer without changing client identities.
- Model price import gains an exchange-rate factor; model catalog and dependency updates are merged while retaining frontend style and fork dependency exclusions/pins.
- Gemini blocked finish reasons are already covered by the fork, including its malformed-tool error path. Keep the superset and upstream test additions.
- Dashboard helper extraction was already implemented in the fork. Keep dashboard_queries.go and existing helpers; remove duplicate methods introduced by automatic merging. The dedicated generation-verification CI job already covers upstream's newly added generation check.
- Keep existing GPT-6.1 Terra and GPT-6 Luna entries in both synchronized catalogs. Do not reintroduce removed direct framer-motion/lobehub UI dependencies; retain the sanitizer and newer Vite pin. GitHub CI generates the final lockfile.

## Peer adaptation

CLIProxyAPI [ec06c3cb8b](https://github.com/router-for-me/CLIProxyAPI/commit/ec06c3cb8bac53c7577772fd6272db8c70a2a93d) identifies string-encoded integer constraints in Google's protobuf Schema. Google documents `minItems`, `maxItems`, `minLength`, `maxLength`, `minProperties` and `maxProperties` as string fields in its [Schema interface](https://googleapis.github.io/js-genai/release_docs/interfaces/types.Schema.html).

Axon now converts canonical nonnegative int64 strings to JSON numbers, and upper-case schema types to lower case, when native Gemini `parameters` or `responseSchema` enter the unified standard JSON Schema path. Traversal follows schema positions only; default/example/enum/vendor data, invalid/noncanonical values and exact integer precision are preserved. Standard JSON Schema carriers retain their existing behavior. Regression fixtures cover the constraints, nesting, literal data and Gemini-to-Chat/Anthropic wire output.

## Frozen ranges

| Repository | New records | Frozen head |
| --- | ---: | --- |
| upstream/unstable |34|8a9df52283d9|
| sub2api |4|4a4a83ee5ec8|
| CLIProxyAPI |60|6e6afab99a19|
| OmniProxy |0|4bcee95bf183|
| codex2api |17|231c597dd029|
| gpt-load |8|43b2554fdde9|
| new-api |6|9b473a3fe4fe|
| magpie |575|40a287b21766|
| cc-switch |43|7afceec14b97|

All entries were screened by title/parents/file inventory; only selected runtime patches were inspected. The ledger explicitly distinguishes these depths. Existing max reasoning effort (including Terra), xAI hosted tool-choice adaptation, xAI detailed usage, namespace conversion, ordered native additional_tools and x/net0.62 security update cover several peer candidates. Partial coverage is labeled as such.

No peer-wide JSON-engine migration, new Muse/Devin subscription subsystem, speculative native item/ciphertext rewriting, silent history/media deletion, desktop security policy or automatic post-output failover is imported. Peer policy differences and candidates lacking an Axon failure/contract are recorded individually rather than asserted equivalent.

## Validation and delivery

Use GitHub Test, Build, Lint, Docker build(PR) and Govulncheck on one final SHA. This includes frontend checks/build after upstream dependencies/UI changes, backend/database, Windows, concurrency, LLM and packaged Codex0.166.0/gpt-6.1-sol loopback checks. Module tidy, formatting and generation are performed in CI; downloaded artifact patches are applied to the branch and revalidated.

Exact final outcomes/ref verification/release-start receipt belong in local ignored DELIVERY.md/DELIVERY.json, so no post-validation documentation commit changes the delivered SHA. Passing fixtures do not imply production installation or real-provider acceptance.
