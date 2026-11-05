# Native GIF generation

Baseline: `b4d781732ba2c094ae49cafde29f2a5ba3a4b831`.

The linked thread `01a1ab27-4174-70d3-a282-c53500cdb2b8` first asked for fallback approval despite an existing configured provider, attempted GIF through edits, then received generic 400 responses from both full and minimal generations requests. The archived artifacts establish those outcomes but do not identify the upstream cause of the minimal 400. No production service is listening on port 8090 in this execution environment; no paid inference or service restart is performed.

Current source already forwards output_format and preserves base64 response bytes. Missing frame_count/frame_duration_ms fields would be dropped when reconstructing an image request. This change adds those optional fields through inbound/unified/outbound structures and uses existing native generation endpoints. Omission preserves upstream defaults. GIF edits and contradictory image options receive explicit validation errors. Dedicated channel/model selection and profile boundaries are unchanged.

The existing JSON image preview now recognizes GIF signatures instead of assuming PNG. No additional UI controls or visual style changes are introduced. Browser coverage exercises the same preview and raw-data toggle.

Official sources fetched 2026-11-05:
- https://developers.openai.com/api/docs/guides/image-generation#generate-animated-gif-output
- https://developers.openai.com/api/reference/resources/images/methods/generate

Both support native GIF generation and default 9 frames / 200 ms. They disagree on upper frame and delay limits, so upper limits remain provider-owned. Tests cover exact frame-parameter forwarding, unchanged GIF bytes/frames/delays, regular and Codex routes, body passthrough on/off, invalid controls, and JSON/multipart edit rejection.

Focused web and GitHub code searches over sub2api, CLIProxyAPI, OmniProxy, codex2api, gpt-load, new-api, magpie and cc-switch found no reusable frame_duration_ms or output_format+gif implementation. No previous peer commit review was repeated. Implemented against the official API contract.

Official openai/skills SKILL.md blob `88195882efdedbfab56e0eb2570dd4fd6d9e8610` contains no GIF guidance and retains explicit fallback approval. Current official CLI blob `57cab20e05ced1bb744a2272b40dab096f60cbc0` also lacks native GIF output controls. Retained the local skill and updated it narrowly: configured fallback without redundant confirmation, 2.6/GIF guidance, CLI frame/complexity options, older-SDK extra_body compatibility, and validation of animation bytes before saving. Static default stays gpt-image-2. Six offline skill tests and skill-creator validation passed. Backups and official snapshots are under `/tmp/axonhub-native-gif-20261105/`.

YCE search: `/tmp/yce-results/yce-search-20261105T104526-2501177-90ae3a.xml`; 34283 bytes; SHA-256 `b5807fe304141b7c2e34cbb784414dd617ae272c2c3a73389b699b7a10a85d12`. Exit 0, code gate true, fully read through EOF. No task anchor.

Gateway builds/tests/generation/lint run exclusively in GitHub Actions. Delivery: five mandatory workflows on the same SHA, fast-forward dev, annotated unstable tag, stop at Run GoReleaser start. Final evidence goes in DELIVERY.json/DELIVERY.md.
