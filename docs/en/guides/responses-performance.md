# Responses latency and compatibility

## Prompt-cache stability and gateway overhead

Prompt-cache reuse depends on the provider and an unchanged eligible prompt prefix.
Keep instructions, tool definitions/order and retained history stable between turns;
explicit client cache keys remain authoritative. A native compaction creates a new
prefix, and switching accounts or channels may lose provider-side cache state.
The [official OpenAI prompt-caching guide](https://developers.openai.com/api/docs/guides/prompt-caching)
explains public API behavior; Codex relay capabilities can differ.

Multi-key channels preserve their existing trace-based credential selection. When
tracing is absent, a retained thread or authenticated transport session supplies
stable affinity instead of selecting a random credential on every turn. Client
session IDs are scoped to the authenticated owner. Disabled keys are rejected even
if a prior selection is still cached. Requests without a trusted conversation
identity continue to distribute normally. This supports upstream connection and
cache reuse without changing channel priorities, quotas or retry budgets.

Identity-only Responses cache reads no longer copy the retained conversation.
Continuation preparation reuses a complete snapshot already loaded for that request,
while preserving ownership, expiry, context-window rollover and replay isolation.
Metadata updates before dispatch, on response headers and at finalization select
only required database fields, avoiding readbacks of request/response bodies and
stream chunks. Stored history, headers, usage accounting and terminal status remain
available through their normal readers.

Measure provider cache reads as `sum(cached_tokens) / sum(input_tokens)` over the
same model, channel, credential and conversation-window population. Track cache
writes separately, together with first meaningful output latency, total latency
and retry count. Gateway allocation benchmarks do not establish a production
cache-hit percentage or a provider latency improvement. These changes do not
send prewarming requests or alter billable provider cache settings.

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

Explicitly learned rejection is kept in both a bounded process-local cache and
a versioned database record for six hours, scoped by the final channel, endpoint,
model and hashed credential. The database stores only scope digests and expiry
times, never credentials or request contents, with at most 1,024 capabilities.
Expired entries are ignored on reads and pruned on subsequent learning; the
earliest-expiring entries are evicted when the limit is reached. After a restart
or upgrade against the same database, the first request can restore an unexpired
capability without extending its original expiry. The first installation of this
feature, expiry or a new scope may still require one explicit rejection to learn;
old execution logs are not used to guess credential capabilities.

Subsequent requests avoid the known-failing round trip. Other endpoints and
credentials retain native metadata. Database read failures or malformed records
leave the original request intact, allowing the existing explicit-rejection retry;
write failures do not block in-memory recovery. Storage operations are bounded to
two seconds, with a one-minute lookup-miss/error cache. Conditional updates merge
concurrent learning without overwriting other scopes. Channel settings, retry
budgets and failed-attempt audit records are unchanged. Generic HTTP 400 and
encrypted-reasoning errors never become persistent global capabilities; the latter
retain the per-request guarded recovery described below.

## Native compaction source retention

Before returning a native compaction checkpoint, AxonHub saves its complete source
history, encrypted and bound to the API key, project and native checkpoint. This
allows later recovery without depending on request-log retention. Source history
has a 64 MiB limit; its compressed, Base64-encoded storage uses a separate size
budget from the 4 MiB local text summary limit. New sources encrypt compressed bytes directly in a versioned binary envelope, removing a redundant inner Base64 layer. Older saved sources remain readable.
Incompressible encrypted history can therefore cross the old 4 MiB storage boundary
without being truncated or discarded. A real storage or size-limit failure still
prevents publishing a checkpoint whose source cannot be recovered.

## Rejected encrypted reasoning

Encrypted reasoning is preserved by default, following the
[official stateless handoff guidance](https://developers.openai.com/api/docs/guides/deployment-checklist#use-reasoningencrypted_content).
If a Codex channel explicitly returns HTTP or pre-output streaming 400 whose
code or complete error message confirms an encrypted-content rejection, a same-channel retry can instead rebuild
reasoning from retained explicit history. This is a degraded recovery, not
decryption or lossless recovery of hidden reasoning.

Recovery requires user history and correctly paired function/custom-tool results.
Named standalone function results without a `call_id` remain valid; an unmatched
nonempty `call_id` still fails closed.
Generic encryption errors refuse unresolved `previous_response_id`, compaction or item references,
encrypted agent messages/arguments, and unknown input types. A complete rejection naming
one reasoning item may preserve other known native checkpoints and agent messages unchanged;
this does not establish that the destination can decrypt them. Only encrypted
reasoning items are replaced: visible summaries and reasoning text become
assistant text, while messages, tool inputs/results and cache keys remain intact.
Optional item IDs dependent on rebuilt reasoning are detached, preserving `call_id`.
Unsupported summary/content types fail closed.

The rule is scoped to the current request and channel; it does not disable
encrypted reasoning for later requests. Generic validation errors do not trigger
it, and it consumes the existing same-channel retry budget.

When the provider redacts the encrypted item identity to `gAAA...AA==`, the gateway
cannot distinguish reasoning from a native checkpoint. This complete decryption
rejection uses staged recovery: first rebuild reasoning from complete explicit
history while preserving the sole native checkpoint. If the same rejection
continues after no encrypted reasoning remains, restore the checkpoint from its
retained source bound to the current API key, project and checkpoint identity.
Each stage schedules at most one changed retry within the existing retry budget.
Missing source history fails explicitly; checkpoints are never simply deleted.
Explicit item IDs, full ciphertext and indexed parameters must agree with the
request; conflicts cannot fall through to generic recovery. Unresolved history,
orphan tool results and ambiguous encrypted agent messages still prevent recovery.
Only completed recoveries remember scoped hashes of old reasoning. New reasoning,
other conversations and other destination credentials remain unaffected.

The complete relay error `invalid encrypted summary` (optionally followed by `(request id: …)`) uses the same two-stage recovery. It does not identify an encrypted item, so ambiguity, retained-source and ownership checks still apply. Ordinary HTTP 400 responses, permission errors and messages that merely contain similar text do not authorize recovery.

### Portable agent messages

For authenticated Codex requests, the dedicated `message` parameter of `agents` and
`collaboration` messaging tools uses gateway encryption. AxonHub requests that parameter
as plaintext, then seals the exact returned text with AES-GCM before delivering the tool
call to the client. Argument fragments are withheld until the complete sealed call is ready.
Receiving agent messages and replayed sender calls are opened before upstream execution,
including native compaction and local compaction recovery. Other tools and encrypted reasoning
keep their existing behavior. This supports switching channels within the same installation,
project and API key without depending on the original upstream's encryption keys.

The installation secret must remain stable, as with sealed local compaction. Accepted tool
transport state is retained for the upstream conversation, credential/account and model, so
a resumed native checkpoint can omit tool definitions without changing message delivery.
HTTP compaction and WebSocket continuation share that state; client session identities are
preserved. New messages carry their encrypted body and do not need one database row per message.

Older provider-encrypted messages require separately recovered original text. Delivery labels
and later final reports do not replace intermediate messages. An operator can import an
installation-encrypted recovery bundle bound to the exact message ID, author, recipient and
original ciphertext hash. Unmatched messages remain intact. The runtime reads these entries
without probing a source provider, enabling disabled channels or editing original history.

The SQLite maintenance command defaults to read-only validation and opens no server or
migrations. Run the native binary on the database's host OS, after reviewing the private bundle:

```sh
axonhub agent-recovery --database axonhub.db --file recovery.encrypted.json
axonhub agent-recovery --database axonhub.db --file recovery.encrypted.json --apply
```

An import authenticates the bundle for the existing installation and API-key owner, then adds
only missing recovery entries in one transaction. Conflicting entries stop the entire import.
Keep the encrypted bundle for rollback: the same command with `--remove --apply` removes only
the exact matching imported entries. Neither operation alters requests, executions or history.

## Overloaded relay affinity in old conversations

Some relays pin conversations to internal upstream routes, so old conversations can remain
overloaded while new ones succeed. For non-OAuth Codex relays, a 500/503 must explicitly report
model capacity or high demand and include repeated `(channel,channel)` sticky-route entries in
`X-New-Api-Routed-Channel-Id` before the gateway starts recovering the upstream session identity.
The request must contain replayable full history without unresolved response references or
opaque compaction checkpoints.

Recovery updates upstream session, thread and window identities and `prompt_cache_key` together,
and clears old turn-state/routing hints. Input history, item IDs, every ciphertext, tool results,
model and credentials remain intact. Downstream thread and turn ownership remain unchanged.
Only a completed response confirms the alias for later requests. The in-memory cache is scoped
to project, API key, channel configuration, destination, upstream credential, model and client
thread, with a two-hour lifetime and at most 2,048 entries. Failed aliases are not confirmed.

Recovery consumes the existing same-channel retry budget and allows at most three distinct
replacement identities per request. A new selection can still be exhausted: a repeated explicit
capacity error selects a different identity instead of replaying the failed one. This also covers
plain route IDs or an omitted route header after migration, including an alias previously confirmed
for this owner. Once the bound is reached, the existing alternate-channel path applies. If a selected
resource instead returns the supported opaque 400 rejection, the existing complete-history replay
detaches optional item IDs on that same identity while preserving all ciphertext and tool linkage.
The sticky-pair requirement only admits the initial migration. Exhausted quota,
wait hints longer than one minute, user cancellation, official
endpoints and already-committed output do not trigger this recovery. It neither decrypts history
nor guarantees that the relay can supply a healthy upstream route.

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
requests. If a source contains an older native compaction, AxonHub first tries
that source with the checkpoint intact. It only restores the ancestor after an
explicit, matched rejection before summary output starts. Locally sealed ancestors
are authenticated and expanded before sending. It does not remove opaque compaction items;
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

Automatic HTTP negotiation remembers a peer `INTERNAL_ERROR` or
`HTTP_1_1_REQUIRED` reset for ten minutes and uses HTTP/1.1 for subsequent
requests to that HTTPS origin. This also covers resets while reading the response
body. The bounded, in-memory policy belongs to the client/proxy pool and does not
change channel settings; unrelated origins and WebSocket connections are unaffected.
Explicit HTTP/1.1 settings remain authoritative. TLS verification, proxy
credentials, request bodies and encrypted history are preserved.

The transport never replays a POST itself. Before output, the existing pipeline
may retry within its configured budget using the learned protocol. After text,
reasoning or tool output, the failed attempt remains failed; only a subsequent
request uses HTTP/1.1. This mitigates protocol-specific resets, but cannot repair
an upstream that also fails over HTTP/1.1 or restore a partially delivered response.

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

## Frontend assets and routing caches

Release builds precompress text assets such as HTML, JavaScript and CSS before embedding them, while preserving Vite's original `frontend/dist`. Clients accepting gzip receive those bytes directly; other clients receive the restored content. Existing content-hashed `/assets/` files allow one year of immutable caching. Entry HTML remains uncached, and missing assets receive no long-lived cache policy. This reduces embedded size and browser transfer without changing model requests, context or token-cache keys.

Standalone search reuses the candidate slice when no channel needs exclusion. The model-association cache is bounded to 1024 entries, with opportunistic eviction of expired entries and old channel snapshots. Reads still validate channel versions, association signatures, model update times and other existing conditions. These changes do not establish lower provider TTFT or a higher prompt-cache hit rate; those require separate measurements.

## Native checkpoint continuity across channels

`auto` tries native history first. Both `auto` and `native` remain eligible for
normal routing and previous-channel affinity; a later local-bridge candidate does
not rewrite an earlier native request. After a confirmed checkpoint rejection,
another retained channel is tried before reconstructing the checkpoint locally.
Configured routing tiers, retry budgets, model and reasoning effort still apply.

Recovery first tries a small exact-history replay. This requires an authenticated
source, uniquely matching retained items in order, complete tool pairs, matching
model/instructions/tools, and text-only materialized history. Its conservative
budget is the smaller of 64 KiB of serialized JSON and half the catalog context
limit; an unknown context limit disables this path. No items are truncated. This
restores the retained source, not information already removed by the client
before that source was captured. Confirmed replay has its own versioned scope.

For an authenticated source explicitly marked `responses_compaction_v2`, recovery
can regenerate a **native checkpoint on the destination** instead of first
converting the whole history into a generic text summary. It reuses the saved
source input and current destination session/window identity. The completed
result must contain one valid native compaction item. Only the matching checkpoint
is replaced; surrounding messages, tool results and current request options stay
intact. The source for the new native checkpoint is retained as well.

An encrypted alias binds the original checkpoint to the project, API key, thread,
window, target channel, endpoint, model and credential. It survives restart and
request-log cleanup and reuses the same target checkpoint on later turns. A
rejected target checkpoint disables that alias for six hours and falls back within
the existing retry budget. Unsupported native compaction or a matched ancestor
rejection can use the text fallback; overload, authentication failures and partial
streams do not authorize an additional generation. Native reconstruction can
still take time and tokens and cannot guarantee exact preservation of all semantic
details. Hidden upstream account rotation can invalidate even a target-generated
checkpoint. The gateway does not possess provider decryption keys.

Compatibility remains the default: saved authenticated summaries remain usable,
and new text summaries are generated only when earlier paths cannot continue.
Ancestor recovery is lazy, each completed result is retained, and logs distinguish
native migration (`X-Axonhub-Native-Compaction-Migration`) from text fallback
(`X-Axonhub-Remote-Compaction-Cache`, `recovery_kind=text_summary`). Internal markers
are not sent upstream. Text summarization is lossy; no synthetic test can prove
unchanged answer quality. Cancellation stops pending generation and releases
waiting callers; already completed checkpoints get a bounded persistence window.

One recovery shares matched ancestor rejections and explicitly unsupported item
type `status` fields across its native and text requests, scoped to the same
channel, endpoint, model and credential. Text fallback does not resend an ancestor
already rejected by native migration. Waiters on that migration receive the same
evidence. Input indexes, reasoning removal and resource-affinity repairs are not
carried into other requests, and this evidence is not a global capability cache.

After an ancestor rejection, recovery first tries restoring its exact source
under the same budget and integrity checks described above. When safe, it
summarizes the latest source directly without an intermediate ancestor summary,
preserving the configured reasoning effort. Large, opaque or ambiguously matched
histories still use recursive compatibility fallback. Eliminating duplicate
failed attempts does not eliminate long-history generation cost or guarantee
provider prompt-cache reuse.

## Measuring actual cache reuse and user wait

Report both `cached / current input` and how much of the preceding unchanged input
was reused. A 30k cached prefix followed by 10k new tool output yields 75% while
reusing nearly all reusable history. Never pad prompts or discard tool output to
improve the ratio. Compare completed requests within the same model, thread,
agent, channel and window, checking settings and identity before attributing a
miss to the provider. Cache reuse across hidden accounts cannot be forced by
changing the displayed percentage or adding public API fields to Codex endpoints.

New request-level latency and TTFT include routing, checkpoint preparation and
all retries from orchestrator admission. Execution-level metrics and channel
health retain their per-attempt clocks. Historical records are not rewritten.
A high cache ratio alone does not establish low latency: upstream throttling,
queueing and generation can still dominate. Request `updated_at - created_at` is
a useful historical approximation, but also includes persistence overhead.

The pass-through reader retains the provider terminal event before fan-out. If a client disconnects immediately after a failed or incomplete response, execution accounting preserves that provider outcome and explicit final usage even when the transformed consumer has not reached the event. This evidence does not acknowledge downstream delivery; the main request keeps the existing delivery rules.
