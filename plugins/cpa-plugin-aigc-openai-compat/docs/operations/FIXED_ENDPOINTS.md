# Fixed vendor endpoints and code-owned model routing

Vendor-specific adapters and the generic fallback adapter have fixed API addresses
and code-owned routing patterns. The plugin operates with **zero backend configuration**:
there are no YAML configuration blocks (`enabled`, `models`, `paths`), and no
`DefaultConfig` or `SetConfig` runtime methods.

## Adapter-owned model prefixes and Trie routing

Model routing is adapter-owned and resolved using a **per-kind Trie longest-prefix lookup**
with zero runtime adapter traversal. Each dedicated adapter declares its content kind (`Kind()`)
and its registered model prefixes (`Prefixes()`). Prefix registration strictly enforces
full `vendor/model` prefix format (no wildcards `*`/`?`, no bare models). Bare models and
unregistered vendor prefixes fall through to the generic `openai-compat` fallback.

Endpoints, HTTP methods, and lifecycle support are derived directly from adapter implementations:

| Adapter (`provider`)      | Kind          | Adapter-Owned Model Prefixes                                                                                               | Creation (Submit)                                                                                                                                                              | Polling (Query)                                                                           | Cancellation                                                                |
| :------------------------ | :------------ | :------------------------------------------------------------------------------------------------------------------------- | :----------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | :---------------------------------------------------------------------------------------- | :-------------------------------------------------------------------------- |
| `qwen-image`              | Image         | `alibaba-cn/qwen-image`<br>`alibaba/qwen-image`<br>`qwen/qwen-image`<br>`alibaba-cn/wanx`<br>`alibaba/wanx`<br>`qwen/wanx` | `POST https://dashscope.aliyuncs.com/api/v1/services/aigc/multimodal-generation/generation`                                                                                    | `GET https://dashscope.aliyuncs.com/api/v1/tasks/{task_id}` _(if async task_id returned)_ | `POST https://dashscope.aliyuncs.com/api/v1/tasks/{task_id}/cancel`         |
| `qwen-wan`                | Video         | `alibaba-cn/wan`<br>`alibaba/wan`<br>`qwen/wan`                                                                            | `POST https://dashscope.aliyuncs.com/api/v1/services/aigc/video-generation/video-synthesis`                                                                                    | `GET https://dashscope.aliyuncs.com/api/v1/tasks/{task_id}`                               | `POST https://dashscope.aliyuncs.com/api/v1/tasks/{task_id}/cancel`         |
| `chinadatapay-wan`        | Video         | `cdp/wan3.0-video`<br>`cdp/wan3.0-video-prime`<br>`chinadatapay/wan3.0-video`<br>`chinadatapay/wan3.0-video-prime`         | `POST https://tp-api.chinadatapay.com:8000/api/v1/services/aigc/video-generation/video-synthesis`                                                                              | `GET https://tp-api.chinadatapay.com:8000/api/v1/tasks/{task_id}`                         | _Unsupported upstream_ _(returns error)_                                    |
| `volcengine-seedance`     | Video         | `volcengine/doubao-seedance`                                                                                               | `POST /contents/generations/tasks`                                                                                                                                             | `GET /contents/generations/tasks/{task_id}`                                               | `POST /contents/generations/tasks/{task_id}/cancel`                         |
| `chinadatapay-seedance`   | Video         | `cdp/doubao-seedance`<br>`chinadatapay/doubao-seedance`                                                                    | `POST https://tp-api.chinadatapay.com:8000/api/v3/contents/generations/tasks`                                                                                                  | `GET https://tp-api.chinadatapay.com:8000/api/v3/contents/generations/tasks/{task_id}`    | _Unsupported upstream_ _(returns error)_                                    |
| `chinadatapay-minimax-h3` | Video         | `cdp/minimax-h3`<br>`chinadatapay/minimax-h3`                                                                              | `POST https://tp-api.chinadatapay.com:8000/v2/video_generation`<br>_(Regeneration: `POST .../v2/video_regeneration`)_                                                          | `GET https://tp-api.chinadatapay.com:8000/v2/query/video_generation/{task_id}`            | `DELETE https://tp-api.chinadatapay.com:8000/v2/video_generation/{task_id}` |
| `lk888-minimax-h3`        | Video         | `lk/minimax-h3`<br>`lk888/minimax-h3`                                                                                      | `POST /media/generate`                                                                                                                                                         | `GET /media/status?task_id={task_id}`                                                     | _Unsupported upstream_ _(returns error)_                                    |
| `volcengine-seedream`     | Image         | `volcengine/doubao-seedream`                                                                                               | `POST /images/generations`                                                                                                                                                     | `GET /contents/generations/tasks/{task_id}` _(async fallback)_                            | `POST /contents/generations/tasks/{task_id}/cancel`                         |
| `openai-compat`           | Image / Video | Unmatched models & bare models _(see fallback filter rules below)_                                                         | Image Generation: `POST /images/generations`<br>Image Edit: `POST /images/edits`<br>Image Variation: `POST /images/variations`<br>Video Generation: `POST /videos/generations` | `GET /videos/generations/{task_id}` _(video only)_                                        | `POST /videos/generations/{task_id}/cancel` _(video only)_                  |

## Adding an adapter

Built-in adapters register their factory from each vendor adapter package's `init()` function.
The entrypoint `main.go` and test suites blank-import the vendor adapter packages so those
initializers are linked and run. To add another adapter within an already linked package, add its
factory registration beside its constructor. If it lives in a new Go package, add its
blank import to `main.go`. `NewDriver` and its route table do not need an adapter-specific edit.

Each factory name must match the constructed adapter's `Name()`. Dedicated adapters must
implement `Kind()` and `Prefixes()`; the unique fallback must implement `Supports()` and
register with `Fallback: true`. Model prefixes remain declared by each adapter and are
validated when each `Driver` instance builds its route registry.

The `openai-compat` adapter does **not** match arbitrary models. In `Supports()`:

1. **Empty models**: Rejects empty or whitespace-only model strings.
2. **Unsupported content kinds**: Rejects any non-image, non-video generation kind (e.g. audio).
3. **Cross-kind keyword filtering**:
   - For `Image`: Rejects models containing known video keywords (`video`, `seedance`, `sora`, `kling`, `cogvideo`, `luma`, `runway`).
   - For `Video`: Rejects models containing known image keywords (`image`, `seedream`, `dall-e`, `flux`, `recraft`, `midjourney`, `wanx`).
4. **Preceding dedicated match**: Requests matching earlier dedicated adapters never reach the fallback.

## Request parameters

Parameters are passed to upstream vendor requests **only where actually supported**:

- **`prompt_extend`** (Image and Video): Passed to DashScope/gateway parameters by `qwen-image`, `qwen-wan`, and `chinadatapay-wan`. Can be provided via `extra_body.prompt_extend` or at the request root. `qwen-image` additionally accepts `prompt_extend_mode` (`extra_body.prompt_extend_mode` or root).
- **`enable_thinking`** (Image only): Passed to DashScope parameters exclusively by `qwen-image`. Can be provided via `extra_body.enable_thinking` or at the request root. It is **not** supported on video models (`qwen-wan`, `chinadatapay-wan`, etc.) or other image adapters.
- **Omitted parameters**: When absent from the caller's request, neither parameter is synthesized or defaulted by the plugin; they are omitted from the outgoing upstream payload so upstream defaults remain in effect.
- Other adapters (`volcengine-seedance`, `chinadatapay-seedance`, `chinadatapay-minimax-h3`, `lk888-minimax-h3`, `volcengine-seedream`, `openai-compat`) do not accept or forward these parameters.

## Upstream credentials and endpoints

Upstream credentials remain entirely **host-owned**:

- CPA upstream channel configuration provides BaseURLs, API keys, Tokens, proxy settings, and authentication headers (`Authorization: Bearer <Token>`).
- Relative paths (Volcengine, LK888, and generic OpenAI-compatible paths) are appended to CPA's configured credential BaseURL. For LK888, CPA's upstream BaseURL is configured as `https://api.lk888.ai/v1`, and polling queries `GET /media/status?task_id={task_id}`.
- Absolute vendor URLs (ChinaDataPay, DashScope) bypass BaseURL concatenation via existing internal `custom_endpoint` execution metadata.
- Public prefix `cdp/` is standard; legacy `chinadatapay/` patterns remain fully accepted.
- Internal adapter names (`chinadatapay-seedance`, `chinadatapay-minimax-h3`, `chinadatapay-wan`, etc.) remain stable so existing and persisted tasks continue polling without interruption.

## Historical context and removed plugin settings

Earlier versions of this plugin used YAML configuration blocks with per-adapter settings:

- `models`: previously used to configure model lists per adapter.
- `endpoint` / `base-url`: previously used to override vendor addresses (e.g., `qwen-image.endpoint`, `qwen-wan.endpoint`, `volcengine-seedance.endpoint`, `volcengine-seedream.endpoint`, `chinadatapay-minimax-h3.base-url`, `chinadatapay-wan.base-url`).
- `paths`: previously used to configure generic `openai-compat` submission, edit, and poll paths.
- `enabled`: previously used to enable or disable individual sub-adapters.

All such YAML blocks and configuration keys are now obsolete and have been removed. The plugin operates with zero backend configuration, resolving routing and endpoints deterministically from code. Any obsolete plugin YAML blocks in deployment configs can be safely deleted.

No database migration, pricing change, historical-task rewrite or upstream
submission is performed by this architecture. Existing tasks retain their provider
identity and task IDs. Verification uses fixtures and existing local HTTP executor
tests; it does not call paid APIs or overwrite deployed binaries.
