# Codex tools correction

Baseline `60a01ef800d647663a42d20ef87cebc3892f6bbc`. User rejected the previous UI and clarified that consistency, readability and appearance are primary acceptance criteria.

- Tool rows share one compact component. Only the information icon triggers tooltips; no persistent explanatory paragraphs or one-off model-add button.
- Image model selector includes fetched models and persists its exact choice. Dedicated image routing uses this model while preserving caller/profile boundaries.
- Separate native/other-channels image policy; tools disabled does not deny native capability. Retry and dispatch enforce policy.
- Official conversation catalog supplementation includes all five currently documented GPT Image 2/2.5/2.6 IDs. Third-party discovery stays authoritative.
- Replace the boolean speed UI with Follow request/Standard/Fast/Ultrafast. Request tier/alias wins; matching relay aliases are selected independently, otherwise use native tiers. Native history and cache fields remain untouched.
- Explicitly configured relay aliases are not limited to the official model list: Terra and custom-model Fast/Ultrafast pairs resolve independently. Unconfigured custom suffixes and official model names remain untouched.
- On narrow screens the model side panel occupies the dialog content and has a back control; the form returns with fetched model choices retained. Wide screens keep the existing side-by-side layout.
- Linked imagegen conversation `01a1aacb-d73c-7013-bc36-19409e52ca5b` first checked only environment variables and incorrectly reported missing credentials. Its later successful call used existing Codex auth.json API key and active provider base_url. One rule added to the local imagegen SKILL.md to check that configured pair before reporting missing configuration; no script changes, credential disclosure or live image generation. Skill validator passed.

Sources: official [model catalog](https://developers.openai.com/api/docs/models) fetched 2026-11-05; previous focused official/source research reused without repeating the peer commit review.

YCE receipt: `/tmp/yce-results/yce-search-20261105T084053-2423956-6106df.xml`, SHA-256 `1b077ff066280b149ea88c0e020326669504c796f28758220bd0a8dc5cc36a10`; successful search and full EOF read.

Validation uses GitHub Actions only. UI coverage must inspect actual Chinese/light/dark desktop/mobile controls, tooltip hit areas, fetched model save/reopen, separate policies, and both speed choices. Release only after five mandatory workflows pass on one exact SHA, then stop at Run GoReleaser start.

The first CI run exposed the missing generated GraphQL image-model binding and model-panel overlap on narrow screens. The CI-generated patch was reviewed/applied, and the panel layout was corrected without relaxing overflow or interaction assertions. Subsequent validation and release evidence are recorded in DELIVERY.md/DELIVERY.json.
