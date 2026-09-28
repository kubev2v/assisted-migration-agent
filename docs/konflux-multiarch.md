# Multi-architecture Konflux builds

Konflux builds one image index containing `linux/amd64` and `linux/arm64`. Linux clients can keep
using the existing image repository and tags; the container runtime selects the image for the host
architecture.

## Image behavior

| Build trigger | Published tag |
| --- | --- |
| Pull request | `on-pr-<revision>` (expires after 5 days) |
| Push to `main` | `<revision>` and `latest` |
| Push to `stable` | `<revision>` and `stable` |
| Git tag | `<git_tag>` |

The AMD64 image keeps the appliance and standalone RVTools flows. ARM64 keeps the UI, OPA policies,
backend, and ARM64 DuckDB extension for standalone RVTools. Appliance conversion dependencies
(`virt-v2v`, libguestfs, qemu-kvm, and the VDDK `nbdkit` plugin) are installed only on AMD64 because
[RHEL 9 does not provide `virt-v2v` on ARM64](https://docs.redhat.com/en/documentation/red_hat_enterprise_linux/9/html/configuring_and_managing_virtualization/assembly_feature-support-and-limitations-in-rhel-9-virtualization_configuring_and_managing-virtualization).
The x86-specific filler image assets are also built only on AMD64; RVTools does not use them. The
ARM64 startup check requires `--rvtools-mode`. The Containerfile's AMD64 UI source stage only
provides static files; it does not set the architecture of the final image.

## Konflux requirements

Pull request, push, and tag runs build each platform through a matrix, then combine the image
references into the existing output tag. Existing AMD64 consumers and tag aliases continue to use
the same repository and names.

The ARM64 task uses Konflux's `buildah-remote` task. The tenant must have the
[Multi Platform Controller](https://konflux-ci.dev/architecture/add-ons/multi-platform-controller/)
configured with an ARM64 builder; without it, the ARM64 TaskRun cannot start. Confirm that support
in the assisted-migration Konflux tenant before relying on these pipeline runs. The controller's
platform configuration is cluster-owned and is not changed by this repository.

The remote build allows a cross-platform source image because the UI image is AMD64-only. The
Containerfile copies static files from it; the backend and final runtime stages still build for
their target architecture.

## Verification

- Inspect a Konflux-produced tag and confirm its image index lists both `linux/amd64` and `linux/arm64`.
- On native AMD64 Linux, smoke-test RVTools and appliance conversion and check that the existing appliance tools are present.
- On ARM64, smoke-test the UI and RVTools import, and confirm that starting without `--rvtools-mode` fails with the supported-mode message.
