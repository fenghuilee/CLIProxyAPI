# AIGC OpenAI-Compatible Plugin Documentation

This directory is the documentation source of truth for the plugin. Start with the guide that matches your task:

## Key architecture highlights

- **Zero backend configuration**: No YAML config blocks (`enabled`, `models`, `paths`), no `DefaultConfig`/`SetConfig`.
- **Adapter-owned prefix routing**: Dedicated adapters declare their supported content kind and model prefixes (`Kind()`, `Prefixes()`). Routing is resolved via per-kind Trie longest-prefix matching with zero request traversal, falling back to `openai-compat` (which performs cross-kind and empty-model validation).
- **Prefix rules & compatibility**: Registered prefixes strictly enforce full `vendor/model` format (no wildcards `*`/`?`, no bare models). Bare models fall back to `openai-compat`. Public `cdp/` is standard, legacy `chinadatapay/` aliases remain accepted, and internal adapter names are stable.
- **Provider-based response parsing**: Submission, polling, and cancellation response parsing strictly resolves the owning adapter by `provider` in response metadata without probing other adapters.
- **Host-owned credentials**: Upstream credentials, BaseURLs, and API keys/tokens are managed entirely by CLIProxyAPI upstream channels.
- **Caller-controlled parameters**: `prompt_extend` (Qwen image/video, Wan) and `enable_thinking` (Qwen image only) in `extra_body` or the request root are passed through only where supported; omitted flags preserve upstream defaults.

## Adapter guides

- [ChinaDataPay Seedance](adapters/CHINADATAPAY.md) — fixed routing, request format, and task compatibility.
- [ChinaDataPay MiniMax-H3](adapters/MINIMAX_H3.md) — generation, regeneration, polling, and cancellation.
- [ChinaDataPay Wan 3.0](adapters/WAN_GATEWAY.md) — request formats, task lifecycle, and retry behavior.
- [LK888 MiniMax-H3](adapters/LK888_MINIMAX_H3.md) — fixed routing, media inputs, and task lifecycle.

## Operations and compatibility

- [Fixed endpoints and routing](operations/FIXED_ENDPOINTS.md) — authoritative vendor endpoints, code-owned routing order, and zero-config reference.
- [Seedance usage metadata](operations/SEEDANCE_USAGE.md) — persisted upstream usage fields; billing is unchanged.
- [CDP model-prefix migration](migrations/CDP_PREFIX.md) — public aliases, legacy prefix compatibility, and code-owned matching.

The root [README](../README.md) is the plugin overview and quick start. Adapter-specific guides describe behavior not covered by the shared endpoint and routing reference.
