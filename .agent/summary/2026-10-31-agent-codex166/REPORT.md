# Agent continuation and Codex 0.166.0

Base: `3148997384731606a23cbd24bdb16ee512c511db` (PR21). User retained CI-only validation and exact-SHA dev/tag/GoReleaser-start delivery.

## Observed requests

All 39 requests #71693–#71731 reached channel 40 (agent), one execution each, HTTP409 with an explicit invalidated session/model binding and instruction to use a new session ID. Provider diagnostic code30013 appears in the saved message; raw response bodies/chunks were not retained. No usage rows. #71710–#71721 used0.164.0-alpha.11.1 and failed identically; the incident is not established as a0.166 regression. The first thread has no checkpoint; the second has one native checkpoint. #71722 previous_response_id was removed before upstream execution. Details in request-evidence.json; private bodies remain outside the repository.

## General lifecycle

The user rejected an error-specific patch. Manage logical conversations independently from upstream transport sessions. Derive the initial upstream identity from authenticated owner, physical destination, final credential, model and original session/thread. Unchanged scopes reuse their identity across requests and restarts; model/channel/account changes cannot inherit another binding. Only explicit session-expired/invalidated signals may renew it, using a durable compare-and-swap alias. Concurrent stale failures reuse the winner. One renewal per scope/request, inside the existing retry budget. No numeric vendor code in product logic.

History, reasoning ciphertext, native checkpoints, tools, numeric precision and turn attribution remain intact. Archive and delta identities remain logical; actual outbound request records show the transport identity. Server-only legacy references cannot authorize renewal. Ordinary conflict, overload, quota, auth and policy failures do not trigger renewal. Non-OAuth Responses relays are supported; native OAuth and connection-owned upstream WebSocket sessions keep their existing lifecycle. Initial namespace migration can cause a cold cache once; provider acceptance and real cached_tokens are not proven by mocks.

Current WebSocket-frame identity overrides stale prewarm handshake metadata before channel header overrides. Body contents remain untouched.

## Validation

GitHub CI only: new HTTP/WS handler full-history/checkpoint/delta/model-switch regressions; durable restart and late-rejection handling; in-band and HTTP failure recovery; retry budgets and error boundaries; frame metadata precedence. Packaged CLI0.166.0 is the only real-client CI version, using gpt-6.1-sol. Future upgrades replace this entry rather than accumulate versions. Guardian response metadata and approval catalog extensions tested for preservation. Final CI and release receipts are recorded separately after the exact commit passes.
