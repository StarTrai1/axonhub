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

## Replay cache

Replay snapshots retain the existing two-hour TTL, 2,048-record/64-MiB aggregate
limits and one-MiB record limit. Ordered eviction avoids scanning all records on
each lookup. Immutable stored snapshots allow copying outside the shared lock;
bulk byte copies reduce per-item allocations without sharing writable capacity
between items. These are gateway overhead improvements, not promises of a given
provider TTFT or prompt-cache hit rate.

Hosted Test CI runs focused race tests and publishes `responses-cache-benchmarks`.

## Claude Code identity version

The fallback Claude Code identity is 2.1.263. A validated
`AXONHUB_CLAUDE_CODE_VERSION=2.1.263` environment variable pins the gateway's
generated identity and bypasses automatic version discovery. An authentic client
User-Agent remains preserved; existing channel header overrides still take
precedence. Invalid version values are ignored. Without a pin, asynchronous
official release discovery remains enabled.
