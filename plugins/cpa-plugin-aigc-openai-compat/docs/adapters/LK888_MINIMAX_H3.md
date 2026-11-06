# LK888 MiniMax-H3

The `lk888-minimax-h3` adapter translates OpenAI-compatible video generation
requests to LK888's custom media protocol (`/v1/media/generate` and `/v1/media/status`).
It supports text-to-video, first/last frame generation, and multimodal reference
video generation across 768P, 1080P, 2K, and 4K resolutions.

## Zero backend configuration and fixed model matching

The plugin has **zero backend configuration**: no YAML configuration blocks
(`enabled`, `models`, `paths`) and no `DefaultConfig`/`SetConfig` runtime methods.
Model matching is code-owned in `internal/driver/driver.go` and matches:

- `lk/minimax-h3`
- `lk/MiniMax-H3`

The internal adapter name remains `lk888-minimax-h3`. Previously, `models` was
configured in plugin YAML blocks; all such YAML blocks are now obsolete and should
be removed.

The adapter uses fixed relative paths (`/media/generate` and `/media/status?task_id=<task_id>`)
which are appended to CPA's configured upstream credential BaseURL (`https://api.lk888.ai/v1`).
Upstream credentials remain entirely **host-owned**: configure the LK888 platform API key in
CPA's upstream OpenAI-compatible credentials with `base-url: https://api.lk888.ai/v1`. The adapter
emits `minimax-h3` as the upstream model identifier.

## Generation Request Examples

### 1. Text to Video

```json
{
  "model": "lk/minimax-h3",
  "prompt": "阳光洒在湖面上，一只白鹭缓缓掠过水面",
  "seconds": 5,
  "size": "1280x720",
  "extra_body": {
    "notify_url": "https://example.com/webhook"
  }
}
```

- `seconds` (or `extra_body.duration`): required integer between 4 and 15 seconds.
- `size`: mapped to resolution tier and aspect ratio:
  - up to 1,400,000 pixels: `768P` (e.g. `1280x720`, `1024x1024`)
  - up to 2,500,000 pixels: `1080P` (e.g. `1920x1080`)
  - up to 5,500,000 pixels: `2K` (e.g. `2560x1440`, `2048x2048`)
  - up to 10,000,000 pixels: `4K` (e.g. `3840x2160`)
- `extra_body.resolution`: explicit override (`768P`, `1080P`, `2K`, `4K`).
- `extra_body.aspect_ratio` (or `extra_body.ratio`): `adaptive`, `16:9`, `9:16`, `1:1`, `4:3`, `3:4`, `21:9`.

### 2. First and Last Frame (首尾帧)

```json
{
  "model": "lk/minimax-h3",
  "prompt": "镜头平稳推进，晨雾逐渐散开",
  "seconds": 6,
  "size": "1920x1080",
  "extra_body": {
    "first_frame": "https://example.com/first.jpg",
    "last_frame": "https://example.com/last.jpg"
  }
}
```

Or via `input_reference`:

```json
{
  "model": "lk/minimax-h3",
  "prompt": "镜头平稳推进，晨雾逐渐散开",
  "seconds": 6,
  "size": "1920x1080",
  "input_reference": [
    {
      "type": "image_url",
      "role": "first_frame",
      "image_url": { "url": "https://example.com/first.jpg" }
    },
    {
      "type": "image_url",
      "role": "last_frame",
      "image_url": { "url": "https://example.com/last.jpg" }
    }
  ]
}
```

The adapter automatically sets `mode: "shouweizhen"` and passes `images: [first, last]`.
Per platform rules, `last_frame` requires `first_frame` to be supplied.

### 3. Reference Media (参考生)

```json
{
  "model": "lk/minimax-h3",
  "prompt": "按照参考画面风格生成人物漫步夜市的场景",
  "seconds": 6,
  "size": "1920x1080",
  "input_reference": [
    {
      "type": "image_url",
      "role": "reference_image",
      "image_url": { "url": "https://example.com/ref1.jpg" }
    },
    {
      "type": "video_url",
      "role": "reference_video",
      "video_url": { "url": "https://example.com/ref.mp4" }
    },
    {
      "type": "audio_url",
      "role": "reference_audio",
      "audio_url": { "url": "https://example.com/ref.mp3" }
    }
  ]
}
```

The adapter automatically sets `mode: "cankaosheng"` and organizes items into:

- `image_url`: 0 to 9 reference images
- `video_url`: 0 to 3 reference videos
- `audio_url`: 0 to 3 reference audio clips

First/last frames and reference media are mutually exclusive.

## Task Lifecycle

- Submit path: `POST /media/generate` (appended to CPA's `https://api.lk888.ai/v1`)
  - Response: `{"code": 200, "data": {"task_id": 123456}, "msg": "..."}`
- Query path: `GET /media/status?task_id={task_id}` (appended to CPA's `https://api.lk888.ai/v1`)
  - States: `pending` (progress ~10%), `running` (progress ~50% or parsed from `progress` field), `success`, `failed`.
  - Artifact: on success, an `output_video` artifact is produced with storage provider `lk888`, MIME `video/mp4`, and URI from `result_url`.
- Cancellation: LK888 v1 custom media tasks cannot be cancelled upstream while in progress. Attempted cancellation reports that cancellation is not supported, and CPA applies local-only cancellation.
