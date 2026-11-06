# ChinaDataPay Wan 3.0

Use the public `cdp/` prefix; see [prefix migration](../migrations/CDP_PREFIX.md).

`chinadatapay-wan` and `qwen-wan` share `WanAdapter`, while retaining separate
routing, endpoints, authentication sources and lifecycle policies. The relay entry
only supports `wan3.0-video` and `wan3.0-video-prime` as upstream model IDs.

## Zero backend configuration and fixed model matching

The plugin has **zero backend configuration**: no YAML configuration blocks
(`enabled`, `models`, `paths`), no UI configuration, and no `DefaultConfig`/`SetConfig`
runtime methods. Model matching is code-owned in `internal/driver/driver.go` and matches:

- `cdp/wan3.0-video` (standard public prefix)
- `cdp/wan3.0-video-prime` (standard public prefix)
- `chinadatapay/wan3.0-video` (legacy prefix)
- `chinadatapay/wan3.0-video-prime` (legacy prefix)

The dedicated `chinadatapay-wan` adapter matches before the generic `openai-compat`
fallback. The internal adapter name remains `chinadatapay-wan`. Previously, `models` was
configured in plugin YAML blocks; all such YAML blocks are now obsolete and should
be removed.

The HTTPS gateway origin and `/api/v1` business paths are built into the adapter:
- Create: `POST https://tp-api.chinadatapay.com:8000/api/v1/services/aigc/video-generation/video-synthesis`
- Query: `GET https://tp-api.chinadatapay.com:8000/api/v1/tasks/{public_task_id}`

Upstream credentials remain entirely **host-owned**: keep CPA's existing upstream BaseURL
`https://tp-api.chinadatapay.com:8000/v1`. Configure the gateway Token in CPA's OpenAI-compatible upstream
credentials, and map the exposed model aliases to the upstream names without
`cdp/`. Do not supply the DashScope API key. The host sends Bearer auth;
the plugin does not store credentials. The gateway supplies the asynchronous header.

Creation and queries must use the same gateway account. Do not mix different
accounts in a credential pool for account-owned tasks; the plugin does not pin an
account. Absolute plugin URLs override the upstream credential BaseURL.

## OpenAI-style input

```json
{
  "model": "cdp/wan3.0-video",
  "prompt": "A cat running across a moonlit roof",
  "seconds": 5,
  "input_reference": [
    {
      "type": "image_url",
      "role": "first_frame",
      "image_url": { "url": "https://example.com/first.png" }
    }
  ],
  "extra_body": {
    "resolution": "480P",
    "ratio": "16:9",
    "audio": false,
    "prompt_extend": false,
    "watermark": false,
    "seed": 0
  }
}
```

Media may also be supplied as `extra_body.media` with Wan `type`/`url` entries.
Frames and reference/file/link entries are mutually exclusive. There can be at
most one first frame, one last frame, ten reference images, five reference videos
and five reference audio entries. Input video duration/codec/size restrictions
are still checked upstream; the plugin does not probe remote files.

## Native input and extensions

CPA keeps the existing top-level OpenAI validation. Put native Wan objects inside
`extra_body`, not at the request root:

```json
{
  "model": "cdp/wan3.0-video-prime",
  "extra_body": {
    "input": {
      "prompt": "Animate the scene",
      "media": [
        { "type": "first_frame", "url": "https://example.com/first.png" }
      ]
    },
    "parameters": {
      "resolution": "720P",
      "duration": -1,
      "audio": false,
      "seed": 0
    }
  }
}
```

Native `input` replaces convenience prompt/reference inputs entirely. Native
`parameters` fields take precedence over flat `extra_body` parameter fields, and
native/flat duration takes precedence over top-level `seconds`. Unknown native
input/media/parameter fields and other non-convenience `extra_body` fields are
preserved as upstream extensions.

Duration must be an integer from 2 through 30, or -1; it defaults to 5. Resolution
defaults to `1080P` when neither an explicit resolution nor OpenAI `size` is given.
Explicit resolution strings are preserved for exact gateway pricing matches;
custom tiers require gateway support. OpenAI `size` maps through the existing Wan
resolution mapper. Canonical metadata does not override native values or introduce
an unwanted `720P` default. Optional booleans/seed/ratio are omitted when absent;
explicit `false`, `seed: 0` and `duration: -1` are preserved.

Wan uses public URLs or supported Base64 media, not Ark `asset://` or MiniMax
`mm_file://` identifiers. The asset plugin skips both standard relay Wan aliases,
even with broad `cdp/*` patterns. For custom aliases, restrict the Ark
asset plugin's model patterns to Seedance to avoid accidental registration.

## Tasks, retries and cancellation

- Create: `POST /api/v1/services/aigc/video-generation/video-synthesis`.
- Query: `GET /api/v1/tasks/{public_task_id}`; only public `task_...` IDs are stored
  and used, and response IDs are checked against the requested ID.
- Default background polling is 15 seconds, with opt-in backoff after unsuccessful
  checks. An explicit host polling interval overrides the suggested interval.
- `UNKNOWN`, network failures, 429/5xx, invalid JSON and missing output keep the task
  nonterminal and retry the same task ID. Valid `FAILED` and `CANCELED` stop polling.
  Query HTTP 400/401/404 are terminal; do not recreate tasks automatically.
- Full responses and actual usage are preserved in task metadata under `wan`.
  Artifacts preserve usage and fractional duration, preferring
  `usage.output_video_duration` over `usage.duration`. No billing rules or gateway
  pricing configuration are changed.
- Creation opts into a single executor attempt. The host passes this policy to
  CPA AuthManager, preventing credential rotation, unauthorized refresh replay,
  outer retries and Home client redispatch after an execution error. Queries and
  other adapters retain existing retry behavior. This is a client-side policy;
  the Home server protocol does not change. Custom plugin executors/internal
  provider retries are not covered by this AuthManager policy.
- A creation error or timeout can still mean a task was created upstream. Do not
  blindly recreate it; reconcile with the gateway if no public task ID was received.
  Single-attempt execution is not upstream idempotency and does not guarantee
  exactly-once creation across a process crash or manual resubmission.
- No upstream cancellation API is provided. Existing local cancellation only stops
  tracking; it does not cancel billing or prove a refund. No callback, batch query
  or new permanent result-storage interface is added.

The existing background tracking timeout remains unchanged. On-demand queries from
clients are not throttled by the background loop. Download time-limited video URLs
promptly or use your existing configured artifact persistence plugin.

## Verification and deployment

Tests use mocked protocol responses and execution dispatchers, not paid APIs.
No database migration is required. Deploy the rebuilt host, AIGC plugin and asset
plugin together for polling, retry policy and asset-registration bypass. Validation
builds target the approved temporary directory and do not overwrite deployed binaries.
