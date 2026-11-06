# Asset registration channels

Public `cdp/` aliases and upgrade compatibility:
[CDP prefix migration](../cpa-plugin-aigc-openai-compat/CDP_PREFIX.md).

This plugin registers HTTP(S) media references before an AIGC task is created,
replaces their URLs with `asset://<ID>`, and checks asset readiness before submission.
It does not change video generation adapters or expose an asset management API.

## Configuration

The following YAML is the plugin's configuration payload, not the complete CPA configuration:

```yaml
volcengine:
  enabled: true
  access-key: "<AK>"
  secret-key: "<SK>"
  group-id: "<group ID>"
  # project: default
  # region: cn-beijing
  # models: ["volcengine/doubao-seedance-*"]

chinadatapay:
  enabled: true
  token: "<platform Token>"
  # group-id: "<optional group ID>"
  # models: ["cdp/doubao-seedance-*"]

# Optional: these are the shared defaults.
runtime:
  poll-interval-ms: 1200
  poll-timeout-seconds: 60
  cache-ttl-seconds: 86400
```

Use the deployment's secret-management mechanism for credentials; do not commit them.
Client instances are constructed on initialization/reconfiguration. Each client owns
its authentication. Volcengine uses AK/SK signing and the SDK's standard regional
Ark endpoint. ChinaDataPay uses `Authorization: Bearer <platform Token>` and the
fixed endpoint `https://tp-api.chinadatapay.com:8000/v1/api/assets/`, with
`Action=CreateAsset` or `Action=GetAsset` and `Version=2024-01-01` query parameters.
ChinaDataPay has no endpoint, BaseURL, AK/SK, Project, or Region settings. Requests
do not carry `model` or `ProjectName`; the server selects the channel and project
from the Token identity.

Volcengine sends `ProjectName` in `CreateAsset` and `GetAsset`, and `GroupId` in
`CreateAsset`. Its Project defaults to `default`; AK, SK, and GroupId are required.
ChinaDataPay requires only Token. Its `group_id` is optional: when omitted, the
plugin omits `GroupId` and ordinary assets enter the server's default group. When
specified, `GroupId` is sent only in `CreateAsset`; `GetAsset` sends only `Id`.
A Project identifier is **not** an asset GroupId. For real-person
assets, provision the authorized group through the H5 verification workflow first;
the plugin does not create verification sessions, groups, or management APIs.
Each configured channel uses its configured group, or ChinaDataPay's default group.
Do not use
one real-person group for different people; it must match the authorized identity.

ChinaDataPay asset-library authentication is configured independently in this plugin.
Use a platform Token belonging to the account used for video generation. Generation
continues using CPA's upstream Bearer credentials and its unchanged `/v1` BaseURL.

Omit a channel block to leave that channel disabled. A present block is enabled by
default; set `enabled: false` to disable it without supplying credentials. Model
patterns default to `volcengine/doubao-seedance-*` and
`cdp/doubao-seedance-*`, respectively, rather than matching all models. Legacy
prefixes remain compatible with the same configured suffix patterns.
Matching is case-insensitive. Overlapping configured patterns fail explicitly.
Incomplete enabled-channel settings fail initialization/reconfiguration. A failed
reconfiguration leaves the active router untouched; there is no fallback to another
account. Removed and unknown YAML fields are rejected without echoing their values.
The host's top-level `enabled`, `priority`, and `store` instance settings are
accepted during registration/reconfiguration, but do not participate in channel
authentication, routing, or cache scopes. Keep channel settings in their blocks.

### Configuration migration

- Move legacy top-level `access_key`, `secret_key`, `group_id`, `project`, `region`,
  and `models` into `volcengine`.
- Move the three top-level polling/cache settings into `runtime`.
- Replace ChinaDataPay AK/SK settings with `token`. Delete `access_key`, `secret_key`,
  `project`, `region`, and any `endpoint`; only the documented Bearer Action endpoint
  is supported. Keep `group_id` only if a specific target group is needed.
- Preserve explicit `models` overrides if needed; omitted patterns now target
  Seedance only. Standard MiniMax-H3 and Wan aliases always bypass Ark registration.

Removed keys are not silently ignored. ChinaDataPay has no AK/SK compatibility mode.

## Adapter structure

`router.go` selects one `AssetAdapter` by model. `client_volcengine.go` and
`client_chinadatapay.go` construct independent adapters. `client_ark.go` owns the
Volcengine SDK protocol; ChinaDataPay owns its Bearer Action transport. Both return
the same channel-neutral asset and error types. `mutator.go` receives only model and
runtime settings, never account credentials, and owns reference conversion and
readiness waiting. `store.go` owns the scoped cache.

To add a channel, implement `AssetAdapter.CreateAsset/GetAsset`, normalize responses
to `AssetItem` and safe retryable `AssetAPIError` values, then add its typed configuration
and constructor in the router. The shared lifecycle does not need vendor branches.
There is intentionally no dynamic registration framework or multi-account list.

## Media references

The plugin recognizes `input_reference`, image/video/audio URL fields, first/last frames,
and reference image lists, including references inside `extra_body.content`.
Unrelated URLs such as prompts and callbacks are left unchanged. Explicit media types
take precedence over URL extensions. Prefer typed references for extensionless URLs:

```json
{
  "input_reference": [
    {
      "type": "video_url",
      "video_url": { "url": "https://example.com/video" }
    },
    { "type": "audio_url", "audio_url": { "url": "https://example.com/audio" } }
  ]
}
```

Bare HTTP references in `input_reference` become typed objects so that replacement
with an asset URI does not lose their media type. Existing asset URIs are not registered
again; they must belong to the selected channel account.

Data URIs must first be converted to public URLs by `data-uri-to-tos`. Configure the
mutator execution order accordingly. This plugin does not upload Base64 media itself.
The default polling interval is 1.2 seconds and can be overridden through `runtime`.
Polling uses exponential backoff for HTTP 429/5xx and transport errors. Terminal API
errors fail immediately. Neither adapter automatically retries failed creation;
the Volcengine SDK's automatic retries remain disabled. Both respect the lifecycle
context and reject redirects to avoid forwarding credentials. ChinaDataPay response
bodies are limited to 1 MiB; HTTP and platform-envelope errors never echo raw
responses or Token values.

## Cache compatibility

New registrations use `channel_assets`, keyed by channel/account scope and a hash of
media type plus URL. Account scope is hashed, and credentials are never persisted in
plaintext. Volcengine scope includes channel, AK, signing region, Project, and GroupId;
same-account SK rotation does not change scope. ChinaDataPay scope includes the
fixed Action endpoint, normalized Token, and optional GroupId. Changing its Token
or target group creates a new scope. Pending tasks with a different
recorded scope are rejected instead of being submitted through a different account.
Restore their original account configuration rather than recreating video tasks.
Existing AK/SK gateway scopes are not imported into Bearer scopes. Direct Volcengine
scope calculation is unchanged for the same account/region/project/group.

The table is created lazily on first cache access; the host database account needs
CREATE TABLE permission, or an operator must provision the schema from `schema_channel.go`.
The legacy `volcengine_assets` table is left intact. Legacy cache entries are not
automatically imported because their account ownership cannot be established; expect
new registrations after upgrading. No existing data is deleted.

## Verification

```sh
go test ./...
go test -race ./...
go vet ./...
go build ./...
```

Tests use mock clients and a local HTTP server, not live upstream credentials.
