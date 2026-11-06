# ChinaDataPay Seedance channel

Use the public `cdp/` prefix; see [prefix migration](../migrations/CDP_PREFIX.md).

The `chinadatapay-seedance` entry uses the same Seedance protocol implementation as
`volcengine-seedance`. The entries have separate model patterns, model prefixes,
endpoints, and provider identities. No database migration is required.

## Zero backend configuration and fixed model matching

The plugin has **zero backend configuration**: no YAML configuration blocks
(`enabled`, `models`, `paths`) and no `DefaultConfig`/`SetConfig` runtime methods.
Model matching is code-owned in `internal/driver/driver.go` and matches:

- `cdp/doubao-seedance-*` (standard public prefix)
- `chinadatapay/doubao-seedance-*` (legacy prefix)

The internal adapter name remains `chinadatapay-seedance`. Previously, `models` was
configured in plugin YAML blocks; all such YAML blocks are now obsolete and should
be removed.

The adapter has one built-in task collection URL for both creation and queries:

```text
POST https://tp-api.chinadatapay.com:8000/api/v3/contents/generations/tasks
GET  https://tp-api.chinadatapay.com:8000/api/v3/contents/generations/tasks/<task ID>
```

Endpoints and URLs are protocol constants, not configuration options. There is no
`endpoint`, `poll-endpoint`, `base-url`, or `models` plugin configuration. The adapter returns
the complete URL; the existing host passes it through the internal `custom_endpoint`
execution metadata, which bypasses BaseURL concatenation. Direct Volcengine behavior remains unchanged.
Existing submitted tasks can continue polling with their original task IDs after
loading the rebuilt plugin; they do not need to be recreated. Relay cancellation is unsupported;
CPA's existing local cancellation does not guarantee upstream cancellation or stop billing.

## Upstream credentials

Configure a CPA OpenAI-compatible upstream channel with the ChinaDataPay platform
Token as its API key, the existing BaseURL
`https://tp-api.chinadatapay.com:8000/v1`, and model mappings from the exposed
`cdp/doubao-seedance-*` aliases to the actual upstream model IDs.
The executor supplies `Authorization: Bearer <Token>`; this plugin has no Token field.
Do not change that BaseURL to `/api/v3`. Creation and polling use the built-in full
URL while authentication still comes from the selected credential. They do not
produce `/v1/api/v3/...` or `/v1/contents/...`. No executor changes are needed.

Use the same relay account for asset registration and generation. Do not pool
credentials from different accounts for requests containing account-owned asset IDs.
The plugin does not pin credentials to an account or add cross-account fallback.

## Input

CPA retains its OpenAI-style video contract. Use `prompt`, `seconds`, `size`,
`input_reference`, and `extra_body`. Native Seedance `content` and other parameters
belong in `extra_body`, not at the request root:

```json
{
  "model": "cdp/doubao-seedance-2-5",
  "prompt": "Create a new video using these references",
  "input_reference": [
    {
      "type": "video_url",
      "video_url": { "url": "asset://asset-video" },
      "role": "reference_video"
    },
    {
      "type": "audio_url",
      "audio_url": { "url": "asset://asset-audio" },
      "role": "reference_audio"
    }
  ],
  "extra_body": {
    "duration": -1,
    "ratio": "adaptive",
    "resolution": "720p",
    "omni_reference_task_type": "reference",
    "generate_audio": false,
    "priority": 0
  }
}
```

Model IDs must be enabled by the relay. Typed references preserve video/audio identity
even for extensionless URLs and `asset://` URIs. `extra_body.content` also supports
text, image/video/audio URL entries, frame roles, and `draft_task` entries.
Use explicit `ratio: adaptive` for Seedance 2.5 frame/edit/extend tasks and
`duration: -1` for edit tasks. The plugin retains existing canonical parameter
normalization rather than duplicating model-specific upstream validation rules.

## Lifecycle compatibility

The host forwards request metadata to response parsers and saves the selected
sub-adapter in task metadata as `aigc_adapter`. The task's `provider` remains the
host plugin ID. The internal adapter name `chinadatapay-seedance` remains stable
so tasks already locked to it keep using the correct parser across polling cycles.
Existing tasks without this metadata retain the code-owned matching behavior.

Tests use protocol fixtures and local HTTP servers; no paid upstream requests are
made. On the current host, only the rebuilt AIGC plugin needs to be loaded for this
address change; full-URL execution and metadata-based task locking already exist.
Verification builds do not overwrite deployed plugin binaries.
