# OpenAI-Compatible Fallback Adapter (`openai-compat`)

The `openai-compat` adapter is the plugin's generic fallback engine. It handles all image and video generation requests that do not match dedicated vendor prefixes in the Trie.

---

## 1. Admission and Role

- **Fallback Provider ID**: `openai-compat`
- **Supported Content Kinds**: `aigc.ContentKindImage` and `aigc.ContentKindVideo`
- **Matching Precedence**: Lowest priority. Any request whose prefix matches a dedicated adapter (`qwen-*`, `chinadatapay-*`, `volcengine-*`, `lk888-*`) is routed to that adapter first.
- **Kind-Driven Admission**: Relies strictly on caller `req.Kind` and non-empty `Model`. It does not perform string-based modal filtering on the model name.

---

## 2. Image Pipeline (`prepareImageSubmit`)

### Endpoint Routing Strategy

The adapter determines the target endpoint through explicit operations or implicit payload inspection:

1. **Explicit Operation** (`metadata.image_operation`):
   - `generations`: Routes to `POST /images/generations`. Disallows reference images or masks (returns validation error if present).
   - `edits`: Routes to `POST /images/edits`. Requires both a `prompt` and reference images.
2. **Implicit Routing** (for legacy or direct callers without operation metadata):
   - Has images + prompt \(\to\) `POST /images/edits`
   - Has images + no prompt \(\to\) `POST /images/variations`
   - Prompt only \(\to\) `POST /images/generations`

### Payload Building and Parameter Normalization

- **Images and Masks**:
  - For edits: Reference images are formatted as `images: [{"image_url": "..."}]`. An optional `mask` is formatted as `mask: {"image_url": "..."}`.
  - For generations: Packed as root `image` or `images`.
- **Dimensions**: Dimensions in `size` are normalized to standard `WIDTHxHEIGHT` format.
- **Standard Controls**: Forwards `n`, `quality`, `style`, `seed`, `response_format`, and `negative_prompt`.
- **Advanced Format Controls**: Explicitly passes through `output_format`, `frame_count`, `frame_duration_ms`, `complexity`, and `background` (supporting animated outputs like GIFs or advanced models such as GPT Image 2.6).

---

## 3. Video Pipeline (`prepareVideoSubmit`)

- **Fixed Endpoint**: `POST /videos/generations`
- **Requirements**: Requires a `prompt` or at least one reference image.
- **Frame Role Mapping**:
  - Image 1 is mapped to `image_url` (first frame).
  - Image 2 is mapped to `last_frame_image_url` (tail frame).
- **Video Controls**: Forwards `duration` (normalized from `seconds` or `duration`), `fps`, `size`, `ratio` (auto-derived from `size`), `quality`, and `seed`.

---

## 4. Response Parsing and Lifecycle

### Submission Parsing (`ParseSubmit`)

- **Direct Synchronous Output**: If the response contains immediate image/video artifacts (`data[].url` or `data[].b64_json`), the task transitions immediately to `StatusSucceeded`.
- **Asynchronous Task Recognition**: Scans for task identifiers (`id`, `task_id`, `video_id`, `task.id`). If found and the status is running/pending, returns `StatusRunning` with `ProviderTaskID` for background polling.

### Polling (`PreparePoll` / `ParsePoll`)

- **Poll URL**: `GET /videos/generations/{task_id}`
- **Status Mapping**:
  - `completed`, `succeeded`, `success`, `done` \(\to\) `StatusSucceeded` with extracted artifacts.
  - `failed`, `error` \(\to\) `StatusFailed`.
  - `canceled`, `cancelled` \(\to\) `StatusCanceled`.
  - `queued`, `pending` \(\to\) `StatusRunning` with 10% progress.
  - Other running states \(\to\) `StatusRunning` with progress extracted from response (default 50%).

### Task Cancellation (`PrepareCancel`)

- **Cancel URL**: `POST /videos/generations/{task_id}/cancel`

---

## 5. Artifact Extraction

The helper `extractOpenAICompatArtifacts` handles diverse gateway response formats:

1. Standard OpenAI array: `data[].url` and `data[].b64_json`.
2. Gateway fallback fields: `output.0.uri`, `output.0.url`, `output.video_url`, `result.video_url`.
3. MIME Type Detection: Detects `video/mp4` when URLs contain `.mp4` or `video`; defaults to `output_image` with proper image MIME detection otherwise.
