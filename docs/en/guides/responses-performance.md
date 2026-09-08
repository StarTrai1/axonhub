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
internal metadata after an upstream HTTP or pre-output streaming 400 explicitly identifies it as an
unknown or unsupported parameter. Message content, tools, encrypted compaction
history and the prompt cache key remain unchanged. The normal retry budget still
applies; enable at least one same-channel retry to recover the first rejection.

Learned rejection is kept in a bounded, process-local cache for six hours, scoped
by channel, endpoint, model and hashed credential. Subsequent requests avoid the
known-failing round trip. Other endpoints and credentials retain their native
metadata. Generic HTTP 400 errors never activate this behavior.

## Rejected encrypted reasoning

Encrypted reasoning is preserved by default, following the
[official stateless handoff guidance](https://developers.openai.com/api/docs/guides/deployment-checklist#use-reasoningencrypted_content).
If a Codex channel explicitly returns HTTP or pre-output streaming 400 with
`error.code: invalid_encrypted_content`, a same-channel retry can instead rebuild
reasoning from retained explicit history. This is a degraded recovery, not
decryption or lossless recovery of hidden reasoning.

Recovery requires user history and correctly paired function/custom-tool results.
Named standalone function results without a `call_id` remain valid; an unmatched
nonempty `call_id` still fails closed.
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

For streams within the replay limit, the native snapshot is published by the upstream stream
producer before a terminal event is fanned out to the pass-through client. An
immediate tool-result continuation therefore does not wait for the separate
pipeline consumer or database completion. This also covers synthesized Codex
terminal events. Terminal snapshots and completed native items remain recoverable
even when the auxiliary one-MiB wire-event buffer overflows. Wire events are not
retained without a bound.

Hot replay snapshots retain the two-hour TTL and 2,048-record/64-MiB aggregate
limits. A complete record can now reach the same 16-MiB limit as persisted replay,
so an immediate continuation of a large history does not race database completion.
If history cannot be restored, Codex HTTP fallback returns
`previous_response_not_found` instead of silently removing the reference and
sending an isolated tool result. Clients must replay full input, as described in
the [official WebSocket guide](https://developers.openai.com/api/docs/guides/websocket-mode#fork-a-conversation-onto-a-new-stream).
Ordered eviction avoids scanning all records on
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

After a restart, local summaries are found by their exact retained compaction key
across HTTP and WebSocket request formats, independent of model changes and the
recent-history scan limit. Memory entries and duplicate-generation locks are
isolated by authenticated API key and project. Missing source history produces a
recorded `compaction_history_unavailable` 400, not an unrecorded generic 500.

## Transient upstream rate limits

HTTP/Responses streaming 429 and 503 overload errors before meaningful output use the
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

Peer-originated HTTP/2 `INTERNAL_ERROR` and `REFUSED_STREAM` resets are also
retryable before output commitment, including the last sticky channel. Local
cancellation and protocol errors are not classified as these transient resets.
WebSocket error `status_code` aliases and scalar header values are accepted, so
`Retry-After` survives both streaming and synchronous error conversion. New
credit/spend/usage exhaustion codes map to 429 but do not trigger futile
same-channel retries.

## GPT-6 Astra stream compatibility

`response.steer.accepted` only acknowledges queued input. Even if the initial
response completes first, the gateway waits for its automatic successor or an
explicit pending/failure event. Native pass-through and converted streams share
this terminal boundary. Delayed-terminal repair does not synthesize completion
after steering has been accepted.

Async function/custom-tool identity, steering, configuration updates and prompt
cache controls retain their existing contracts. A custom tool whose input arrives
only in `response.custom_tool_call_input.done` now emits the missing suffix once;
duplicate or empty done events cannot erase or repeat an already delivered input.
Conflicting final input fails explicitly instead of executing a changed command.

Generic relay error messages now retain structured `code` and `param` in execution
logs. A successful final attempt does not erase earlier attempt failures, and a
bare historical `bad response status code` is not proof of remote-compaction failure.

## Cross-channel history IDs

When replaying full history to official OpenAI Responses/Codex endpoints, generic
`item_...` IDs emitted by compatible relays can fail type-specific validation.
Final request preparation removes only those generic IDs from known message,
reasoning, function/custom-tool call and result items, including HTTP, WebSocket,
and raw pass-through requests. It preserves the original history, content,
summaries, encrypted fields, extension metadata, and `call_id` values. It does not
fabricate server-side `rs_` or `fc_` references by replacing prefixes.

Native IDs, item references, unknown item types, and third-party destinations are
unchanged. This fixes generic-ID compatibility; it does not decrypt foreign
provider state or guarantee cross-account portability of encrypted content.

## Cross-protocol tools and accounting

Chat Completions function tools with omitted `strict` become explicitly
`strict:false` when converted to Responses, preserving the source API default.
Explicit values and native Responses defaults are unchanged. Response conversion
and stream aggregation preserve the actual upstream `service_tier`; a terminal
value supersedes an earlier value. Cache-read and cache-write token counts remain
separate, and the requested service tier is not substituted for actual accounting.

## Claude Code identity version

The fallback Claude Code identity is 2.1.263. A validated
`AXONHUB_CLAUDE_CODE_VERSION=2.1.263` environment variable pins the gateway's
generated identity and bypasses automatic version discovery. An authentic client
User-Agent remains preserved; existing channel header overrides still take
precedence. Invalid version values are ignored. Without a pin, asynchronous
official release discovery remains enabled.
