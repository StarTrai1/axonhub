# Checkpoint continuity and the conversation archive

AxonHub now continues from the client's current checkpoint and the items after it. The former automatic unified-history assembler, per-segment model summaries, accepted-view rewriting and synthetic compaction executor have been removed. A successful compact operation must produce a usable compact window; the gateway no longer substitutes a serialized original transcript for a summary.

## Native and portable checkpoints

A native checkpoint remains opaque and belongs to the upstream resource that created it. AxonHub preserves its contents and the client's retained messages. Normal continuation does not expand earlier checkpoints or launch summary requests.

When a selected destination explicitly rejects a native checkpoint, AxonHub makes one checkpoint recovery instead of first resending the same ciphertext with optional item IDs removed. It uses the checkpoint's retained source to generate one bounded portable summary, saves the result under the authenticated project/API key and checkpoint identity, and keeps the current retained messages and post-checkpoint tool interactions. Reconnects and process restarts reuse that saved summary.

The rejection also establishes a portable-compaction contract for that thread, model and channel configuration. Subsequent compact requests use the current window and produce a portable summary through an ordinary Responses call. They do not retry native compaction and then reconstruct the original transcript again. A new thread or an operator's channel configuration change establishes a new contract. Explicit local-bridge channels also use this single-summary path.

Portable checkpoints use an authenticated AxonHub capsule. When sent through this AxonHub installation, it becomes a normal summary message, so the upstream does not need to understand AxonHub's format. The capsule remains bound to its project/API key and installation secret; it is not a universally readable provider-native ciphertext. Do not send it directly to an unrelated gateway.

## Compaction boundaries

- The client owns when compaction is requested. AxonHub serves that request or the first explicit migration of an incompatible native checkpoint; it does not summarize each ordinary inference request.
- The canonical window is the source of the next checkpoint. A previous portable summary stays a summary instead of being replaced with its old archive.
- Standalone `/responses/compact` output remains the complete next window. Native output and opaque items are preserved as returned.
- A portable summary targets at most 4,096 tokens and must fit a 64 KiB text limit before publication. Oversized or incomplete summaries fail without replacing the previous context. Model input admission separately reserves output space.
- A migration may read authenticated legacy source records, but it does not fan out into segment summaries. Missing, divergent or oversized sources produce a specific error before publishing a replacement.

## Independent archive

Original request records and checkpoint source windows remain encrypted and scoped to the authenticated project/API key. Old immutable records are retained; upgrading does not delete the database or rewrite historical ciphertext.

The archive is for diagnostics, reconnect recovery and explicit migration. It is not automatically replayed after every compaction. New checkpoint sources store their actual current window in a versioned namespace; readers retain compatibility with older source records.

A summary preserves useful task state, not every original token or a provider's hidden reasoning. Complete archival storage does not prove lossless model memory. When a native source is unavailable, continue through a resource that can read it or supply an explicit handoff; removing opaque state silently would lose context.

## Retry behavior

Automatic relay session rotation and its resend path have been removed. Known model-capacity failures stop same-channel retries before compatibility overrides can request another attempt. Independent permitted channels remain eligible. Checkpoint recovery changes the request once; it does not retry an unchanged rejected checkpoint or restore a growing full transcript to make a request appear successful.

The model catalog parser and Codex Lite `additional_tools` to Responses `tools` normalization operate independently of checkpoint continuity.
