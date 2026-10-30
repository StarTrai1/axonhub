# Upstream and peer integration — 2026-10-30

Base: `178757cc2326a481c7c13b15618f8aac70c5a526` (PR20).

Frozen scope: 1 upstream commit through `b941556253f8be592c25d046a01247706f86885a`, and 216 incremental peer records. cc-switch 27, magpie 161, new-api 18, sub2api 10. CLIProxyAPI, OmniProxy, codex2api and gpt-load have no new commits. Cutoffs are the previous SHA heads in the 2026-10-29 ledger; no repeated review or refreshed heads. PEERS.md / peers-ledger.json distinguish metadata screening from selected runtime patch inspection.

## Changes

- Merge upstream restore-upload fix: 128 MiB multipart cap, feed the upload reader directly into restore, reject trailing JSON. Existing default transport used 32 MiB. This bounds input size, not total restore memory. Upstream's above-32-MiB multipart test retained.
- Adapt cc-switch `9941a6455e` and magpie `fb2e11e288`: remember explicit OAuth refresh rejection per refresh-token hash and client ID within the provider. Only recognized permanent codes on HTTP 400/401 qualify. Do not retain an untrusted error body in the remembered error; preserve status for channel error classification. Repeated foreground/background refresh calls do not hit the same rejected grant again. Successful authorization exchange clears the marker; replacement grants are not poisoned by a late old rejection. Transient HTTP errors and malformed proxy responses stay retryable.
- Retain existing Gemini signature and imageConfig support, bounded zstd decoding, Vertex Anthropic transport, native Responses metadata and WebSocket continuation/auth capabilities. Existing security dependency versions already cover selected peer updates. No desktop account-file or paid reset-credit operations introduced.

## Validation and delivery

Tests/build/lint/format/generation are GitHub CI only. OAuth tests cover explicit string/nested/top-level codes, temporary failures, repeated Get/background access, replacement credentials and authorization recovery using loopback HTTP. CI results and delivery are recorded after the exact SHA passes. Fast-forward that SHA to dev, create an annotated unstable tag, and stop monitoring when Run GoReleaser starts.
