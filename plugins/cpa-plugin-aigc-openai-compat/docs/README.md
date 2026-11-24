# AIGC OpenAI-Compatible Plugin Documentation

This directory is the documentation source of truth for `cpa-plugin-aigc-openai-compat`.

## Key Architecture Highlights

- **Zero Backend Configuration**: No YAML config blocks (`enabled`, `models`, `paths`), no `DefaultConfig`/`SetConfig`.
- **Adapter-Owned Prefix Routing**: Dedicated adapters declare their supported content kind and model prefixes (`Kind()`, `Prefixes()`). Routing is resolved via a per-kind Trie longest-prefix lookup (`driver/registry.go`) with zero runtime traversal, falling back to `openai-compat` for image and video requests with a non-empty model name.
- **Prefix Rules & Compatibility**: Registered prefixes strictly enforce full `vendor/model` format (no wildcards `*`/`?`, no bare models). Bare models and unmatched models fall through to `openai-compat`. Public `cdp/` is standard, legacy `chinadatapay/` aliases remain accepted, and internal adapter names are stable.
- **Provider-Based Response Parsing**: Submission, polling, and cancellation response parsing strictly resolves the owning adapter by `provider` in response metadata without probing other adapters.
- **Host-Owned Credentials**: Upstream credentials, BaseURLs, and API keys/tokens are managed entirely by CLIProxyAPI upstream channels.
- **Caller-Controlled Parameters**: `prompt_extend` (Qwen image/video, Wan) and `enable_thinking` (Qwen image only) in `extra_body` or the request root are passed through only where supported; omitted flags preserve upstream defaults.

---

## Documentation Index

### Core Architecture & Routing

- [Routing, Endpoints & CDP Migration](routing-and-endpoints.md) — Authoritative vendor endpoints, code-owned Trie routing order, parameter pass-through rules, and CDP prefix migration reference.

### Specialized Adapter Guides

- [Seedance Video Adapters](adapters/seedance.md) — Native Volcengine and ChinaDataPay Seedance routing, multimodal inputs, `asset://` IDs, and upstream `usage` metadata recording.
- [MiniMax-H3 Video Adapters](adapters/minimax-h3.md) — ChinaDataPay `/v2` and LK888 `/media` protocols, resolution tiers (768P-4K), first/last frame, reference media, and video regeneration.
- [Wan 3.0 Video Adapters](adapters/wan.md) — ChinaDataPay and DashScope Wan 3.0 video generation, single executor attempt policy, and task retry lifecycles.
- [OpenAI-Compatible Fallback Adapter](adapters/openaicompat.md) — Generic fallback for unmatched image and video requests, image operations (`generations`/`edits`/`variations`), and artifact extraction.

The plugin root [README.md](../README.md) provides the top-level developer quick start.
