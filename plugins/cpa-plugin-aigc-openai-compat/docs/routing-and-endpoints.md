# Fixed Endpoints, Prefix Routing, and CDP Migration

This document provides the authoritative reference for model routing, vendor endpoints, upstream parameter conventions, and prefix migration for `cpa-plugin-aigc-openai-compat`.

## 1. Architecture and Zero Backend Configuration

The plugin operates with **zero backend configuration**:

- **No YAML configuration blocks**: There are no `enabled`, `models`, or `paths` blocks required or supported for individual adapters.
- **No runtime config calls**: The plugin does not implement `DefaultConfig` or `SetConfig`.
- **Adapter-owned model prefix registration**: Dedicated adapters declare their supported content kind (`Kind()`) and model prefixes (`Prefixes()`). Requests are routed via a per-kind Trie longest-prefix lookup implemented in `driver/registry.go` with zero adapter traversal per request.
- **Strict prefix format**: Prefixes enforce the full `vendor/model` format (no wildcards `*`/`?`, no bare models). Bare models and unmatched models fall through to the `openai-compat` fallback adapter.
- **Stable internal adapter names**: Adapter names (`qwen-image`, `qwen-wan`, `chinadatapay-wan`, `volcengine-seedance`, `chinadatapay-seedance`, `chinadatapay-minimax-h3`, `lk888-minimax-h3`, `volcengine-seedream`, `openai-compat`) remain stable across versions for task locking (`aigc_adapter` in task metadata) and metadata persistence.
- **Strict response parsing**: Submit, poll, and cancel response parsing resolves the adapter strictly by `provider` in response metadata, returning an error on missing or unknown providers without probing other adapters.
- **Host-owned credentials**: Upstream API keys, platform tokens, and base URLs remain entirely owned and managed by CLIProxyAPI host configurations.

---

## 2. Authoritative Endpoints and Routing Table

| Adapter (`provider`)      | Kind          | Adapter-Owned Model Prefixes                                                                                               | Submit (Create)                                                                                                                              | Poll (Query)                                                                           | Cancel                                                                      |
| :------------------------ | :------------ | :------------------------------------------------------------------------------------------------------------------------- | :------------------------------------------------------------------------------------------------------------------------------------------- | :------------------------------------------------------------------------------------- | :-------------------------------------------------------------------------- |
| `qwen-image`              | Image         | `alibaba-cn/qwen-image`<br>`alibaba/qwen-image`<br>`qwen/qwen-image`<br>`alibaba-cn/wanx`<br>`alibaba/wanx`<br>`qwen/wanx` | `POST https://dashscope.aliyuncs.com/api/v1/services/aigc/multimodal-generation/generation`                                                  | `GET https://dashscope.aliyuncs.com/api/v1/tasks/{task_id}` _(if async)_               | `POST https://dashscope.aliyuncs.com/api/v1/tasks/{task_id}/cancel`         |
| `qwen-wan`                | Video         | `alibaba-cn/wan`<br>`alibaba/wan`<br>`qwen/wan`                                                                            | `POST https://dashscope.aliyuncs.com/api/v1/services/aigc/video-generation/video-synthesis`                                                  | `GET https://dashscope.aliyuncs.com/api/v1/tasks/{task_id}`                            | `POST https://dashscope.aliyuncs.com/api/v1/tasks/{task_id}/cancel`         |
| `chinadatapay-wan`        | Video         | `cdp/wan3.0-video`<br>`cdp/wan3.0-video-prime`<br>`chinadatapay/wan3.0-video`<br>`chinadatapay/wan3.0-video-prime`         | `POST https://tp-api.chinadatapay.com:8000/api/v1/services/aigc/video-generation/video-synthesis`                                            | `GET https://tp-api.chinadatapay.com:8000/api/v1/tasks/{task_id}`                      | _Unsupported upstream_ _(returns error)_                                    |
| `volcengine-seedance`     | Video         | `volcengine/doubao-seedance`                                                                                               | `POST /contents/generations/tasks`                                                                                                           | `GET /contents/generations/tasks/{task_id}`                                            | `POST /contents/generations/tasks/{task_id}/cancel`                         |
| `chinadatapay-seedance`   | Video         | `cdp/doubao-seedance`<br>`chinadatapay/doubao-seedance`                                                                    | `POST https://tp-api.chinadatapay.com:8000/api/v3/contents/generations/tasks`                                                                | `GET https://tp-api.chinadatapay.com:8000/api/v3/contents/generations/tasks/{task_id}` | _Unsupported upstream_ _(returns error)_                                    |
| `chinadatapay-minimax-h3` | Video         | `cdp/minimax-h3`<br>`chinadatapay/minimax-h3`                                                                              | `POST https://tp-api.chinadatapay.com:8000/v2/video_generation`<br>_(Regeneration: `POST .../v2/video_regeneration`)_                        | `GET https://tp-api.chinadatapay.com:8000/v2/query/video_generation/{task_id}`         | `DELETE https://tp-api.chinadatapay.com:8000/v2/video_generation/{task_id}` |
| `lk888-minimax-h3`        | Video         | `lk/minimax-h3`<br>`lk888/minimax-h3`                                                                                      | `POST /media/generate`                                                                                                                       | `GET /media/status?task_id={task_id}`                                                  | _Unsupported upstream_ _(returns error)_                                    |
| `volcengine-seedream`     | Image         | `volcengine/doubao-seedream`                                                                                               | `POST /images/generations`                                                                                                                   | `GET /contents/generations/tasks/{task_id}` _(async fallback)_                         | `POST /contents/generations/tasks/{task_id}/cancel`                         |
| `openai-compat`           | Image / Video | Unmatched models & bare models _(fallback)_                                                                                | Image: `POST /images/generations`<br>Edit: `POST /images/edits`<br>Variation: `POST /images/variations`<br>Video: `POST /videos/generations` | `GET /videos/generations/{task_id}` _(video only)_                                     | `POST /videos/generations/{task_id}/cancel` _(video only)_                  |

---

## 3. Fallback Adapter (`openai-compat`) Admission Rules

The `openai-compat` adapter serves as the single generic fallback. It does not match arbitrary requests; its `Supports()` check enforces:

1. **Non-empty Model**: Rejects empty or whitespace-only model identifiers.
2. **Supported Content Kinds**: Accepts only `ContentKindImage` and `ContentKindVideo`. Requests for other kinds (such as audio) return unsupported.
3. **Dedicated Prefix Precedence**: Requests whose prefixes match any dedicated adapter in the Trie are handled by that adapter and never reach the fallback.
4. **Explicit Kind Enforcement**: Content kind is driven strictly by caller request type (`req.Kind`). The adapter does not attempt to infer or reject modalities based on model name substrings, allowing arbitrary custom or fine-tuned model identifiers to be passed through transparently.

---

## 4. Parameter Pass-Through Conventions

Parameters are forwarded to upstream vendor requests **only where natively supported**:

- **`prompt_extend`** (Image and Video): Passed to DashScope/gateway parameters by `qwen-image`, `qwen-wan`, and `chinadatapay-wan`. Accepted via `extra_body.prompt_extend` or at the request root. `qwen-image` additionally accepts `prompt_extend_mode`.
- **`enable_thinking`** (Image only): Passed to DashScope parameters exclusively by `qwen-image`. Accepted via `extra_body.enable_thinking` or at the request root. It is omitted on video models and non-Qwen adapters.
- **Omitted parameters**: When absent from the caller request, neither parameter is synthesized or defaulted by the plugin; they are omitted from the outgoing payload to preserve upstream vendor defaults.
- **Image output and animation controls** (Image only): The `openai-compat` adapter passes explicit `output_format`, `frame_count`, `frame_duration_ms`, `complexity`, and `background` fields (e.g. for GPT Image 2.6 and animated formats). Frame parameters are intended only for animations/GIFs and must be omitted for static images.

---

## 5. CDP Public Model Prefix Migration

Public model aliases standardized on the `cdp/` prefix:

- `cdp/doubao-seedance-2.0-aicc`
- `cdp/doubao-seedance-2-5`
- `cdp/wan3.0-video`
- `cdp/wan3.0-video-prime`
- `cdp/MiniMax-H3`

### Compatibility Rules

1. **Legacy Prefix Preservation**: The legacy `chinadatapay/` prefix remains fully recognized by the prefix Trie.
2. **Upstream Model Stripping**: Seedance and Wan strip the vendor prefix segment (`cdp/` or `chinadatapay/`) and transmit the remainder as the upstream model name. H3 preserves the exact casing `MiniMax-H3`.
3. **Artifact Provider Tagging**: Generated video and image artifacts from CDP adapters record `storage_provider: "cdp"`.
4. **Asset Plugin Coordination**: When using `cpa-plugin-volcengine-asset`, Wan and MiniMax-H3 models bypass Ark asset registration under both `cdp/` and `chinadatapay/` prefixes.
