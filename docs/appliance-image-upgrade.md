# Appliance image upload and upgrade

OMA-2016 adds image uploads that start a disconnected appliance image upgrade, plus
image upgrade status in the existing agent-status response. Connected downloads
are described at the end of this document.

The image upgrade service is initialized at startup without a feature flag. The upload
endpoint and image upgrade status are always available; the UI decides whether to
expose image upgrade controls.

Upload exactly one multipart file in disconnected mode. A successful upload
starts the upgrade; there is no separate confirmation or apply request:

```sh
curl -k -X POST https://localhost:3333/api/v2/image-upgrades \
  -F file=@agent-image.tar
```

The agent validates the entire upload and atomically renames the complete
archive from `.staging/<uuid>.part` to
`updates/<uuid>.tar` on the same filesystem. Only then does it return
`202 Accepted`, with no response body. The host updater watches that directory
and performs the upgrade asynchronously. Partial or invalid uploads never
appear in the watched directory.

The maximum archive payload is 3 GiB; multipart framing has a separate 1 MiB
limit. Preparation requires three times the archive size in free space on the
data filesystem. The archive must contain one OCI image for Linux and the
running agent architecture, with `org.opencontainers.image.version` set.
Descriptors and all layer content digests are validated. Digest checks establish
integrity, not publisher authenticity.

Each layer must contain a valid tar archive using its declared compression
(uncompressed, gzip, or zstd). Validation reads complete compression streams to
check their trailers and checksums. All layers together are limited to 16 GiB
of expanded tar data, including headers and padding, and one million entries.
The zstd decoder accepts windows up to 64 MiB. Sparse entries are rejected in
both the outer archive and its layers. Validation is cancellable and does not
extract filesystem entries.

Konflux publishes Docker schema-2 images. Export them to the required OCI layout
and OCI media types before uploading (`docker save` is not supported):

```sh
skopeo copy --override-os linux --override-arch amd64 --format oci \
  docker://<appliance-image-reference> oci-archive:agent-image.tar
```

Choose the appliance's architecture. The validated digest is the exported OCI
manifest digest, which can differ from the registry's Docker manifest digest.

The required `io.kubev2v.oma.min-upgrade-version` label is compared with the
running appliance version during upload validation:

| Versions being compared | Comparison |
|-------------|------------|
| Both production (`vX.Y.Z`) | Compare major, minor, and patch numerically; the running version must meet or exceed the minimum. |
| Either version is dev (`main-release-X.Y-<sha>`) or stable / stage (`stable-release-X.Y-<sha>`) | Compare only major and minor numerically across channels; ignore patches and SHAs. Equal or newer releases are accepted. A higher major version satisfies the minimum even if its minor version is lower. |

Version parsing follows the planner: numeric components may have leading zeros;
branch SHAs must be exactly seven lowercase hexadecimal digits. Compatibility
for non-production comparisons is defined at major/minor release granularity.
SHAs are validated for format but are ignored in the comparison and are never
sorted lexically.
Missing or empty minimums, invalid versions (including `unknown`, `DEV`, and
`v0.0.0`), and PR versions are rejected. Channel differences do not prevent
otherwise compatible versions from passing the minimum gate.

The [export regression fixture](../internal/services/testdata/image_upgrade/README.md)
uses published Konflux metadata and the real skopeo exporter with a tiny layer.

Upload errors are `400` for invalid input, `409` for connected mode, another
upload in progress, or a published archive still pending host resolution,
`413` for size limits, `507` for insufficient storage, and `500` for unexpected
failures. Unavailable services and uploads interrupted by cancellation or a
deadline return `503`.

Uploads are serialized while receiving and validating an archive. The request
finishes after placing the file in the watched directory. Collection deletion,
mode changes, and ongoing jobs retain their existing behavior. The host owns
archive processing, service shutdown, migrations, and recovery.

Temporary files live under `<DataFolder>/updates/.staging/`. Startup removes
abandoned `.part` files. Published archives remain in `<DataFolder>/updates/`
until the host resolves the upgrade. While a published `*.tar` remains in that
directory, new uploads return `409` before staging or validating another image.
This check also applies after an agent restart. Once the host removes the archive
or moves it out of the watched directory, another upload can be accepted.
Staged files and `status.json` do not block admission; an old host result is
not treated as an active upgrade.

The existing `GET /api/v2/agent` response always includes an `imageUpgradeStatus`
object in connected and disconnected modes. It
remains available while connecting or when the console is unreachable, so the UI
can show the result of a disconnected upgrade. The existing `mode` and
`consoleConnection` fields describe connectivity; image upgrade status does not
duplicate them. There is no separate image-upgrade status endpoint.

`imageUpgradeStatus` separates the agent's upload activity from the last host
upgrade result:

```json
{
  "currentVersion": "v0.31.0",
  "upload": { "state": "idle", "lastError": null },
  "upgrade": {
    "file": "image.tar",
    "outcome": "applied",
    "time": "2026-11-03T00:00:00Z"
  },
  "upgradeError": null
}
```

`upload.state` and `upload.lastError` describe agent upload activity and upload
failures. `upgrade` contains the last host result, or null when no result is
available. `upgradeError` reports a generic host-result read error independently
of upload failures; it is null when no read error occurred. A malformed host
result therefore leaves `upload.state` and `upload.lastError` unchanged and
returns `upgrade: null` with `upgradeError` set. If the upload state itself is
unavailable, `upload.state` is `"unknown"`. Agent status still returns `200` so
the UI can open.

The host-owned `updates/status.json` format is unchanged. Old host records with
only `file`, `result`, and `time` remain supported. The API maps `result` to
`outcome`, and maps `error_stage`, `running_image`, and `last_successful_update`
to `errorStage`, `runningImage`, and `lastSuccessfulUpdate`. The UI SDK must be
regenerated for the new response shape.

After a successful upload, the host records the result in
`updates/status.json`; once the agent is reachable again, the UI can read it as
`imageUpgradeStatus.upgrade` through `GET /api/v2/agent`. This includes the outcome
(`applied`, `rolled-back`, `failed`, or `unchanged`), timestamp, and any error and
failure stage. The result is the last outcome recorded by the host and may
still describe an earlier upgrade until the host writes a new record.

`upload.state` describes upload activity: `uploading`, `validating`, or `idle` after a
request finishes. An idle upload endpoint can coexist with host upgrade work.
`unknown` reports that the upload state is unavailable without blocking the UI.
GET only reads local state and the host result; it does not contact SaaS,
execute an upgrade, or validate existing application data. Upload errors are
held in memory; host results persist across restart.

## Connected mode download

In connected mode the agent gets the image from the console instead of an
upload (OMA-3031). Updates start only when the user asks; there is no automatic
update.

```sh
curl -k https://localhost:3333/api/v2/image-upgrades/latest
curl -k -X POST https://localhost:3333/api/v2/image-upgrades/latest
```

`GET /api/v2/image-upgrades/latest` calls the planner's
`GET /api/v1/sources/{id}/image-upgrade-status` on every request; nothing is
cached. It returns `updateAvailable`, `latestVersion`, `supported`, and `reason`.
`supported: false` with `reason: "new-ova-required"` means the appliance is too
old for an in-place update and needs a new OVA. Errors are `409` in disconnected
mode and `503` when the console is unreachable.

`POST /api/v2/image-upgrades/latest` checks the console again to get a fresh
presigned URL, opens the download, and returns `202`. The download then runs in
the background through the same staging and validation as an upload, so failed,
corrupted, or unsupported downloads never reach the watched directory.
`upload.state` is `downloading` while the image streams in, then `validating`,
then `idle`; failures go to `upload.lastError`. Downloads and uploads share one
lock and the pending-archive check. Errors are `409` in disconnected mode, when
no update is available, when the update is not supported in place, or when a
download, upload, or published archive is pending. `503` means the console is
unreachable or has not staged the image yet.

An interrupted download is not resumed: the partial file is removed and the user
starts again. A download is limited to two hours.
