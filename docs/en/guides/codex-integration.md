# Codex Integration Guide

---

## Overview
AxonHub can act as a drop-in replacement for OpenAI endpoints, letting Codex connect through your own infrastructure. This guide explains how to configure Codex and how to combine it with AxonHub model profiles for flexible routing.

### Key Points
- AxonHub performs AI protocol/format transformation. You can configure multiple upstream channels (providers) and expose a single OpenAI-compatible interface for Codex.
- You can aggregate Codex requests from the same conversation by enabling `server.trace.codex_trace_enabled` (uses `Session_id`) or adding extra headers via `server.trace.extra_trace_headers`.

### Prerequisites and AxonHub setup
- AxonHub instance reachable from your development machine.
- Valid AxonHub API key with project access.
- Access to Codex (OpenAI compatible) application.
- An existing Codex channel in AxonHub. The channel supplies the upstream base URL, credentials, and HTTP transport.

Before configuring Codex, open the **Codex Compatibility** settings in AxonHub:

1. Enable the compatibility switch.
2. Select the Codex channel to use for the native model catalog.
3. Use the channel's **Test** action to verify that its `/models` endpoint is reachable. Testing does not save the setting, enable the channel, or change its status.

The switch is off by default. Saving an enabled configuration requires a selected existing Codex channel. The catalog test can be run while the switch is off, and a failed test does not prevent saving. Catalog results are filtered to the models visible to the caller; this does not authorize a manually selected model, which is still checked by the normal inference permission path.

### Configure Codex authentication and routes
1. Create `${HOME}/.codex/auth.json` with the AxonHub API key in both supported fields. Keeping both fields provides an API-key fallback across Codex client versions:
   ```json
   {
     "personal_access_token": "ah-***",
     "OPENAI_API_KEY": "ah-***"
   }
   ```
2. Edit `${HOME}/.codex/config.toml` and point the provider and auth API at the same `/codex` compatibility surface:
   ```toml
   model = "gpt-6.1-sol"
   model_provider = "openai"
   openai_base_url = "https://your-axonhub-host/codex"
   chatgpt_base_url = "https://your-axonhub-host/codex"
   web_search = "live"

   [features]
   fast_mode = true
   context_management = true
   ```
3. Set the required auth API base in the same shell that starts Codex on macOS/Linux:
   ```bash
   export CODEX_AUTHAPI_BASE_URL=https://your-axonhub-host/codex
   ```
   On Windows, add `CODEX_AUTHAPI_BASE_URL=https://your-axonhub-host/codex` in **System Environment Variables**. Do not set a separate catalog URL; the `/codex` base is used for whoami, models, and Responses requests.
4. Restart Codex to apply the configuration.

The compatibility surface used by this guide is:

- `GET /codex/v1/user-auth-credential/whoami` — local API-key identity.
- `GET /codex/api/codex/accounts/check` — account metadata for Codex account selection.
- `GET /codex/models` — selected-channel native catalog, intersected with caller-visible model IDs.
- `POST /codex/images/generations` — native image generation through ordinary model/channel routing.
- `POST /codex/images/edits` — native image editing through ordinary model/channel routing.
- `POST /codex/responses` — Responses HTTP/SSE inference.
- `GET /codex/responses` — Responses WebSocket inference.

#### Trace aggregation by conversation (important)
Enable the built-in Codex trace extraction to reuse the `Session_id` header as the trace ID:

```yaml
server:
  trace:
    codex_trace_enabled: true
```

If Codex sends a different stable conversation identifier header (for example `Conversation_id`), you can configure AxonHub to use it as a fallback trace header in `config.yml`:

```yaml
server:
  trace:
    extra_trace_headers:
      - Conversation_id
```

**Note**: Enabling this also ensures that requests from the same trace are prioritized to be sent to the same upstream channel, significantly improving provider-side cache hit rates (e.g., Anthropic Prompt Caching).

#### Testing
- Start Codex and send a sample prompt; AxonHub's request logs should show `/codex/responses`.
- Confirm that the model list loads from `/codex/models` and that the selected channel remains available for inference.
- If your Codex client uses account discovery, confirm `/codex/api/codex/accounts/check` is reachable with the same API key.
- Image generation and editing use `/codex/images/generations` and `/codex/images/edits`; the routed inference channel—not necessarily the catalog channel—must expose the corresponding native image endpoint and model.
- Enable tracing in AxonHub to inspect prompts, responses, and latency.

The current version uses the ordinary Responses path for remote compaction-v2: a `compaction_trigger` is sent to `/codex/responses`, and the returned compaction item can be used in a later Responses request. It does not implement the history/notes APIs or a standalone `/codex/responses/compact` route; those unsupported paths return JSON 404. `context_management` enables Codex's Responses compaction flow, while history notes remain part of future compatibility work. `fast_mode` selects the Responses `service_tier` requested by the client; the upstream model catalog and response determine whether that tier is available. There is no separate `ultrafast` endpoint or local capability probe. Existing `/v1` routes are unchanged.

### Working with Model Profiles
AxonHub model profiles remap incoming model names to provider-specific equivalents:
- Create a profile in the AxonHub console and add mapping rules (exact name or regex).
- Assign the profile to your API key.
- Switch active profiles to alter Codex behavior without changing tool settings.

<table>
  <tr align="center">
    <td align="center">
      <a href="../../screenshots/axonhub-profiles.png">
        <img src="../../screenshots/axonhub-profiles.png" alt="Model Profiles" width="250"/>
      </a>
      <br/>
      Model Profiles
    </td>
  </tr>
</table>

#### Example
- Request `gpt-4` → mapped to `deepseek-reasoner` for getting more accurate responses.
- Request `gpt-3.5-turbo` → mapped to `deepseek-chat` for reducing costs.

### Responses Lite continuation compatibility

Responses Lite `additional_tools` definitions and GPT-6 `configuration_update` items remain in their original positions during a bounded retry after an explicit encrypted-reasoning rejection. Recovery still requires complete visible history and matched tool calls; opaque compaction items, unresolved references, and encrypted function arguments are not discarded. Local compaction bridging and recovery of later encrypted reasoning are separate stages.

An explicit upstream rejection of `internal_chat_message_metadata_passthrough` can establish compatibility for all metadata-bearing Codex input item types, including `cell_id` nested in the metadata of `custom_tool_call_output`. Only that optional metadata property is removed; IDs, tool outputs, arguments, ciphertext, and other request fields remain unchanged. The existing channel/endpoint/model/credential scope and six-hour persistence limit still apply.

Codex 5-hour, 7-day, and reported GPT-Reserve windows display absolute reset timestamps in the browser's local time zone (`yyyy-MM-dd HH:mm`). Missing provider reset times are not synthesized. Main-window hover details retain the relative countdown.

Each window retains its reported usage percentage. Account-wide exhaustion does not turn a partially used weekly window into 100% usage; the channel still follows its configured quota routing policy when ordinary usage is exhausted.

### Local compaction checkpoints

`auto` preserves native history until an actual rejection. Where possible, the
gateway restores exact retained text or generates and retains a destination-native
V2 checkpoint before using text summarization. See [continuation recovery and its
quality limits](responses-performance.md#native-checkpoint-continuity-across-channels).

An upstream `validation_error` / `invalid encrypted summary`, or an exact reasoning-item rejection containing `reasoning hydration failed: Encrypted content could not be decrypted or parsed.`, enters the existing bounded recovery flow. Recovery requires a complete encryption-rejection match, complete visible history, and the matching project/API key. It first repairs rejected reasoning while retaining the native checkpoint. If that checkpoint is also rejected, it uses authenticated retained source history or a saved summary. Missing history or an exhausted retry budget stops recovery instead of inventing context.

Local encrypted agent messages and tool arguments are authenticated before checkpoint recovery checks the retained history. This ordering applies both to the initial recovery and to later requests using a confirmed recovery cache entry. The exact recovered tool arguments, call/output links, and agent-message prefixes are preserved; unknown ciphertext, tampered content, and a different project/API key still stop recovery before an upstream request is sent. Disabling body pass-through also preserves the encrypted-argument marker during conversion. Only the supported local `message` envelope can be opened; markers declaring other encrypted arguments are not discarded.

New local bridge results carry an authenticated, encrypted summary in their `axonhub-local-v2` reference. They can be restored after a restart or request-log cleanup without generating another summary. References are bound to their item ID, API key, project, and installation secret. Preserve the installation secret when restoring a database backup; replacing it invalidates previously sealed references.

For older `axonhub-local-v1` references, AxonHub first uses an existing checkpoint or the original compaction record. If that record has expired, recovery can use a completed continuation only when its thread, compaction window, full retained input prefix, and tool identities match. Client-only execution metadata and absent/null reasoning content do not prevent this comparison. The exact recovered summary is encrypted and saved separately from request logs. Missing or divergent history returns an explicit error; it is not replaced with a guessed summary.

### Background memory costs after a foreground failure

When a foreground request marked `request_kind=turn` finally fails, the gateway cancels active `request_kind=memory` requests for the same project, API key, and thread, and pauses new memory requests for 5 minutes. New requests receive HTTP 400 with `codex_memory_paused` before reaching upstream. Foreground retries remain allowed; a newly started foreground request that succeeds clears the pause early. A recovered intermediate channel error does not trigger the pause, and memory failures do not extend it.

Foreground outcomes are ordered by request start: a late failure from an older concurrent request cannot override a newer success. Expiry is checked when the next request arrives and admits that request. Clearing the pause only changes in-memory admission state; it creates no LLM request and does not resubmit canceled memory jobs. Codex schedules subsequent memory work. Gateway runtime logs record `Codex background memory admission resumed` with `foreground_succeeded` or `pause_expired`; earlier failed request rows remain unchanged.

Per-frame WebSocket task metadata takes precedence over handshake prewarm metadata. Other threads, API keys, subagents, search, and compaction requests are unaffected. This policy requires explicit client task and thread identity. It does not permanently disable memory or refund tokens already processed upstream. Pause state lives in a bounded cache in the current gateway process and clears on restart.

### Codex 0.154.0 compatibility

The gateway preserves `configuration_update` items without inventing message IDs and retains supplied `client_metadata.parent_response_id` and `guardian_credits_requested`. The default client version for requests without a Codex identity is 0.154.0; an explicit client identity remains authoritative.

Additional quota data retains the reported `normal_model_slug` as metadata, without remapping requests. The passive quota checker does not advertise `x-openai-codex-luna-reserve: 1`: that capability is for clients able to apply a Reserve selection, as specified in the [Codex 0.154.0 usage client](https://github.com/openai/codex/blob/rust-v0.154.0/codex-rs/backend-client/src/client/rate_limit_resets.rs).

### Codex 0.162.x compatibility

The gateway preserves `partial_answer` message phases and explicit `end_turn: false` through Responses conversion and stream aggregation. Completing one inference does not imply that Codex should end the turn. Base instructions supplied as developer input messages and incremental `additional_tools` declarations retain their history order, including tool-removal notices.

Generated stream failures use `response.failed`, with validated retry timing under `response.error.headers`. Nested WebSocket error advice is retained as well. Existing retry budgets and hard-quota handling still apply. Provider misalignment details, including opaque `review_target` values, survive normal Responses error conversion; hidden/custom error policies continue to omit those details.

Codex 0.162.0 adds custom-provider capability overrides. If the selected AxonHub channels support native compaction or have local compaction bridging enabled, this optional client configuration enables the V2 `compaction_trigger` protocol:

```toml
[model_providers.axonhub-responses.capabilities]
remote_compaction = "v2"
external_web_access = true
```

`remote_compaction = "unsupported"` keeps compaction in the client. `external_web_access` controls whether Codex may request live web search; it does not grant upstream search or account access. Omitted values retain the client's provider defaults. See the [official capability schema](https://github.com/openai/codex/blob/rust-v0.162.0/codex-rs/model-provider-info/src/capabilities.rs).

The fallback Codex identity is 0.164.0. Explicit client identity and dynamic stable-version refresh remain authoritative. Hosted checks compare 0.162.1 and 0.163.0 with gpt-6-sol and gpt-6.1-sol against loopback fixtures. Both versions exercise partial-answer continuation, developer input instructions, retry timing from a gateway-generated failure frame, encrypted agent-message delivery, usage, concurrency, and cancellation. They do not establish provider account availability.

The [0.163.0 release](https://github.com/openai/codex/releases/tag/rust-v0.163.0) enables instant steering by default and waits for cancellation acknowledgements before reusing a WebSocket. The gateway acknowledges late interrupts for responses owned by the connection and drains acknowledgements that arrive after the terminal response. Relays that omit or mismatch the acknowledgement lose that pooled connection; their terminal response and usage remain available. The gateway also preserves optional `output.encrypted_content` replay, `history_initialization` metadata, and server-provided model-catalog fields. Background compaction and tool-output retention remain client-controlled; this release does not manufacture model or Daybreak access.

#### Instant steering

Codex 0.162.0 enables the existing `instant_interrupt` client feature by default. For Responses Lite over WebSocket, the client sends `response.interrupt` with `mode: "discard_partial_items"`, drains the interrupted response, and continues with the new input. AxonHub already forwards this control event on the active connection, preserves completed output and usage, and excludes unfinished items from the interrupted snapshot. HTTP cancellation follows the existing cancellation path; a gateway cannot provide graceful upstream interruption when the selected upstream lacks it.

The public [mid-turn steering API](https://developers.openai.com/api/docs/guides/steering) still accepts only `type`, `previous_response_id`, and `input` for `response.steer`. Do not add an `instant` field. This public queued-steering flow and the Codex client interrupt flow retain their respective completion and tool-result rules.

### Troubleshooting
- **Codex reports authentication errors**: ensure `${HOME}/.codex/auth.json` contains a valid AxonHub API key in `personal_access_token`, and that `CODEX_AUTHAPI_BASE_URL` points to the same `/codex` base as `openai_base_url` and `chatgpt_base_url`.
- **The model list is empty or unavailable**: confirm that the compatibility switch is enabled, a Codex channel is selected, and the channel's `/models` endpoint passes the settings-page test. Catalog filtering is not a substitute for inference authorization.
- **Unexpected model responses**: review active profile mappings in the AxonHub console; disable or adjust rules if necessary.

---

## Provider Quota Tracking

AxonHub automatically tracks quota usage for Codex provider channels, displaying the current status with battery icons in the interface.

### How It Works

- **Automatic Polling**: AxonHub periodically polls your Codex account to check quota status
- **Storage**: Quota data is stored in the database and updated based on the configured check interval
- **Visual Indicators**: Battery icons show your remaining quota at a glance.

### Quota Windows

Codex uses multiple quota windows:
- **Primary window**: Main usage limit with a configurable duration (e.g., 5 hours, 1 day)
- **Secondary window**: Optional secondary usage limit with its own duration and reset schedule

The system shows both window percentages including the primary window duration and reset time.

### Configuration

Adjust the quota check interval in `config.yml`:

```yaml
provider_quota:
  check_interval: "5m"           # Check every 5 minutes (default)
```

Or via environment variable:

```bash
export AXONHUB_PROVIDER_QUOTA_CHECK_INTERVAL="30m"
```

Supported intervals: `1m`, `2m`, `3m`, `4m`, `5m`, `6m`, `10m`, `12m`, `15m`, `20m`, `30m`, `1h`, `2h`, etc.

**Recommendations:**
- **Development**: Use shorter intervals (e.g., `5m`) for quick feedback
- **Production**: Use `5m` (default) for timely quota detection; increase to `10m` or `20m` to reduce API calls

### Refreshing Quota Data

You can manually trigger a quota refresh by clicking the refresh icon in the quota status popover.

### Viewing Quota Status

1. Look for the battery icon next to the settings gear in the header
2. Click the battery icon to view detailed quota information including:
   - Primary window usage percentage and duration
   - Primary window reset time
   - Plan type (if available)
   - Secondary window usage (if configured)

### Related Documentation
- [Tracing Guide](tracing.md)
- [OpenAI API](../api-reference/openai-api.md)
- README sections on [Usage Guide](../../../README.en-US.md#usage-guide)


## Codex 0.159.2 and GPT-6.1 Sol

The gateway recognizes `gpt-6.1-sol` and the local `gpt-6.1-sol-fast` alias. The alias selects `service_tier: priority` only when the request has not supplied a tier. Explicit client versions remain authoritative; the fallback version follows the current Codex baseline described above.

The [public API model](https://developers.openai.com/api/docs/models/gpt-6.1-sol) supports `low`, `medium`, `high`, `xhigh`, and `max`, with `medium` as its API default. Codex 0.159.2 uses its own model preset (default `low`, plus a client `ultra` option); the gateway does not replace an explicit client effort with the API default. When migrating requests, unsupported `none`/`minimal` become `low`, including ordered `configuration_update` items. Sampling and logprob parameters are removed when reasoning is enabled. Use a Responses endpoint for tool calls; the model's Chat Completions endpoint supports requests without tools.

The bundled catalog records the official standard input/cache-read/cache-write/output rates of $2/$0.10/$2.50/$10 per million tokens, and the >272K context rates and Fast mode metadata. Existing channel price settings remain authoritative for billing. [Astra Ultrafast](https://developers.openai.com/api/docs/guides/ultrafast-mode) uses `service_tier: ultrafast`; its metadata is included, and the tier is preserved through HTTP, WebSocket, conversion, and response usage reporting. Sol 6.1 Ultrafast is not advertised as available.

A TLS `bad record MAC` before usable output is classified as an upstream transport failure and uses the existing bounded retry policy. If encrypted reasoning was already repaired following an explicit rejection, the retry retains that repair and visible history. TLS verification is unchanged. Generic HTTP 400 responses without an encryption error code or precise item rejection do not authorize deleting conversation history. A retry can still fail if the upstream connection or capacity remains unavailable.

DevDay's Agents API browser sessions use a separate `/v1/agents/sessions` lifecycle, and Private Safety Processing is an organization/project provisioning feature. These are not enabled by a Responses model change. Decisions API was announced in limited preview; no stable public request contract was found during this update. This gateway update does not claim support for those separate services.

Mid-turn `response.steer` is accepted for the GPT-6 family, including Sol 6.1 and local Fast aliases, while its upstream WebSocket is active. It remains bound to the registered response on the same connection and the existing bounded control queue. GPT-5.6 and older models remain unsupported. See [mid-turn steering](https://developers.openai.com/api/docs/guides/steering).

## Search-only channels

Enable **Search only** in a Codex channel's edit dialog to reserve it for `openai/search` (`/v1/alpha/search`). The channel is excluded from Responses HTTP, Responses WebSocket, Chat Completions, compaction, and other generation routes. Turning the setting off restores normal routing.

Choose a GPT-5.6 or newer model in **Search model** to explicitly pin standalone searches. For example, searches from `gpt-6-astra` can use `gpt-6.1-sol`. **Follow request model** (the default) keeps the model supplied by the current conversation in its search request. It never chooses a channel's default test model or an arbitrary advertised model. Project and API key channel scopes, tags, and quota restrictions still apply.

This setting does not grant upstream search capability. When `/alpha/search` is unavailable, the request fails or follows the existing retry policy to another channel. The search-only channel cannot fall back to `/responses` to emulate search.

Web search policy offers **Native search**, **Other channels**, and **MCP only**. Select **Other channels** for a generation channel that cannot handle standalone search: candidate selection excludes it before dispatch, so `openai/search` goes directly to another channel allowed by the project/API key scope, tags and quotas. If none is available, routing fails without spending retries on the excluded channel. Normal generation and its search-tool declarations are preserved; this setting does not redirect hosted search performed inside an upstream generation. **MCP only** also removes native search tools from generation. The UI keeps **Search only** and **Other channels** mutually exclusive; an externally saved conflicting configuration honors the standalone-search opt-out.

Stored **Compatible** policies normalize to native search. GPT-5.5 Responses search emulation, its capability cache, and Responses Lite search-tool injection/retry have been removed. An unavailable native search endpoint follows the existing cross-channel retry policy without changing to a generation request.

### Codex CLI 0.164.0

Codex can now drain an interrupted WebSocket response before applying queued
agent messages. A delivered `response.incomplete` with reason `interrupted`
retains its response ID for the next delta. AxonHub records this as canceled,
keeps the terminal response's accepted output, and does not mark the task as
successfully completed. Token limits and other incomplete reasons remain failures.

The request's optional `prompt_cache_key` is independent of `Session-Id`: an
explicit custom key is preserved and an omitted key is not synthesized. Client
metadata patches are composed by Codex before transmission; AxonHub forwards the
resulting metadata. The CI compatibility matrix includes the real 0.164.0 CLI
against local synthetic upstreams; it makes no paid provider calls.

### GPT-6.1 Terra and resource recovery

`gpt-6.1-terra` supports `none`, `low`, `medium` (default), `high`, `xhigh`, and `max`. Use a Responses channel for tool calls; Chat Completions supports text requests but cannot carry this model's tool calls. The gateway retains `none`, normalizes legacy `minimal` to `low`, and keeps the original reasoning prefix when applying `configuration_update` items. The `gpt-6.1-terra-fast` alias selects the same model with the priority service tier.

The bundled model catalog uses Standard prices of $1 input, $0.05 cached input, $1.25 cache write (30 minutes), and $5 output per million tokens. Requests exceeding 272,000 input tokens use twice the input/cache prices and 1.5 times the output price for the whole request. Fast costs twice Standard. A 12-hour cache write is $2.50 per million tokens before long-context or tier adjustments; configure matching channel prices when using that retention. See the [official model page](https://developers.openai.com/api/docs/models/gpt-6.1-terra).

Azure history compatibility is now automatic for an explicit resource-mismatch rejection from a third-party Codex channel. The channel-wide switch has been removed and old saved values are ignored. The initial attempt retains native history; a rejected request can retry once with readable retained history. Native checkpoints may require a retained-history summary request. This correction does not become a policy that strips IDs and encrypted reasoning from later requests, and it does not change `prompt_cache_key` or relax retry budgets. Missing retained history returns an error instead of silently dropping context.
