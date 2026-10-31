# Responses continuation: session loss carried by opaque history

PR: https://github.com/StarTrai1/axonhub/pull/23
Base: c518d9b9cf1fc2e890bad1726871db72a4de0f0e (PR22).

## Incident evidence

All 16 requests #72049–#72064 have two executions on agent channel40, gpt-6-astra. All32 return HTTP409 with session binding invalidated. The first retry changes the transport alias; the next client request reuses the persisted alias before renewing again. Session/thread/conversation/cache identities are coherent in headers and body, and previous_response_id is absent upstream. The earlier fix was running but could not resolve the history-carried binding.

Both threads contain a native opaque checkpoint (80 and104 input items); metadata reports warm_fork or cold_resume. Nearby fresh requests #72066–#72081 include successes on the same channel/model, plus distinct429 capacity errors. Later resumed #72083/#72086–#72096 reproduce409. These observations alone did not establish whether metadata, IDs or ciphertext caused the failure.

## Authorized live differential diagnosis

Three minimal metadata controls all returned HTTP200/completed: fresh metadata, retained context_window_id, and retained original turn metadata with fresh session/thread/window identities. Recorded usage totaled70 tokens. Ten429 responses and one transport failure were excluded per the user's explicit clarification and retried; the missing response cannot establish billing for that transport failure.

The user separately authorized three opaque-state controls against the original destination, then one additional ID control. No original plaintext conversation or tools were sent, and output was capped at128 tokens per request:

| Input accompanying the synthetic OK prompt | Result |
| --- | --- |
| Original native checkpoint with ID | 400, encrypted-content verification rejection |
| Same checkpoint without optional ID | 400, encrypted-content verification rejection |
| One original encrypted reasoning item with ID | 409, session_binding_invalidated |
| Same reasoning ciphertext without optional ID | 409, session_binding_invalidated |

The last two cases reproduce the session failure independently of transport IDs, turn metadata and checkpoint. Removing optional item IDs is insufficient. Checkpoint portability is a second, separately rejected condition. Provider internal routing/decryption implementation remains unobserved. Probe evidence contains only statuses, sizes, hashes and usage; credentials and opaque payloads are excluded.

## Repair

After one successfully persisted transport renewal is itself explicitly rejected, schedule at most one reasoning recovery for complete materialized history. Reuse existing summary recovery and success-confirmed item hashes scoped to owner, route, credential, model, thread and window. Preserve readable reasoning summaries, message content, tool arguments/results, call_id relationships, numeric precision and native checkpoints. No provider host or numeric diagnostic code is hardcoded.

The fallback does remove unusable encrypted reasoning from that outbound attempt; its hidden content cannot be recovered from ciphertext. Immutable original records remain intact. A native checkpoint remains byte-preserved until its own explicit rejection; then the existing authenticated retained-source migration applies. Missing or mismatched retained source fails closed rather than discarding compacted context. This can involve the existing paid summary generation path, not an added diagnostic call. All stages share the configured continuation retry budget. Failed fallbacks do not create learned corrections, and fresh target reasoning remains native.

TypeSafe/Jev selected this bounded recovery boundary with confidence1.0. Its judgment is not provider evidence or execution authorization.

## Validation and delivery

GitHub CI only. Regressions cover HTTP and WebSocket continuation, in-band errors, repeated rejection after renewal, preserved native checkpoints followed by explicit decryption rejection, full and delta history, retry exhaustion, success-only remembered corrections, and newly generated reasoning outside the affected hashes. Packaged client coverage stays at Codex0.166.0 / gpt-6.1-sol. Exact-SHA workflow and delivery results belong in the final delivery receipt.

YCE exit0, analysis/edit gate true; result /tmp/yce-results/yce-search-20261031T084709-2396952-38aa66.xml read through terminal EOF (35421bytes, SHA256 a102c86dfafed1cb79e5ca93d14490029cf6009fb3c3d5c8f22a1e096ad353da).

Read-only Windows database access extracted selected diagnostic rows. The31887360-byte temporary selected database was deleted and absence verified. No production configuration, database or process was changed. The production executable has not been replaced by this task. Live synthetic controls establish the failure mechanism, not acceptance of the original long conversation after deployment. No local build/test/formatter ran.

Read-only source availability check: both #72049 and #72061 have non-deleted owner-scoped window-v2 checkpoint aliases in systems. Presence is verified; this does not attest every encrypted manifest/block or source-splice contract. Runtime retains its authenticated validation and fail-closed behavior.
