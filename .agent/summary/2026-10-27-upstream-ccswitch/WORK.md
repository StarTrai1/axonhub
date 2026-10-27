# Incremental upstream and peer integration

- Base: dev e551d4669fbe38da89362eb0e85c5f511fe8cc6f (PR17). Branch sync/upstream-ccswitch-20261027.
- User authorizes existing integration and release procedure: CI-only tests/build/lint/format/tidy/generation, fast-forward exact passing SHA to dev, annotated tag, stop at GoReleaser start. Preserve frontend style. No production modification or paid probes.
- Previous peer cutoff: 2026-10-27-upstream-terra/peers-ledger.json. Each repo head frozen once into /tmp/axonhub-sync-20261027-next/*/snapshot.json; only new commits reviewed. New cc-switch: pinned gateway architecture plus commits since 2026-10-26T20:50:09Z and explicit 96f01af07fddc80790c2fe4fd80c4a073a06074a (older, separately included).
- YCE passed exit0, may_analyze_or_edit_code=true, /tmp/yce-results/yce-search-20261027T122404-402909-4eb9e1.xml; 32858 bytes; c30c5107cfa55d6af93cae174df1d4620b884ec3fbebd525753e6f148c8f95a5. Full file read through EOF; no task anchor.
- Applicable AGENTS.md, Go/cache/biz/frontend/i18n/UI/GraphQL/docs rules read. No Markdown rules in ~/.codex/rules; no Jev routing or agents.
- OpenAI Docs official Terra model page fetched; published capabilities match prior adaptation. Exact tool and pricing constraints retained.
- Review and validation in progress.

- Frozen review complete: 171 records (25 upstream, 24 sub2api including two merge records, 112 magpie, one gpt-load, eight cc-switch incremental + explicit older Azure commit); four peers unchanged. Full decisions in peers-ledger.json. No remote-head refresh or new range.
- Merge commit 1305accbe9aaa76fb075e5d87489cfdaf5709370; PR18 https://github.com/StarTrai1/axonhub/pull/18.
- Adapted cc-switch param=input resource-error recovery; initial request and subsequent fresh requests untouched. Adapted magpie pre-message_start SSE comment; retain configured heartbeat timings.
- Upstream conflicts: preserve motion/react-only frontend dependency set; retain native thinking extensions/strict between_tools validation, model catalog Terra, bounded retry behavior, lossless payload compression and request references. Move fastest-query updates into dashboard_queries.go; do not revive dead daily throughput APIs.
- PostgreSQL jsonb NUL fix uses existing version1 lossless envelope, with structural marker fallback for normalized jsonb envelopes/references. CI explicitly runs real PostgreSQL ranking parity and NUL storage tests.
- CI1: LLM, frontend/browser, concurrency and three CLI loopback jobs passed. Root build/lint/vuln/Windows failed on duplicate dashboard imports left by conflict resolution; fixed. Generation/formatter artifacts from Test40510276821 and Lint40510276707 applied. Dependency and schema generation produced no substantive delta. Next validation pending.
