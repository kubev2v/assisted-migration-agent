# Building the forecaster filler assets on macOS (Podman)

This document explains how to (re)generate the forecaster filler files:

- `pkg/vmware/assets/alpine-filler.raw.gz` (~40 MB) — bootable Alpine disk image
- `pkg/vmware/assets/seed.iso.gz` (~1 KB) — cloud-init seed ISO (kept for compatibility)

These files are produced by [`scripts/build-filler-image.sh`](../scripts/build-filler-image.sh) and
consumed at runtime by [`pkg/vmware/filler_image.go`](../pkg/vmware/filler_image.go) through the
`AGENT_ASSETS_DIR` environment variable.

## Why a container?

The `build-filler-image.sh` script needs Linux tools that are not available (or painful to install) on
macOS:

- `qemu-img`
- `guestfish` (from `guestfs-tools` / `libguestfs`)
- `genisoimage` / `mkisofs` (provided by `xorriso`)
- `curl`, `gzip`, `tar`, `bash`

The solution is to build a **linux/amd64** Podman image based on UBI9 that bundles all these tools, then
run the script inside the container. We build this image once, tag it `localhost/filler-builder:local`
(RHEL/UBI 9.8, ~5.8 GB), and reuse it for every asset regeneration.

> **Architecture note**: the image is `linux/amd64`. On an Apple Silicon Mac (arm64), Podman runs it
> under emulation. `guestfish` boots a small QEMU appliance in software mode (TCG — no KVM on macOS),
> so generation is slow (a few minutes) but works. This is expected.

## Prerequisites

- Podman installed and the Podman machine running:

  ```sh
  podman machine list        # should show a machine "Currently running"
  podman machine start       # if needed
  ```

## Step 1 — Build the builder image (once)

The builder image does not exist yet — create it. (Once built, `podman images | grep filler` will show
`localhost/filler-builder:local` and you can skip straight to step 2 on subsequent runs.)

Create a `Containerfile.filler` at the repo root:

```dockerfile
FROM registry.access.redhat.com/ubi9/ubi:latest

# guestfs-tools / libguestfs-appliance / qemu are not in the UBI repos:
# add the CentOS Stream 9 repos to pull them.
RUN cat > /etc/yum.repos.d/centos-stream-baseos.repo <<'EOF' && \
    cat > /etc/yum.repos.d/centos-stream-appstream.repo <<'EOF2'
[centos-stream-baseos]
name=CentOS Stream 9 - BaseOS
baseurl=https://mirror.stream.centos.org/9-stream/BaseOS/x86_64/os/
gpgcheck=1
gpgkey=https://www.centos.org/keys/RPM-GPG-KEY-CentOS-Official-SHA256
enabled=1
EOF
[centos-stream-appstream]
name=CentOS Stream 9 - AppStream
baseurl=https://mirror.stream.centos.org/9-stream/AppStream/x86_64/os/
gpgcheck=1
gpgkey=https://www.centos.org/keys/RPM-GPG-KEY-CentOS-Official-SHA256
enabled=1
EOF2

RUN dnf install -y \
        libguestfs \
        libguestfs-appliance \
        guestfs-tools \
        qemu-img \
        xorriso \
        curl \
        gzip \
        tar \
        bash \
    && dnf clean all

# guestfish runs without a libvirt daemon inside the container: direct backend.
ENV LIBGUESTFS_BACKEND=direct

COPY scripts/build-filler-image.sh /tmp/build-filler-image.sh

CMD ["/bin/bash"]
```

> `xorriso` provides the `genisoimage` and `mkisofs` compatibility commands the script expects.
> `LIBGUESTFS_BACKEND=direct` is required: there is no libvirt daemon in the container.

Build the image, forcing the amd64 platform:

```sh
podman build --platform linux/amd64 -t localhost/filler-builder:local -f Containerfile.filler .
```

## Step 2 — Generate the assets

From the root of the `assisted-migration-agent` repo:

```sh
podman run --rm \
  --platform linux/amd64 \
  -e SKIP_BOOT_TEST=1 \
  -e FILLER_OUTPUT_DIR=/out \
  -v "$(pwd)/pkg/vmware/assets:/out:Z" \
  localhost/filler-builder:local \
  bash /tmp/build-filler-image.sh
```

Option breakdown:

- `--platform linux/amd64` — the image and the guestfs appliance are x86_64.
- `SKIP_BOOT_TEST=1` — step 7 of the script (a QEMU boot test) needs `qemu-system-x86_64`, which is
  **not** installed in the builder image. Skipping it does not affect the generated files.
- `FILLER_OUTPUT_DIR=/out` — the script writes its outputs to this directory (read at line 18 of the
  script).
- `-v .../assets:/out:Z` — mount the destination folder so the files land back on the host.

The script (see [`build-filler-image.sh`](../scripts/build-filler-image.sh)) will:

1. Download the Alpine virt 3.21.7 ISO + syslinux + openssl from the Alpine mirrors
2. Create the cloud-init seed ISO
3. Extract kernel/initramfs and APK packages from the ISO
4. Prepare a minimal Alpine rootfs (with the `fill-disk.start` script that fills the disk at boot)
5. Build the raw disk image (partition, ext4 format, syslinux)
6. Compress into `alpine-filler.raw.gz` and `seed.iso.gz`

When it finishes, check:

```sh
ls -lh pkg/vmware/assets/
# alpine-filler.raw.gz   (~40 MB)
# seed.iso.gz            (~1 KB)
```

## Step 3 — Run the agent with these assets

At runtime the agent reads the assets from `AGENT_ASSETS_DIR` (default: `/app/assets`, see
[`filler_image.go`](../pkg/vmware/filler_image.go)). Locally, point it at the generated folder:

```sh
AGENT_ASSETS_DIR=pkg/vmware/assets make run
```

## Alternative without Podman

If `qemu-img`, `guestfish` and `genisoimage`/`mkisofs` are installed directly (Linux, or macOS via
Homebrew), the Make target works without a container:

```sh
make build-filler-image
# = FILLER_OUTPUT_DIR=$(CURDIR)/pkg/vmware/assets bash scripts/build-filler-image.sh
```

On macOS the container approach is recommended because `guestfish`/`libguestfs` are not properly
available there.

## Troubleshooting

| Symptom | Cause / fix |
|---------|-------------|
| `ERROR: guestfish required` | Tool missing — use the container image (step 1). |
| `qemu-system-x86_64: not found` at the end of the run | Boot test (step 7); add `SKIP_BOOT_TEST=1`. |
| Very slow generation | Expected: amd64 emulation + guestfs in TCG (no KVM on macOS). |
| `WARNING: image platform (linux/amd64) does not match ... arm64` | Expected on Apple Silicon, harmless. |
| Files not written on the host | Check the `-v .../assets:/out:Z` mount and `FILLER_OUTPUT_DIR=/out`. |
