# Azure history compatibility — 2026-10-26

## Evidence

Read-only native Windows SQLite online backup, quick_check=ok, max request 68395. Seven screenshot requests belong to Codex channel 40. Details are sanitized in request-evidence.json; no prompts or credentials are committed.

- #68359: 9 upstream attempts, one Azure resource mismatch and eight token-rate 429s. ID detachment did run.
- #68365: 8 attempts, one 502, two resource 400s, five token-rate 429s.
- #68366: 11 attempts, two resource 400s, eight token-rate 429s, final context cancellation. First resource repair removed 59 message/tool IDs; second removed two encrypted reasoning items. All tool pairs remained.
- #68367–#68369: one resource rejection followed by a canceled retry. #68370: cancellation without a recorded HTTP response. Request UI cancellation alone is not proof of a resource mismatch.

These traces establish that recovery/retries already execute. Failed turns never produce successful correction pairs, so the evidence-based proactive policy is not learned. The requested operator option avoids the recurring staged resource rejections. It cannot remove token-rate limits.

## Behavior

Add `policies.azureHistoryCompatibility`, default false, shown as a compact checkbox beside existing Codex policies only for third-party authentication. Include create/edit/duplicate defaults, submit/reset, all policy-returning GraphQL operations, schema input/output and stored-channel/cache round trips. Existing style, dark theme, Chinese/English labels and mobile wrapping are retained.

When enabled on a non-OAuth Codex channel, materialize owner-scoped continuation/checkpoint history first, then remove protocol item IDs and encrypted reasoning, preserving visible reasoning summaries, messages, exact tool arguments/results, call_id associations and omitted/empty/custom prompt_cache_key. Remove reasoning.encrypted_content include and stale turn-state header. A transport-only flag prevents WebSocket incremental optimization from reintroducing previous_response_id. Native downstream WebSocket turns retain complete snapshots for subsequent recovery.

Native checkpoints use the existing authenticated retained-history summary mechanism, even with native compaction selected. It can issue a summary request at runtime and reuse the summary on later turns. Missing or opaque-only history produces a clear error instead of silently discarding messages, references or tools. No live inference was run in this task.

## Reference and adaptation

Reviewed https://github.com/Lester0504/codex-api-compat at `fce8f0217307638a603a1c4db3a5cb38255edc5f`, notably `sanitizeRequestBody` and its tests. Its recursive removal can touch business JSON fields named id/encrypted_content and drop unresolved item references. This implementation restricts rewrites to known protocol structures and requires readable complete history. Its unlimited fixed-interval retry loop is not transplanted; AxonHub retains configured bounded retries, cancellation and pre-output commitment semantics.

## Validation and delivery

All build/test/lint/format/tidy/code generation runs only in GitHub CI. Added pipeline first-attempt tests, opaque-history guards, storage/cache enable/disable, checkpoint summary reuse, real WebSocket full-history transport and three-width browser save/reopen/disable/a11y coverage. Exact-SHA CI checks, annotated release tag and GoReleaser start are recorded in DELIVERY.md/json. No production configuration/database edits, installation, restart or paid probe.

## Key-channel continuation repair (#68661–#68662)

Both requests failed once on channel 39 with invalid_id_prefix at input[35].id. Each carries 29 custom_tool_call items whose id equals call_id and starts with call_. Responses conversion used the correlation ID as output-item identity, losing the native ctc_ ID. Preserve native identity in transformer metadata for HTTP and streaming responses, including delta/done/completed events. Persisted malformed calls are repaired at final transport preparation by removing only their optional item ID; call_id and all input/output bytes remain intact. Native IDs, unknown shapes and item-reference targets remain unchanged. This repair is independent of the optional Azure history policy.

CI follow-up also restores the shared guard for explicit encrypted/reference/file protocol parts in tool-output arrays while permitting arbitrary business objects and fields. The pending frontend contract correction distinguishes full policy snapshots from auto-disable-only selections.
