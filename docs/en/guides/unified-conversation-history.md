# Unified conversation history (experimental)

This opt-in feature is developed on a separate branch. It stores completed Responses input/output, native compaction sources and local bridge sources as encrypted history independently of request logs. Explicit response/checkpoint references identify branches; no mutable “latest thread” lookup joins unrelated conversations.

```yaml
unified_history:
  enabled: true
  retention_days: 0
```

Alternatively set `AXONHUB_UNIFIED_HISTORY_ENABLED=true`. Configuration is read at startup; apply it through your deployment procedure. Zero retention disables automatic history expiry, so plan disk capacity. A positive value makes records unreadable after that many days without a refreshed reference; scheduled GC deletes expired records after one additional grace day. This policy is independent of request-log cleanup. Short retention may expire sources needed by old checkpoints. Disabling the feature does not erase stored records.

## Storage and recovery

Input/output items use 64 KiB content-addressed blocks, deduplicated only inside an authenticated project/API-key scope. Manifests retain ordering, repetitions, tool arguments and results. Blocks, manifests and references are independently authenticated and encrypted. One ID cannot overwrite another immutable branch version. A transaction publishes the records together, large history bypasses the settings cache, and reads/writes are batched. Snapshots are limited to 64 MiB and manifests to 4 MiB.

Completed responses can reconstruct HTTP/WebSocket deltas after log deletion or restart. Native and local checkpoints share this storage; legacy sources and sealed summaries remain readable. Missing historical data cannot be invented. Ordinary archival failures emit warnings; a failed source write prevents publication of a new compaction checkpoint.

## Context assembly

Native/auto routes still prefer accepted native state. After a matched rejection, or for local bridging, recovery expands authenticated original sources and verifies unique ordered retained-prefix matches, tool pairs, model/instructions/tools, a 16-generation bound and a 64 MiB source bound.

A known catalog context limit is required. The working budget is the smaller of half that limit and 256 KiB of JSON bytes. This conservative text budget does not cover images, encrypted tool arguments or unknown items; those retain the existing compatibility path.

- When the source fits, replay original content without a summary call.
- Otherwise, stable segments contain complete original user turns, each at most 128 KiB. All original user/system/developer messages and the latest complete user turn remain exact.
- Older segments receive separate reusable summaries keyed by original content, model, instructions, tools and reasoning options. No segment is generated solely from another summary.
- At most four new summaries and 32 segments are allowed per preparation. Unknown budgets, indivisible oversized turns or too many cold segments retain existing fallback. Failures after generation begins do not start another summarization chain, and mandatory instructions are never silently truncated.
- Remaining space can restore complete relevant segments using deterministic keyword matching against current user text. This is limited lexical retrieval, not guaranteed semantic recall; no unimplemented history tool is injected into client tool lists.
- Local compaction seals the assembled context directly without another model call. Segment generations retain independent usage records; local assembly reports zero model usage.

User corrections retain chronological order. The gateway does not guess which instructions are obsolete. Derived summaries use assistant material, never promoted tool-output instructions. Restored text passes through existing prompt-protection policy before sending or summarizing it.

## Quality and cost limits

Lossless archival does not mean every token is visible in every model request. Long histories still use model summaries, and lexical retrieval can miss relevant evidence. Original private reasoning ciphertext remains archived; portable views carry only visible reasoning summaries and log the number of private items omitted. The gateway cannot decrypt or guarantee migration of hidden provider state. Encrypted tool arguments are not eligible for this exception.

Cold segment generation still costs tokens and time; subsequent reuse must justify it. Prefix changes after compaction and hidden upstream account changes may still reduce cache hits. Model and reasoning effort are preserved. Evaluate total cost and task quality as well as cache percentages.

Acceptance should compare source coverage, summary count, cached tokens, TTFT and total latency, with tests for early constraints, exact numbers/code/tool results, later corrections and branch isolation. Synthetic CI and loopback providers cannot prove unchanged real-model answer quality.

Archived input is captured independently from the compiled provider view. Where the source can be verified, new checkpoints flatten earlier original sources before publication so repeated compaction does not accumulate ancestry depth. Publication verifies the immutable alias in the same transaction and rolls back conflicting versions. Existing blocks refresh timestamps without resending ciphertext to SQL; valid reads also refresh an explicitly finite retention policy.
