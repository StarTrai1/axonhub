# Codex CLI0.165.0 →0.166.0

Official [changelog](https://learn.chatgpt.com/docs/changelog) and [source comparison](https://github.com/openai/codex/compare/rust-v0.165.0...rust-v0.166.0). Comparison is diverged, with265 target-side records; do not assume the release tags form a linear history. Source and inventory are recorded in codex-ledger.json.

- 0.166.0/gpt-6.1-sol is the sole packaged CLI hosted loopback entry; older versions are removed per the user’s CI request. No local installation or inference probe.
- Guardian assessments arrive in response.metadata with response_id and metadata.guardian item_id/requires_review. Existing raw metadata roundtrip is retained and extended with a regression; the gateway never grants client permissions from that metadata (fc916a22b5).
- Granular approval templates and headings are catalog-owned extensions. Existing raw catalogs preserve them; add an explicit assertion (c587d6e54c,133c11db43,e8e6a922c0). No blanket replacement of catalogs.
- Host user input IDs use msg_input_ (e3149ddb11). Existing native item preservation retains them; only explicit upstream rejection permits optional-ID recovery.
- policy_capacity_unavailable remains a distinct upstream code and Retry-After follows ordinary overload advice in the CLI (6a6e2fb1f0). It is never classified as session expiration.
- Reasoning accounting on resume/fork is a client context-baseline fix: last_usage_includes_reasoning persists with token usage (c62e9df39f). Preserve actual upstream usage; no gateway-side speculative token adjustment.
- Guardian parent affinity is connection-owned, ChatGPT-origin/path-scoped routing cookies (e742db9f7b). No transport of client cookies to third-party gateway destinations is introduced.
- Runtime/gateway auth changes retain standard bearer delivery and explicitly reject conflicting configured headers (cf29971d93,9526f8d0d0,9cce575134). Existing gateway authentication remains compatible.
- Built-in OpenAI catalog overrides accept only allowed ChatGPT hosts; third-party hosts still require a custom provider (f00c70f5d0). Explicit-catalog1MiB client cap is removed (adca78ddae); gateway input bounds remain its own contract.
- New queue resume, doctor network-only, TUI selection, executor packaging, Rust/Node tooling, GCP credentials and local permission/Guardian changes are client behavior, not gateway implementation to copy.

- Literature search adds `literature_search_query` to standalone search (aa854dabf5). The gateway already forwards the raw search envelope, including new command keys.
- Named untrusted context uses `function_call_output` with a name/namespace and no call_id (6d8f311b00). Existing named-output conversion and recovery completeness guards already distinguish it from an orphan output with a missing call. Dynamic tool replacement (f088e0c4ff) uses existing additional_tools retention.
