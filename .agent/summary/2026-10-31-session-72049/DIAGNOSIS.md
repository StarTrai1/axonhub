# Continued session-binding failure: #72049–#72064

Status: cause not yet isolated; no product patch or release claimed.

All 16 requests have two recorded executions on agent channel 40, gpt-6-astra. All 32 return HTTP 409 with the explicit session binding invalidated message. The first retry changes the alias; subsequent client retries reuse the persisted alias before renewing again. Session/thread/conversation/cache carriers are coherent in headers and body. previous_response_id is absent upstream. This establishes that the previous fix is active and recovery scheduling is working; adding retries or repeating the same fix is not justified.

Both logical threads include a native opaque compaction checkpoint (80 and 104 input items). history_initialization is warm_fork or cold_resume. context_window_id is retained when transport aliases change. These are correlated differences, not proven causes. No checkpoint, ciphertext, history, or permission metadata was modified.

Nearby #72066–#72081 include successful calls to the same channel/model, with fresh metadata and no checkpoint. They also show distinct 429 capacity errors, which must not be relabeled session failures. Later resumed requests #72083/#72086–#72096 reproduce the 409 pattern. Provider source or a routing contract has not been found; the provider public repository contains only a roadmap link.

The exact prior delivery c518d9b9 remains local and remote dev. Windows EXE mtime is 2026-10-31 08:15:33 UTC; running process start was 08:36:26.771 UTC. This is timing evidence, not an executable hash attestation. Observable alias behavior confirms the relevant implementation runs.

Read-only Windows database queries exported only relevant records to a temporary selected database. No production process, configuration or database was modified, no real provider inference was sent, and no local build/test/formatter ran. Private diagnostic payloads and credential-bearing data remain outside the repository.

YCE exit 0 and analysis/edit gate true. Result /tmp/yce-results/yce-search-20261031T084709-2396952-38aa66.xml was read through the terminal EOF sentinel (35421 bytes, SHA256 a102c86dfafed1cb79e5ca93d14490029cf6009fb3c3d5c8f22a1e096ad353da).

TypeSafe/Jev selected preparing a differential diagnostic at confidence 0.99. This does not establish the root cause or authorize live calls.

Prepared /tmp/axonhub-session-72049/probe.py: default prints a redacted plan without network access. Opt-in mode sends at most three requests directly to the original provider with gpt-6-astra, a synthetic OK prompt, fresh transport aliases and max_output_tokens=128 per request. Cases: fresh metadata, retained context_window_id only, retained turn metadata with fresh transport aliases. No native history, checkpoints or tools are sent; no automatic retries; stop on auth, rate limit, server or transport failure. Credentials are read in memory from the existing channel only after opt-in. These probes can isolate metadata routing but cannot by themselves validate checkpoint replay. Runtime response statuses and usage are saved with no content/credentials.

Pending: explicit opt-in for live probes (previous workflow did not authorize production inference), then a supported patch, GitHub CI-only validation, exact-SHA dev/tag and GoReleaser-start delivery if a code change is established.

Temporary selected database (31887360 bytes) deleted after extracting sanitized evidence and a metadata-only private probe template; deletion verified. Probe credentials are loaded only at execution from the live read-only channel record.

Live-probe authorization: user approved the prepared three minimal calls, then explicitly clarified that auth/rate-limit/server errors do not count and requested three successful probes. Continue pending cases after transient errors and keep already-successful results. Fresh metadata case returned HTTP200/completed with 11 input tokens and 16 output tokens; old-context case initially returned429, not a valid differential conclusion.

All three minimal metadata cases returned HTTP200/completed, recorded total usage70 tokens. Transport timeout billing cannot be established from a missing response. These results refute a standalone retained-context-window or turn-metadata cause. Next prepared (not executed) three cases: native checkpoint with id, same checkpoint without optional id, one encrypted reasoning item with id. Each uses fresh synthetic transport metadata and the same OK prompt/output128 cap. They send only opaque state previously sent to this destination, but checkpoint hydration token charges are unknown; they require separate opt-in because initial consent expressly excluded history/checkpoints.

User separately approved the three opaque-state cases. Observed checkpoint-with-ID400 and checkpoint-without-ID400, both ciphertext verification rejection; reasoning-with-ID409/session_binding_invalidated. No usage was returned for these three rejected requests. A single original reasoning item reproduces the session error with wholly new transport metadata; the checkpoint is not required for that409. A prepared follow-up removes only reasoning.id while retaining ciphertext and synthetic input, to isolate stored-item affinity from cryptographic portability. Not yet executed.

Additional separately authorized reasoning-without-ID case returned409/session_binding_invalidated too. Thus optional item ID removal is not a remedy; the opaque reasoning can reproduce binding loss independently of header metadata. Jev engineering review selected staged reasoning recovery with confidence1.0. Implement one reasoning repair only after a successfully persisted transport renewal is itself rejected, guarded by complete history and exact destination/owner/window/ciphertext hashes. Existing compatibility retains readable summaries, message/tool content and checkpoint; existing explicit checkpoint error recovery remains responsible for its separate400. No provider code or host is hardcoded. Pending CI and release.
