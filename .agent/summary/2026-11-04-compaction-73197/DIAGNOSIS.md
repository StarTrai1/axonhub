# Rejected checkpoint recovery: #73197–#73198, #73209–#73213

## Evidence boundary

Read-only extraction from the running Windows SQLite database. The source connection used `mode=ro` and `PRAGMA query_only=ON`; selected records were copied to private temporary databases. Production EXE build metadata identifies `5d57cda871e0f7b7f160ae541283d8b52c3b25bc`, matching the inspected baseline. No production configuration, history, process or binary was changed. No provider inference was sent for this diagnosis.

Stored payloads were decoded according to `biz/payload_codec.go`, verifying their size and SHA-256. Retained windows were authenticated with the implementation's AES-GCM associated data and checked against manifest and block digests. Only structural facts, hashes, IDs and errors are exported in `request-evidence.json`; private transcripts, ciphertext and credentials stay outside Git.

## First failure: an intact window depends unnecessarily on a six-generation archive

#73197 and #73209 reached channels 9 and 39 respectively. Executions #208362 and #208373 each rejected the exact current native compaction item. The gateway then returned `compaction_history_unavailable`. #73198 failed during preparation, with no execution. These belong to trace 9714.

The latest checkpoint came from completed request #73130 on channel 9. Its authenticated `window-v2` source contains **302 items**, one native ancestor and a terminal compaction trigger, with **no previous_response_id**. The immediate ancestor was successfully consumed in stored completed executions on channels 39 and 9. Its existence and readability are independently evidenced, not inferred from ciphertext formatting.

The recovery's eager `restoreCheckpointWindow` recursively unfolds six older generations. The sixth is a legacy source capsule containing two items and a connection-local `previous_response_id`. A live owner-scoped lookup found no corresponding request row; response and wire aliases for channels 0, 9, 39 and 73 were absent. The error therefore describes an old archival coverage gap, not a missing latest window.

The trace also contains one completed channel-73 request (#73120) and subsequent failed channel-73 attempts. The database establishes the recorded channel sequence; it cannot identify any private account rotation inside an upstream relay.

## Second failure: migration expands three generations before asking for a summary

#73210–#73213 belong to a different trace, 9715. #73210 first tried channel 9 then 39; both rejected its current native checkpoint. The recovery generated request #73211. Later #73212 generated #73213.

The latest checkpoint was produced by completed request #73054 on channel 9. Its authenticated source contains **343 items**, including one native ancestor, and no unresolved predecessor. That ancestor has many completed channel-9 reads. The full recovery expanded the three stored source windows (343, 312, 288 items), converted their portable records, and submitted a **3,693,640-byte, 856-item** summary request. Executions #208376 and #208377 returned explicit context overflow. The 174-byte execution `request_body` is a lossless reference to that large request, not the actual upstream input size.

This proves the eager expansion and the provider's rejection. It does not establish an exact token count or prove a fresh live provider will accept the smaller canonical source.

## Recovery contract

Preserve the immediate authenticated source window for the first summary attempt, including its native ancestor. A rejection of the current checkpoint does not establish that every older native checkpoint is unreadable. Open locally sealed state under its owner and preserve existing prompt protection. If the selected destination explicitly rejects the precise ancestor in the summary request before generation starts, splice only that ancestor's authenticated immediate source and retry the resulting window. Stop on unavailable or mismatched source, cycles/depth limits, cancellation, unrelated errors or partial output.

There is no separate ancestor-summary generation, arbitrary truncation, image deletion, synthetic successful checkpoint, model-limit inflation or route change. Only one completed summary is published and reused after restart. Existing immutable source records remain unchanged. Ordinary local compaction policy remains separate from rejection-triggered migration.

Jev was consulted for the consequential contract choice using only structural evidence and constraints: `canonical_first`, confidence **0.99**. It was not used as evidence of runtime correctness or authorization. The implementation and CI regression results determine those claims.

## Validation

The tests-only commit `bbdb09dcc1c9e6e269e3a989f8827f46e5d88c43` is submitted first to GitHub CI to demonstrate the old behavior. Synthetic cases exercise HTTP/WS continuation, missing archival predecessor, an upstream size limit, exact one-layer rejection recovery and restart reuse. The final change also covers rejected-but-missing source, unrelated errors, cycles and partial summaries. CI results and the exact delivered SHA are recorded separately after completion.
