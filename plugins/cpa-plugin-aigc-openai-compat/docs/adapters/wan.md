# Wan 3.0 Video Adapters

This document details the Wan 3.0 video generation adapters: `chinadatapay-wan` (relay gateway) and `qwen-wan` (DashScope).

---

## 1. Routing and Endpoints

- **`chinadatapay-wan`**:
  - Model prefixes: `cdp/wan3.0-video`, `cdp/wan3.0-video-prime`, `chinadatapay/wan3.0-video`, `chinadatapay/wan3.0-video-prime`
  - Create: `POST https://tp-api.chinadatapay.com:8000/api/v1/services/aigc/video-generation/video-synthesis` (fixed absolute URL)
  - Query: `GET https://tp-api.chinadatapay.com:8000/api/v1/tasks/{task_id}`
  - Cancel: _Unsupported upstream_ (returns error; local tracking is cancelled)
- **`qwen-wan`**:
  - Model prefixes: `alibaba-cn/wan`, `alibaba/wan`, `qwen/wan`
  - Create: `POST https://dashscope.aliyuncs.com/api/v1/services/aigc/video-generation/video-synthesis`
  - Query: `GET https://dashscope.aliyuncs.com/api/v1/tasks/{task_id}`
  - Cancel: `POST https://dashscope.aliyuncs.com/api/v1/tasks/{task_id}/cancel`

---

## 2. Input Specifications

### Standard OpenAI-Style Input

```json
{
  \"model\": \"cdp/wan3.0-video\",
  \"prompt\": \"A drone shot traversing snow-capped mountains at sunrise\",
  \"seconds\": 5,
  \"input_reference\": [
    {
      \"type\": \"image_url\",
      \"role\": \"first_frame\",\n      \"image_url\": { \"url\": \"https://example.com/first.png\" }
    }
  ],
  \"extra_body\": {
    \"resolution\": \"720P\",
    \"ratio\": \"16:9\",
    \"audio\": false,
    \"prompt_extend\": false,
    \"watermark\": false,
    \"seed\": 0
  }
}
```

### Native Input Structure

Callers can pass native DashScope / Wan structures directly inside `extra_body`:

```json
{
  \"model\": \"cdp/wan3.0-video-prime\",
  \"extra_body\": {
    \"input\": {
      \"prompt\": \"Animate the historical illustration\",
      \"media\": [
        { \"type\": \"first_frame\", \"url\": \"https://example.com/first.png\" }
      ]
    },
    \"parameters\": {
      \"resolution\": \"1080P\",
      \"duration\": 5,
      \"audio\": false,
      \"seed\": 0
    }
  }
}
```

- When `extra_body.input` is supplied, it takes precedence over convenience root fields.
- `duration`: Must be an integer from 2 to 30, or -1 (inheriting source video duration). Defaults to 5.
- `resolution`: Defaults to `1080P` when neither explicit resolution nor `size` is provided.

---

## 3. Operational Policies

### Single Executor Attempt Policy

- Task submission enforces a single attempt policy to prevent duplicate task submissions and accidental billing from automatic retries or credential rotations.
- If task submission times out or returns an ambiguous network error, the task ID should be reconciled with the gateway before resubmitting.

### Polling & Cancellation

- Default background polling interval is 15 seconds.
- Non-terminal statuses (`UNKNOWN`, 429/5xx, transport drops) continue polling with the existing task ID.
- Upstream usage is extracted and preserved in task metadata under `metadata.wan` and artifact properties (e.g., `output_video_duration`).
- Upstream cancellation is not supported on the ChinaDataPay relay gateway; local cancellation stops status polling only.
