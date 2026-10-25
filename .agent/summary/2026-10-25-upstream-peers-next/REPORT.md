# Upstream and peer integration — next pass, 2026-10-25

Start from PR14 / dev `690ef4a5`, retain the earlier Codex 0.164 metadata compatibility, keepalive/native probe request repairs and bounded upstream capacity retries. Merge upstream/unstable through `66ad2fc37ce0f7683a52a9238e8b38fc17e7ccef`.

The user cutoff is Beijing October 25 00:07. Prior frozen per-repository heads are the exact ancestry boundary (the previous API reads finished at 00:08:57–00:09:01). This avoids dropping newly merged commits with older author dates. All 5 upstream and 25 peer decisions, including 3 merge duplicates, are recorded in PEERS.md and peers-ledger.json. No second review window is opened.

## Behavior

- Quota grouping/sorting and persistent view controls are merged while preserving the fork paid-reset confirmation. The max retry limit is already 10.
- Project member selection uses debounced server email search, pagination and dialog-scoped autocomplete; resolved selection labels survive empty search pages and dismissal. IME handling stays intact.
- Cache creation input usage aliases are decoded in Chat and Responses and priced once. Existing canonical/legacy field precedence, especially explicit canonical zero, remains unchanged.
- Responses WebSocket control fields must be unique and canonically spelled before lane routing or interrupt queue mutation. Escaped duplicate keys are rejected; nested input/tool JSON is untouched.
- Explicit incomplete interrupted, steered, content_filter and max_messages results finish once even with zero output. Reasons and usage survive HTTP/SSE transformation. Empty completed, max_output_tokens and unknown reasons still use empty-response retry.
- Upstream Go vulnerability scans cover root/llm with the repository Go version and dev branch. Both catalog copies preserve the Chinese structured-output label.

## Boundaries and validation

Sub2api per-image group charging policy and codex2api native Live/device attestation are architecture-specific additions, not existing AxonHub supersets. They are recorded as skipped. Peer version strings alone do not justify impersonating an unvalidated client.

All compilation, tests, lint, formatting, dependency lock/tidy reconciliation and generation run in GitHub Actions. Added regressions cover cache billing quantities, terminal retry attempt counts/usage, control-field ambiguity and real browser selection behavior. Existing CLI 0.162.1/0.163.0/0.164.0 loopback and offline probes remain required.

Delivery follows exact tested SHA → fast-forward dev → annotated tag → stop once Run GoReleaser starts. No local installation, production configuration/database writes, restart or paid inference. Final verification and release links belong to DELIVERY.md/DELIVERY.json.
