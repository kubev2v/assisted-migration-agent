# Appliance image upload and upgrade

OMA-2016 adds image uploads that start a disconnected appliance update, plus
update status in the existing agent-status response. Connected downloads and
automatic updates are owned by OMA-3031.

The update service is initialized at startup without a feature flag. The upload
endpoint and update status are always available; the UI decides whether to
expose update controls.

Upload exactly one multipart file in disconnected mode. A successful upload
starts the upgrade; there is no separate confirmation or apply request:

```sh
curl -k -X PUT https://localhost:3333/api/v2/update/image \
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

Konflux publishes Docker schema-2 images. Export them to the required OCI layout
and OCI media types before uploading (`docker save` is not supported):

```sh
skopeo copy --override-os linux --override-arch amd64 --format oci \
  docker://<appliance-image-reference> oci-archive:agent-image.tar
```

Choose the appliance's architecture. The validated digest is the exported OCI
manifest digest, which can differ from the registry's Docker manifest digest.

The optional `min-upgrade-version` label is compared with the running appliance
version during upload validation:

| Environment | Comparison |
|-------------|------------|
| Production (`vX.Y.Z`) | Numeric semver ordering; the running version must meet or exceed the minimum. |
| Dev (`main-release-X.Y-<sha>`) | Numeric major/minor ordering within `main`. Equal or newer releases are accepted regardless of SHA; older releases are rejected. |
| Stable / stage (`stable-release-X.Y-<sha>`) | The dev rule within `stable`. |

Version formats match `agent/Containerfile`: no leading zeros; branch SHAs are
7–40 lowercase hex digits. Compatibility is defined at release granularity:
different SHAs within the same release pass. SHAs are validated for format but
are ignored in the comparison and are never sorted lexically.
Malformed minimums, channel mismatches, and unknown running versions (`DEV`,
PR builds) are rejected when a nonempty minimum is supplied. An omitted or empty
minimum permits older images without compatibility metadata.

The [export regression fixture](../internal/services/testdata/update/README.md)
uses published Konflux metadata and the real skopeo exporter with a tiny layer.

Upload errors are `400` for invalid input, `409` for connected mode or another
upload in progress, `413` for size limits, `507` for insufficient storage, and
`500` for unexpected failures. Disabled uploads return `503`.

Uploads are serialized while receiving and validating an archive. The request
finishes after placing the file in the watched directory. Collection deletion,
mode changes, and ongoing jobs retain their existing behavior. The host owns
archive processing, service shutdown, migrations, and recovery.

Temporary files live under `<DataFolder>/updates/.staging/`. Startup removes
abandoned `.part` files. Published archives remain in `<DataFolder>/updates/`
for the host to consume. The agent does not persist upgrade attempts or wait
for a host result before accepting another upload.

The existing `GET /api/v2/agent` response always includes an `updateStatus`
object in connected and disconnected modes. It
remains available while connecting or when the console is unreachable, so the UI
can show the result of a disconnected upgrade. The existing `mode` and
`consoleConnection` fields describe connectivity; update status does not
duplicate them. There is no separate `/api/v2/update` resource.

`updateStatus` contains the running version, current upload state, and the
host-owned `updates/status.json`. Missing host status returns `lastResult: null`.
Malformed or unreadable update status is reported as `state: "unknown"` and a
generic `lastError` inside `updateStatus`; agent status still returns `200` so the
UI can open. Old host records containing only `file`, `result`, and `time` remain
supported. The UI SDK must be regenerated to expose the new field.

After a successful upload, the host records the result in
`updates/status.json`; once the agent is reachable again, the UI can read it as
`updateStatus.lastResult` through `GET /api/v2/agent`. This includes the outcome
(`applied`, `rolled-back`, `failed`, or `unchanged`), timestamp, and any error and
failure stage. The result is the last outcome recorded by the host and may
still describe an earlier upgrade until the host writes a new record.

`state` describes upload activity: `uploading`, `validating`, or `idle` after a
request finishes. An idle upload endpoint can coexist with host upgrade work.
`unknown` reports an update-status read error without blocking the UI.
GET only reads local state and the host result; it does not contact SaaS,
execute an upgrade, or validate existing application data. Upload errors are
held in memory; host results persist across restart.

Connected downloads and automatic update settings belong to OMA-3031.
