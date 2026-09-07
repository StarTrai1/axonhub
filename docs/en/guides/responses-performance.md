# Responses latency and compatibility

## First output timeout

The existing **Stream First Output Timeout** setting now waits for meaningful
output, including text, reasoning, tool calls, or supported media output. A
`response.created` event or a role-only initialization chunk no longer disables
the timeout. A terminal response still completes normally. Zero disables the
timeout. The configuration/API field name remains unchanged.

Before output is committed, configured retry rules can select another attempt.
The timeout does not interrupt a response after meaningful output starts.

## Streaming keep-alive

SSE keep-alive defaults to enabled with a 15-second interval. OpenAI-compatible
streams use SSE comments; Anthropic-compatible streams use protocol ping events.
Heartbeats do not represent model output or billable tokens. They operate after
the streaming handler receives the upstream stream, not during connection setup
or the pre-output retry probe.

To preserve an explicit opt-out:

```yaml
server:
  sse_keep_alive:
    enabled: false
```

## Codex relay metadata compatibility

Some Responses relays reject newer Codex
`internal_chat_message_metadata_passthrough` fields. AxonHub only removes this
internal metadata after an upstream HTTP 400 explicitly identifies it as an
unknown or unsupported parameter. Message content, tools, encrypted compaction
history and the prompt cache key remain unchanged. The normal retry budget still
applies; enable at least one same-channel retry to recover the first rejection.

Learned rejection is kept in a bounded, process-local cache for 30 minutes, scoped
by channel, endpoint, model and hashed credential. Subsequent requests avoid the
known-failing round trip. Other endpoints and credentials retain their native
metadata. Generic HTTP 400 errors never activate this behavior.

## Rejected encrypted reasoning

Encrypted reasoning is preserved by default, following the
[official stateless handoff guidance](https://developers.openai.com/api/docs/guides/deployment-checklist#use-reasoningencrypted_content).
If a Codex channel explicitly returns HTTP 400 with
`error.code: invalid_encrypted_content`, a same-channel retry can instead rebuild
reasoning from retained explicit history. This is a degraded recovery, not
decryption or lossless recovery of hidden reasoning.

Recovery requires user history and correctly paired function/custom-tool results.
It refuses unresolved `previous_response_id`, compaction or item references,
encrypted agent messages/arguments, and unknown input types. Only encrypted
reasoning items are replaced: visible summaries and reasoning text become
assistant text, while messages, tool inputs/results, native tool IDs and cache
keys remain intact. Unsupported summary/content types fail closed.

The rule is scoped to the current request and channel; it does not disable
encrypted reasoning for later requests. Generic validation errors do not trigger
it, and it consumes the existing same-channel retry budget.

## Replay cache

Native Responses snapshots now capture the actual outbound input and upstream
output before downstream conversion. Recovery after an in-memory miss prefers
the authenticated request's completed execution snapshot, not its original
WebSocket delta. This preserves full history and native `msg_`/`fc_` IDs.
Known gateway-generated message IDs and function-call IDs equal to `call_id`
are omitted during replay; call linkage, native IDs and encrypted reasoning
remain intact. An unresolved stored delta is never treated as complete history.

For cacheable streams, the native snapshot is published by the upstream stream
producer before a terminal event is fanned out to the pass-through client. An
immediate tool-result continuation therefore does not wait for the separate
pipeline consumer or database completion. This also covers synthesized Codex
terminal events. Cache admission limits still apply; oversized histories retain
the completed-database fallback rather than an unbounded in-flight cache.

Hot replay snapshots retain the existing two-hour TTL, 2,048-record/64-MiB aggregate
limits and one-MiB record limit. Persisted complete histories up to 16 MiB can be
replayed without admitting them to the hot cache; exceeding the hot-cache limit
alone no longer drops continuation history. Ordered eviction avoids scanning all records on
each lookup. Immutable stored snapshots allow copying outside the shared lock;
bulk byte copies reduce per-item allocations without sharing writable capacity
between items. These are gateway overhead improvements, not promises of a given
provider TTFT or prompt-cache hit rate.

Hosted Test CI runs focused race tests and publishes `responses-cache-benchmarks`.

## Switching existing compacted conversations to local bridge

With a channel's remote-compaction policy set to `local_bridge`, previously
remote-compacted conversations can be reconstructed from retained source
requests. If a source already contains an older compaction, AxonHub first
generates or reuses that older summary, substitutes it into the source, and then
generates the next summary. It does not simply remove opaque compaction items;
those items carry conversation state in the
[official compaction protocol](https://developers.openai.com/api/docs/guides/compaction).

Recovery stops on missing retained sources, cycles, or more than 16 generations.
Summary generation is deduplicated without waiting on nested generations inside
the same single-flight operation. Internal summary requests use the same explicit
metadata/reasoning rejection recovery and configured retry budget. Recovering both
rejections in one summary request needs two same-channel retries; no retry
settings are changed automatically. Each actual upstream attempt is recorded
separately when request persistence is enabled.

## Transient upstream rate limits

HTTP 429 and Responses SSE rate-limit errors before meaningful output use the
same retry policy. Another physical channel is preferred when available. On the
last channel, transient limits can use the configured same-channel retry budget,
including a trace-sticky candidate. With five retries and the default one-second
base delay, backoff is 2, 4, 8, 16 and 32 seconds plus up to 500 ms of jitter.
Longer configured delays and supported `Retry-After` (seconds or HTTP date),
`retry-after-ms`, and `x-ms-retry-after-ms` values take precedence.

Explicit quota exhaustion, local admission limits and provider waits over one
minute do not activate same-channel rate-limit retries. Cancellation interrupts
backoff. No retry is started after text, reasoning or tool output has been
committed, preventing duplicate output or tool execution. The existing retry
count is a hard bound; persistent upstream throttling still returns an error.
No automatic changes are made to system retry settings. Client/proxy deadlines
must accommodate the additional wait; streaming heartbeats do not cover this
pre-output retry phase.

## Claude Code identity version

The fallback Claude Code identity is 2.1.263. A validated
`AXONHUB_CLAUDE_CODE_VERSION=2.1.263` environment variable pins the gateway's
generated identity and bypasses automatic version discovery. An authentic client
User-Agent remains preserved; existing channel header overrides still take
precedence. Invalid version values are ignored. Without a pin, asynchronous
official release discovery remains enabled.
