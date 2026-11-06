# ChinaDataPay MiniMax-H3

Use the public `cdp/` prefix; see [prefix migration](../migrations/CDP_PREFIX.md).

The `chinadatapay-minimax-h3` adapter implements the supplied gateway `/v2`
contract. It supports video generation, regeneration, single-task polling and
upstream cancellation. Context understanding (`h3_context_ir`), upstream task
listing, terminal-task deletion and callback reception are not added.

## Zero backend configuration and fixed model matching

The plugin has **zero backend configuration**: no YAML configuration blocks
(`enabled`, `models`, `paths`), no UI configuration, and no `DefaultConfig`/`SetConfig`
runtime methods. Model matching is code-owned in `internal/driver/driver.go` and matches:

- `cdp/MiniMax-H3` (standard public prefix)
- `chinadatapay/MiniMax-H3` (legacy prefix)

The internal adapter name remains `chinadatapay-minimax-h3`. Previously, `models` was
configured in plugin YAML blocks; all such YAML blocks are now obsolete and should
be removed.

The gateway origin and `/v2` business paths are built into the adapter:

- Creation: `POST https://tp-api.chinadatapay.com:8000/v2/video_generation`
- Query: `GET https://tp-api.chinadatapay.com:8000/v2/query/video_generation/<task_id>`
- Cancel: `DELETE https://tp-api.chinadatapay.com:8000/v2/video_generation/<task_id>`
- Regeneration: `POST https://tp-api.chinadatapay.com:8000/v2/video_regeneration`

Upstream credentials remain entirely **host-owned**: configure the ChinaDataPay platform
Token and BaseURL `https://tp-api.chinadatapay.com:8000/v1` in CPA's upstream
OpenAI-compatible credentials, not in the plugin. Map the exposed alias `cdp/MiniMax-H3`
to the case-sensitive upstream ID `MiniMax-H3`. The adapter always emits that exact
model ID. Requests use absolute gateway URLs.

Task IDs are gateway IDs beginning with `task_`. Keep creation, queries,
regeneration and cancellation on the same account; this adapter does not pin
credentials or implement cross-account fallback.

## Generation

Use the existing CPA OpenAI-style video input:

```json
{
  "model": "cdp/MiniMax-H3",
  "prompt": "An orange cat resting in sunlight",
  "seconds": 6,
  "size": "1366x768",
  "input_reference": [
    {
      "type": "image_url",
      "role": "first_frame",
      "image_url": { "url": "https://example.com/first.jpg" }
    }
  ],
  "extra_body": {
    "aigc_watermark": false
  }
}
```

`seconds` (or `extra_body.duration`) must explicitly specify an integer from 4
through 15. Standard `size` (`WIDTHxHEIGHT`, also accepting `X` or `*`) supplies
the H3 resolution and aspect ratio. The adapter maps up to 1,400,000 pixels to
`768P`, and larger sizes up to 5,500,000 pixels to `2K`, using the existing HD
and QHD pixel ceilings. For example, `1280x720` and `1366x768` select `768P`;
`1920x1080` and `2560x1440` select `2K`. This is a tier selection, not a promise
of exact upstream output dimensions. Larger sizes and malformed dimensions are
rejected when deriving the resolution. `extra_body.size` is a fallback.

Native `extra_body.resolution` (`768P` or `2K`) and `extra_body.ratio` take
precedence over values derived from size, matching the gateway's native override
convention. Resolution or size is required; no default resolution is assumed.
The shared canonical tier mapper is not used because it collapses 2K to 4K.
Ratio is omitted only when neither size nor a native ratio is supplied;
watermark is omitted when absent;
explicit `false` is preserved. Client-supplied `callback_url` parameters
are ignored for security; vendor webhook callbacks are managed internally
by the plugin runtime when configured.

Native content is accepted in `extra_body.content`. It takes precedence over
`prompt` and `input_reference` and must contain a nonempty text item. Media use
`image_url`, `video_url`, or `audio_url` objects containing a string `url`, with
roles from the gateway contract. First/last frames cannot mix with `reference_*`
roles. References can use public URLs, `mm_file://` identifiers or Data URIs.
`extra_body.first_frame` and `extra_body.last_frame` are URL-string conveniences.

H3 does not support Ark `asset://` identifiers. The Ark asset registration router
skips the standard `cdp/MiniMax-H3` alias, even under broad relay patterns.
If you configure another H3 alias, restrict the Ark asset plugin's model patterns
to Seedance to avoid accidental registration. Keep the previously implemented
asset-plugin skip in the deployed asset plugin binary as well.

## Regeneration

```json
{
  "model": "cdp/MiniMax-H3",
  "size": "2560x1440",
  "extra_body": {
    "task_type": "regeneration",
    "source_task_id": "task_original"
  }
}
```

Provide exactly one of `source_task_id` or content. Regeneration accepts
size-derived resolution only if it selects `2K`; `768P` is never automatically
upgraded for regeneration. Native `extra_body.resolution: "2K"` remains supported.
Content mode may use
`extra_body.content`, or a prompt plus references, and additionally accepts a
video with `role: base_video`. Do not provide `seconds`, duration or watermark
for regeneration. The adapter sends `POST /v2/video_regeneration` with no duration.
Account ownership, task retention and source-task eligibility remain upstream checks.

## Task lifecycle

- Query: `GET /v2/query/video_generation/{task_id}`.
- Cancel: `DELETE /v2/video_generation/{task_id}`.
- `queued`/`running` stay running; `succeeded`/`failed`/`cancelled` are terminal.
- Results come from `task.content.url`. Missing output on a successful task is
  treated as an invalid response, not a completed task with no video.
- Failure code/message are preserved; absent details use the documented placeholders.
- Actual task fields and usage are preserved in task metadata under `minimax_h3`.
  Video artifacts also retain returned usage/duration/resolution/ratio. Absent
  fields are not synthesized. This does not add or change CPA/Portal billing rules.
- HTTP 400/401/404 query failures are terminal for this adapter. HTTP 429/5xx and
  transport failures do not falsely complete/fail the task; existing CPA execution
  retry settings apply to execution. H3 background polling backs off after failed
  checks from the default three seconds to 6, 12, 24 and at most 30 seconds, then
  resets after a successful check. Other adapters retain their original cadence.
  On-demand queries made by clients are not rate-limited by this background loop.

For H3, the host requires upstream cancellation confirmation. An upstream failure,
missing executor, invalid task identifier, or response that says `deleted` rather than
`cancelled` must not mark the local running task cancelled. Successfully cancelling
upstream does not itself prove that a refund has been settled. Other adapters retain
local-only cancellation when upstream cancellation is unsupported, but actual
execution/parser failures now propagate rather than being silently ignored.

## Deployment and verification

No database migration is required. Deploy the rebuilt host, AIGC plugin and asset
plugin together for cancellation confirmation, metadata persistence and the H3
asset-registration bypass. Verification builds use temporary output paths and
do not overwrite deployed binaries. Tests use fixtures and mocks, not paid APIs.
