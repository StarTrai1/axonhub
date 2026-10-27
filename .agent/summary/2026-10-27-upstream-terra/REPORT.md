# GPT-6.1 Terra, upstream and continuation recovery

## Changes

- Merged upstream unstable 532a80328d360bcddc517a99fdf1ede7586fe432. Shared dashboard aggregates/cache, SQLite window ranking and deferred off-screen widgets are new. Retained fork motion/react and daily query replacement. Moved helper functions to dashboard_queries.go so GraphQL generation preserves them.
- Removed the Azure history compatibility channel UI, GraphQL input/output and form/query fields. Stored legacy values are ignored. An explicit 400 Azure resource mismatch on a third-party Codex route permits one recovery of the current request on its exact channel/endpoint/model/credential. Full history and native checkpoints are materialized through existing owner-checked retained history. New requests keep native history; old successful resource corrections cannot become a learned stripping policy.
- Fixed #68865/#68868/#68869: locally produced reasoning uses rs_ IDs across HTTP and every SSE event. Legacy materialized reasoning item_* IDs are removed before relay HTTP/WebSocket transport. Native rs_ identities, ciphertext, checkpoints, referenced items, summaries, tool data and prompt_cache_key stay intact.
- Added gpt-6.1-terra and local -fast alias. Preserve none; minimal maps to low; reasoning/sampling restrictions follow effective configuration updates. Chat tools require Responses. Both catalogs contain the six effort levels, correct limits, official Standard/Fast price tiers and release/cutoff dates. No Terra Ultrafast inferred.
- Adapted magpie image-detail compatibility: original becomes high for Chat, while Responses image detail is retained. Existing image/request/ciphertext payloads remain typed and opaque.

## Official evidence

- https://developers.openai.com/api/docs/models/gpt-6.1-terra
- https://developers.openai.com/api/docs/guides/latest-model#gpt-61-terra
- https://developers.openai.com/api/docs/pricing

Published October 26. Context 1,050,000, max input 922,000, max output 128,000; none/low/medium/high/xhigh/max. Standard USD/MTok input 1, cache-read .05, cache-write 1.25 at 30m, output 5. Above 272K input, whole-request input/cache 2x and output 1.5x. Fast 2x Standard. 12h cache-write base 2.50 is documented; the existing channel price configuration must match non-default cache retention. No live paid model-availability probe.

## Frozen review

199 records: 1 upstream, 46 existing peers, 152 magpie; 5 merge records reuse component decisions. sub2api and new-api unchanged. PEERS.md and peers-ledger.json contain exact boundaries, individual decisions, changed-path evidence and review depth. Unrelated desktop/plugin/UI changes were classified by metadata and file inventory, not claimed as full code audits or AxonHub supersets. Magpie initial audit covers pinned gateway architecture plus the same incremental date window, not its lifetime history.

## Request evidence

Read-only Windows SQLite backup, quick_check=ok. #68865 execution201837; #68868 transient502 execution201846 followed by invalid_id_prefix400 execution201847; #68869 execution201848. Each carries 26 locally generated item_* reasoning IDs without ciphertext and 5 native encrypted rs_* items. First invalid item index32. Source prompts/credentials are not committed. Request-scope compatibility and new reasoning identities have separate regressions.

## Validation

All execution is in GitHub CI. Pending final exact-SHA result; WORK.md records CI corrections, delivery receipt records final CI and release-start evidence. No production database/configuration edits, installation, service restart, release-asset verification or paid inference.
