# Seedance Video Adapters

This document covers both the native `volcengine-seedance` adapter and the `chinadatapay-seedance` relay adapter.

## 1. Overview and Routing

Both adapters share the underlying Seedance protocol implementation while maintaining separate endpoints, model prefixes, and lifecycle behaviors.

- **`volcengine-seedance`**:
  - Model prefixes: `volcengine/doubao-seedance`
  - Submit: `POST /contents/generations/tasks` (relative to upstream BaseURL)
  - Poll: `GET /contents/generations/tasks/{task_id}`
  - Cancel: `POST /contents/generations/tasks/{task_id}/cancel`
- **`chinadatapay-seedance`**:
  - Model prefixes: `cdp/doubao-seedance` (standard public), `chinadatapay/doubao-seedance` (legacy)
  - Submit: `POST https://tp-api.chinadatapay.com:8000/api/v3/contents/generations/tasks` (fixed absolute URL)
  - Poll: `GET https://tp-api.chinadatapay.com:8000/api/v3/contents/generations/tasks/{task_id}`
  - Cancel: _Unsupported upstream_ (returns error; local tracking is cancelled)

Both adapters operate under zero backend configuration; model matching is resolved code-side via the driver's prefix Trie.

---

## 2. Upstream Credentials

- **Volcengine**: Host upstream credentials supply the Volcengine/Ark API key and BaseURL.
- **ChinaDataPay**: Configure CPA upstream credentials with the ChinaDataPay platform Token as the API key, and BaseURL `https://tp-api.chinadatapay.com:8000/v1`.
  - The adapter uses its built-in full URL, overriding BaseURL concatenation.
  - Do not change the BaseURL to `/api/v3`.
  - Authentication headers are sent as `Authorization: Bearer <Token>`.

---

## 3. Input Specification

Seedance adapters accept the standard CPA OpenAI-style video format. Specialized parameters are supplied via `input_reference` and `extra_body`:

```json
{
  \"model\": \"cdp/doubao-seedance-2-5\",
  \"prompt\": \"Create a dynamic sequence based on references\",
  \"input_reference\": [
    {
      \"type\": \"video_url\",\n      \"video_url\": { \"url\": \"asset://asset-video-id\" },
      \"role\": \"reference_video\"
    },
    {
      \"type\": \"audio_url\",
      \"audio_url\": { \"url\": \"asset://asset-audio-id\" },
      \"role\": \"reference_audio\"
    }
  ],
  \"extra_body\": {
    \"duration\": -1,
    \"ratio\": \"adaptive\",
    \"resolution\": \"720p\",
    \"omni_reference_task_type\": \"reference\",
    \"generate_audio\": false,
    \"priority\": 0
  }
}
```

### Parameter Guidelines:

- **`ratio: adaptive`**: Recommended for Seedance 2.5 frame-to-video, editing, and extension tasks.
- **`duration: -1`**: Used for video edit tasks where duration should match the source media.
- **Multimodal references**: Roles such as `reference_video` and `reference_audio` preserve media semantics even for extensionless URLs or `asset://` URIs.
- **`extra_body.content`**: Raw Seedance content items (text, frame references, `draft_task` entries) are supported directly when native payload formatting is desired.

---

## 4. Usage Recording (`metadata.seedance.usage`)

The shared Seedance parser extracts the upstream `usage` object returned during task queries and persists it in `content_generations.metadata`:

```json
{
  \"seedance\": {
    \"usage\": {
      \"completion_tokens\": 123456,
      \"total_tokens\": 123456
    }
  }
}
```

### Key Properties:

- **Comprehensive capture**: Preserves all fields returned by the upstream provider, including future extensions and nested tool usage.
- **Non-destructive updates**: Missing, null, or non-object responses do not overwrite previously recorded usage snapshots.
- **Billing impact**: **Billing formulas and pre-charge mechanics are completely unchanged.** Recorded token data is retained strictly for telemetry, analytics, and accounting audits.

Querying recorded usage directly from the database:

```sql
SELECT generation_id, model,
       JSON_EXTRACT(metadata, '$.seedance.usage') AS seedance_usage
FROM content_generations
WHERE generation_id = '<generation_id>';
```
