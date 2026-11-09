# CPA Plugin AIGC OpenAI-Compatible

The plugin implements CLIProxyAPI's `aigc.ContentGenerationDriver` and routes image/video generation requests through specialized vendor adapters, with a generic OpenAI-compatible fallback.

## Architecture and zero backend configuration

The plugin requires **zero backend configuration**:
- No YAML configuration blocks (`enabled`, `models`, `paths`, etc.).
- No `DefaultConfig` or `SetConfig` runtime configuration calls.
- **Adapter-owned model prefix registration**: Dedicated adapters declare their supported content kind (`Kind()`) and model prefixes (`Prefixes()`). Requests are routed via a per-kind Trie longest-prefix lookup with zero adapter traversal on requests.
- **Full vendor/model prefix format**: Prefixes enforce full `vendor/model` format (no wildcards `*`/`?`, no bare models). Bare models and unmatched models fall through to `openai-compat` (which rejects empty models and cross-kind known names, rather than accepting arbitrary input).
- Public prefix `cdp/` is standard for ChinaDataPay models, while legacy `chinadatapay/` patterns remain fully accepted.
- Internal adapter names (`qwen-image`, `qwen-wan`, `chinadatapay-wan`, `volcengine-seedance`, `chinadatapay-seedance`, `chinadatapay-minimax-h3`, `lk888-minimax-h3`, `volcengine-seedream`, `openai-compat`) remain stable for task locking and metadata persistence.
- **Strict response parsing**: Submit, poll, and cancel response parsing resolves the adapter by `provider` in response metadata only, returning errors on missing or unknown provider without probing other adapters.
- Upstream credentials (API keys, Tokens, BaseURLs) remain entirely **host-owned** in CLIProxyAPI host configurations.
- Optional request parameters are passed only where supported: `prompt_extend` on Qwen image/video and Wan adapters, `enable_thinking` on Qwen image only (via `extra_body` or request root); when absent, they are omitted so upstream vendor defaults remain untouched.
- The generic image adapter accepts and forwards explicit `output_format`, `frame_count`, `frame_duration_ms`, `complexity`, and `background` fields for compatible upstreams, including GPT Image 2.6. Frame controls are intended only for GIF requests; callers must omit them for static images. No new defaults are injected, and upstreams retain responsibility for model-specific value/range and pixel-budget validation. GIF/SVG artifact MIME types are recognized from URL paths and decoded image content; MIME detection is not SVG sanitization.
- Synchronous image handlers supply the trusted `image_operation` metadata (`generations` or `edits`). The generic adapter follows this operation instead of inferring a different endpoint from image fields: generation requests with references and edit requests without prompt/references are rejected. Legacy asynchronous/direct driver callers without this metadata keep their existing routing behavior. Deploy the updated CPA host and plugin together, and coordinate callers to use the explicit edits endpoint.
- The host's non-streaming OpenAI-compatible edits executor converts GPT Image 2.6, Sunburst and Flare JSON reference URLs to multipart `image[]` files (including an optional mask). Other models and existing multipart bodies are unchanged. Reference downloads use a credential-free, public-address-only client with DNS pinning and redirect checks, support PNG/JPEG/WebP and bounded data URIs, and enforce 16 references and a 32 MiB aggregate file limit. Conversion precedes the existing final payload configuration; converted binary request bodies are omitted from upstream request logs.

## Documentation

All detailed documentation lives in [`docs/`](docs/README.md):
- [Documentation Index](docs/README.md)
- [Fixed Endpoints and Model Routing](docs/operations/FIXED_ENDPOINTS.md)
- [CDP Prefix Migration](docs/migrations/CDP_PREFIX.md)
- [ChinaDataPay Seedance Adapter](docs/adapters/CHINADATAPAY.md)
- [ChinaDataPay MiniMax-H3 Adapter](docs/adapters/MINIMAX_H3.md)
- [ChinaDataPay Wan 3.0 Adapter](docs/adapters/WAN_GATEWAY.md)
- [LK888 MiniMax-H3 Adapter](docs/adapters/LK888_MINIMAX_H3.md)
- [Seedance Usage Metadata](docs/operations/SEEDANCE_USAGE.md)

## Build and test

Run from this plugin directory:

```bash
go test ./...
make build
```
