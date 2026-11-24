# MiniMax-H3 Video Adapters

This document details the MiniMax-H3 video adapters implemented for ChinaDataPay (`chinadatapay-minimax-h3`) and LK888 (`lk888-minimax-h3`).

---

## 1. Adapter Comparison

| Feature               | ChinaDataPay (`chinadatapay-minimax-h3`)                                                                                                                                                                                                     | LK888 (`lk888-minimax-h3`)                                                                                                                                                  |
| :-------------------- | :------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | :-------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Model Prefixes**    | `cdp/minimax-h3`, `chinadatapay/minimax-h3` (case-insensitive)                                                                                                                                                                               | `lk/minimax-h3`, `lk888/minimax-h3`                                                                                                                                         |
| **Upstream Model ID** | Exact case: `MiniMax-H3`                                                                                                                                                                                                                     | Lowercase: `minimax-h3`                                                                                                                                                     |
| **Endpoints**         | Absolute full URLs:<br>Create: `POST https://.../v2/video_generation`<br>Query: `GET https://.../v2/query/video_generation/{id}`<br>Regen: `POST https://.../v2/video_regeneration`<br>Cancel: `DELETE https://.../v2/video_generation/{id}` | Relative URLs (appended to BaseURL):<br>Create: `POST /media/generate`<br>Query: `GET /media/status?task_id={id}`<br>Regen: _Unsupported_<br>Cancel: _Unsupported upstream_ |
| **Upstream BaseURL**  | `https://tp-api.chinadatapay.com:8000/v1`                                                                                                                                                                                                    | `https://api.lk888.ai/v1`                                                                                                                                                   |
| **Task Cancellation** | Supported upstream (DELETE request confirmation required)                                                                                                                                                                                    | Unsupported upstream (local cancellation only)                                                                                                                              |
| **Storage Provider**  | `cdp`                                                                                                                                                                                                                                        | `lk888`                                                                                                                                                                     |

---

## 2. Parameter Mapping and Resolution Tiers

Both adapters accept standard OpenAI-style parameters and map them to provider-specific contracts.

### Duration

- `seconds` (or `extra_body.duration`): Must be an integer between 4 and 15 seconds.

### Resolution & Size Mapping

Dimensions supplied in `size` (`WIDTHxHEIGHT`, `WIDTHXHEIGHT`, or `WIDTH*HEIGHT`) map to resolution tiers based on total pixel budgets:

| Pixel Threshold           | Resolution Tier                  | Common Dimensions                   |
| :------------------------ | :------------------------------- | :---------------------------------- |
| Up to 1,400,000 px        | `768P`                           | `1280x720`, `1366x768`, `1024x1024` |
| 1,400,001 - 2,500,000 px  | `1080P` _(LK888)_ / `2K` _(CDP)_ | `1920x1080`                         |
| 2,500,001 - 5,500,000 px  | `2K`                             | `2560x1440`, `2048x2048`            |
| 5,500,001 - 10,000,000 px | `4K` _(LK888 only)_              | `3840x2160`                         |

_Note: Native `extra_body.resolution` explicitly overrides size-derived resolution._

---

## 3. Media Modes

### A. First and Last Frame (首尾帧)

Passed via `input_reference` or `extra_body`:

```json
{
  \"model\": \"cdp/MiniMax-H3\",
  \"prompt\": \"The camera glides through morning fog\",
  \"seconds\": 6,
  \"size\": \"1920x1080\",
  \"input_reference\": [
    {
      \"type\": \"image_url\",
      \"role\": \"first_frame\",
      \"image_url\": { \"url\": \"https://example.com/first.jpg\" }
    },
    {
      \"type\": \"image_url\",\n      \"role\": \"last_frame\",
      \"image_url\": { \"url\": \"https://example.com/last.jpg\" }
    }
  ]
}
```

- **LK888**: Automatically sets `mode: \"shouweizhen\"` and packs images into `images: [first, last]`.
- **ChinaDataPay**: Maps directly to native first/last frame properties.

### B. Reference Media (参考生)

Supports mixing reference images, video clips, and audio:

- Reference images: Up to 9 images (`reference_image`)
- Reference videos: Up to 3 videos (`reference_video` or `base_video`)
- Reference audio: Up to 3 audio files (`reference_audio`)

_Note: First/last frames and reference media are mutually exclusive._

---

## 4. ChinaDataPay Video Regeneration

The `chinadatapay-minimax-h3` adapter supports video regeneration via `POST /v2/video_regeneration`:

```json
{
  \"model\": \"cdp/MiniMax-H3\",
  \"size\": \"2560x1440\",
  \"extra_body\": {
    \"task_type\": \"regeneration\",
    \"source_task_id\": \"task_original_id\"
  }
}
```

- Provide either `source_task_id` or fresh content with a `base_video`.
- Regeneration enforces `2K` resolution.
- `seconds` / `duration` must not be sent for regeneration requests.

---

## 5. Lifecycle and Polling Behavior

- **Background Polling**: ChinaDataPay H3 employs exponential backoff on query failures (3s \(\to\) 6s \(\to\) 12s \(\to\) 24s \(\to\) max 30s) before resetting upon a successful query.
- **Terminal Statuses**: HTTP 400, 401, and 404 query errors are treated as terminal; transport errors and 429/5xx trigger standard host retries without prematurely failing the task.
- **Upstream Cancellation Confirmation**: For ChinaDataPay H3, cancellation requires upstream acknowledgment via `DELETE`. Failure to reach upstream does not mark the task cancelled locally.
