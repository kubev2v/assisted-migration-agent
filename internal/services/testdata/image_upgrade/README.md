# Konflux export fixture

`konflux-oci.tar` is a real `skopeo copy --format oci ... oci-archive:...`
export, generated with skopeo 1.24.1. Tests read the committed archive directly;
they do not need skopeo, Podman, a registry connection, or an appliance image.

The platform and labels in `konflux-config.json` were captured on 2026-11-03
from the published Konflux appliance:

```text
quay.io/redhat-user-workloads/assisted-migration-tenant/oma-appliance:main-release-0.32-1e77135
sha256:5ec84cd0408b1a60485e8214fe4dbdc224a700c30fbc9146875cef761641059c
```

The registry image uses Docker schema 2. Exporting with `--format oci` converts
its manifest/config/layer media types to OCI. This fixture keeps the published
linux/amd64 platform and update labels, substituting a tiny valid filesystem
layer and matching rootfs/history for the appliance's approximately 1.5 GiB of
layers. It tests the export format and compatibility metadata, rather than the
contents or bootability of the complete appliance. Its manifest digest differs
from the registry image's digest because of conversion and substituted content.

Regenerate from this directory with:

```sh
python3 generate.py
```

The generator builds a local Docker archive, then runs the real skopeo exporter.
To verify a full appliance export manually on a matching appliance architecture:

```sh
skopeo copy --override-os linux --override-arch amd64 --format oci \
  docker://quay.io/redhat-user-workloads/assisted-migration-tenant/oma-appliance:main-release-0.32-1e77135 \
  oci-archive:/tmp/agent-image.tar
curl -k -X POST https://localhost:3333/api/v2/image-upgrades -F file=@/tmp/agent-image.tar
```

Use a compatible running version (for example `main-release-0.31-212e974`).
The endpoint must be enabled and the appliance must be disconnected.

## Sparse archive fixtures

`sparse-pax-0.1.tar` and `sparse-pax-1.0.tar` contain a regular entry encoded
with GNU sparse PAX metadata. Each stores one byte but declares a 1 PiB logical
size. Tests verify rejection before hashing expands the holes and confirm that
the upload slot and staging files are released afterward.

Regenerate these fixtures with `python3 generate_sparse.py`. Do not extract them.
