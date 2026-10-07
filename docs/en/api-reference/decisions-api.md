# OpenAI Decisions API

AxonHub exposes `POST /v1/decisions` with the native `openai/decisions` format. Authenticate with an AxonHub API key in `Authorization: Bearer …` and send `Content-Type: application/json`. Existing model permissions, model mappings, channel proxies, bounded retry policies, and request/usage logging apply.

OpenAI currently offers this public beta with `gpt-6-luna`. Configure that model, or an alias mapping to it, on an OpenAI or OpenAI Responses channel. Both channel types include a dedicated Decisions endpoint. It always uses HTTP, including when the primary Responses endpoint uses WebSocket. Other compatible channel types can explicitly add `openai/decisions` with a custom base URL/path if their upstream supports it. Candidates without the native endpoint are excluded. If a model has a protocol override, include `openai/decisions` in its allowed protocols to serve these requests.

```json
{
  "model": "gpt-6-luna",
  "input": "The delivery arrived with a cracked screen.",
  "questions": [
    {"type": "predicate", "name": "damage", "instructions": "Does the customer report physical damage?"}
  ]
}
```

`input` accepts text or user messages containing `input_text` and inline base64 `input_image` parts. Remote image URLs, image file IDs, non-user messages, tools, files, audio, and streaming are unsupported. The endpoint accepts at most 128 images.

`questions` and `answers` are ordered arrays. Supported questions are `predicate` (`probability`), `choice` (`choices` and returned `choice`), and `score` (ordered `levels`, with a probability-weighted zero-based score). Optional names, per-question refusals, probability distributions, and extension fields are preserved. This endpoint does not translate to chat, Responses, or TypeSafe System One.

## Configure cost tracking

Add **Decisions Input Tokens** (`decisions_input_tokens`) in the channel model price editor. This item uses the full `usage.input_tokens` count. Chat input/output/cache price items do not apply to Decisions; Decisions items do not apply to other requests. Missing Decisions pricing leaves cost unknown. The optional gateway `usage.cost` field follows the existing cost-injection setting.

As of October 7, 2026, OpenAI lists $0.10 per million Decisions input tokens, with no separate output or cache charges. Configure volume pricing with $0.10 through 272,000 tokens and $0.20 above that threshold to reflect the long-context multiplier on the entire input. Regional processing premiums require corresponding channel prices. Automatic chat catalog prices do not populate this separate item or override an explicitly configured Decisions price.

Sources: [Decisions guide](https://developers.openai.com/api/docs/guides/decisions), [GPT-6 Luna model](https://developers.openai.com/api/docs/models/gpt-6-luna).
