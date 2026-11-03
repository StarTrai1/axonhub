# Validation

All builds, tests, lint and formatting run in GitHub Actions. Local checks are read-only diagnostics, source review and `git diff --check`.

## Baseline reproduction

Tests-only SHA: `bbdb09dcc1c9e6e269e3a989f8827f46e5d88c43`.

[GitHub Test run 41676261575, root job 126057775464](https://github.com/StarTrai1/axonhub/actions/runs/41676261575/job/126057775464) reproduced all six expected failures before the runtime fix:

- `TestCheckpointCanonicalRecoveryKeepsAcceptedAncestor/missing_predecessor/openai/responses` and `/openai/responses-ws`: `Retained compaction source could not be reconstructed: a completed predecessor is missing`.
- `TestCheckpointCanonicalRecoveryKeepsAcceptedAncestor/oversized_archive/openai/responses` and `/openai/responses-ws`: `Your input exceeds the context window of this model`, `context_length_exceeded`.
- `TestCheckpointCanonicalRecoveryExpandsOnlyRejectedAncestor/reject=false` and `/reject=true`: the same missing-predecessor error from an older ancestor that should not have been opened.

The root job completed with failure. These are synthetic deterministic failure-path controls; no private incident body or live provider is involved. Lint found an embedded-field blank-line requirement in the new fixture; that finding is corrected in the fix commit. The unfinished baseline workflows may be superseded by the fix push; no claim is made that the baseline suite passed.

## Fixed behavior coverage

The final suite retains the same reproduction cases, adds failures for a genuinely rejected ancestor with no retained source, unrelated upstream validation, partial streamed summaries and cycles, and updates the existing source-contract tests to require explicit ancestor rejection before expansion. Durable summary reuse and unchanged continuation suffixes are asserted after an adapter restart.

Final exact-SHA CI and delivery evidence are recorded in local DELIVERY receipts after checks complete.
