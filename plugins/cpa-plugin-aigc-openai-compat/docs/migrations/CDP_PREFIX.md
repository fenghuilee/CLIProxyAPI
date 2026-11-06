# CDP public model prefix

Use `cdp/` in public model IDs and API examples:

- `cdp/doubao-seedance-2.0-aicc`
- `cdp/wan3.0-video`
- `cdp/MiniMax-H3`

The plugins also support the existing Seedance variants and Wan Prime under the
new prefix. This does not enable models in the host registry or change billing,
permissions, model suffixes, upstream credentials, or endpoint URLs.

## Current state: zero backend configuration and code-owned model matching

The plugin now has **zero backend configuration**: no YAML configuration blocks
(`enabled`, `models`, `paths`) and no `DefaultConfig`/`SetConfig` runtime methods.
All model routing patterns are code-owned in `internal/driver/driver.go`, which serves
as the single source of truth.

Both the public `cdp/` prefix and legacy `chinadatapay/` prefix are declared directly by
the respective adapters and registered in the driver's per-kind prefix Trie:

- `chinadatapay-seedance`:
  - `cdp/doubao-seedance`
  - `chinadatapay/doubao-seedance`
- `chinadatapay-minimax-h3`:
  - `cdp/minimax-h3` (case-insensitive, matching `cdp/MiniMax-H3`)
  - `chinadatapay/minimax-h3` (case-insensitive, matching `chinadatapay/MiniMax-H3`)
- `chinadatapay-wan`:
  - `cdp/wan3.0-video`
  - `cdp/wan3.0-video-prime`
  - `chinadatapay/wan3.0-video`
  - `chinadatapay/wan3.0-video-prime`

The internal adapter names remain stable (`chinadatapay-seedance`, `chinadatapay-minimax-h3`,
`chinadatapay-wan`) so that persisted tasks continue polling through their original locked
adapter (`aigc_adapter` in task metadata). Upstream credentials (API tokens and BaseURLs)
remain host-owned in CPA upstream credentials.

### Historical context and migration note

Earlier releases configured these patterns via plugin YAML configuration blocks
(`chinadatapay-seedance.models`, `chinadatapay-minimax-h3.models`, `chinadatapay-wan.models`).
With the transition to zero backend configuration, all such YAML blocks are obsolete.
Existing YAML files do not need these blocks; removing them causes no disruption because
code-owned matching inherently recognizes both `cdp/` and `chinadatapay/` prefixes.

Seedance and Wan strip the first vendor segment before sending the remainder as
the upstream model ID. The execution request model stays the full public ID for
host credential and alias matching. H3 continues to send `MiniMax-H3`. New video and tail-frame artifacts use
`storage_provider: cdp`, including results from older tasks queried after upgrade.

## Asset plugin configuration

Rebuild and load `cpa-plugin-volcengine-asset` as well. Its internal channel key,
credentials, and cache/account scopes are unchanged:

```yaml
chinadatapay:
  enabled: true
  token: "<platform Token>"
  models: ["cdp/doubao-seedance-*"]
```

Legacy model patterns automatically recognize their CDP counterparts. H3 and Wan
aliases bypass Ark asset registration under either prefix, including when an
administrator configures a broad `cdp/*` or legacy wildcard.

## Rollout and verification

1. Build and deploy both plugins together before switching callers to `cdp/`.
2. Keep the host's upstream model mappings aligned with the new public prefix.
3. Update GVA model bindings separately; this source change does not write them.
4. Keep historical routing records intact. This change does not rewrite old task
   model IDs, upstream error text, metadata, or media URLs. Public presentation
   and proxy error sanitization are separate work if those expose vendor details.

Run `go test ./... -count=1` from each plugin module. Tests use fixtures and local
HTTP servers, not paid generation calls. Build verification must not overwrite
deployed plugin binaries.
