# Appliance update preparation

OMA-2016 adds `GET /api/v2/update` and `PUT /api/v2/update/image`. Uploading an
image validates and prepares it; it does not install it or restart the appliance.
`POST /api/v2/update`, connected downloads, and auto-update are owned by OMA-3031.

The endpoints are disabled by default pending OMA-2934. For local testing, opt in
with `agent run --update-enabled` or `AGENT_UPDATE_ENABLED=true`. The existing
`--authentication-enabled` flag authenticates outbound console requests; it does
not secure inbound appliance API requests. Do not enable updates in customer
deployments before the authentication blocker is resolved.

Upload exactly one multipart file in disconnected mode:

```sh
curl -k -X PUT https://localhost:3333/api/v2/update/image \
  -F file=@agent-image.tar
curl -k https://localhost:3333/api/v2/update
```

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

Choose the appliance's architecture. The prepared digest is the exported OCI
manifest digest, which can differ from the registry's Docker manifest digest.

The optional `min-upgrade-version` label is compared with the running appliance
version during preparation and startup recovery:

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

Preparation returns `200` with `state: "ready"`. Errors are `400` for invalid
input, `409` for connected mode, active collection/inspection, or another update
operation, `413` for size limits, `507` for insufficient storage, and `500` for
unexpected failures. Disabled endpoints return `503`.

Prepared archives and metadata live under `<DataFolder>/updates/.staging/`.
Incomplete `.part` files never become candidates. A failed replacement leaves
the previous prepared candidate usable. Startup verifies the persisted candidate
and removes orphaned temporary files. No preparation path publishes
`<DataFolder>/updates/*.tar`, the host's installation trigger.

GET returns the running version and configured mode, cached preparation state,
and the host-owned `updates/status.json`. Missing host status returns
`lastResult: null`; malformed or unreadable status returns `500`. Old host records
containing only `file`, `result`, and `time` remain supported. Host `applied`
confirms API readiness, not database compatibility or recovery.

Until OMA-3031, `updateAvailable` and `latestVersion` are `null`; `autoUpdate` is
`false` when connected and `null` when disconnected. GET does not contact SaaS or
revalidate the archive. Candidate metadata and host results persist across
restart; preparation errors are held in memory.
