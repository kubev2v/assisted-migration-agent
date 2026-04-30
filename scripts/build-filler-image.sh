#!/usr/bin/env bash
#
# Build an Alpine-based filler VMDK for the forecaster VM strategy.
#
# Prerequisites: qemu-img, curl, guestfish
# Output:
#   pkg/vmware/assets/alpine-filler.raw.gz   (~15-20 MB)
#   pkg/vmware/assets/seed.iso.gz            (~1 KB)
#
# The Alpine VM boots, runs a local init script that fills /dev/sdb
# with random data, then powers off. No cloud-init needed.
#

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
OUTPUT_DIR="${FILLER_OUTPUT_DIR:-$REPO_ROOT/pkg/vmware/assets}"
WORK_DIR=$(mktemp -d)
trap 'chmod -R u+w "$WORK_DIR" 2>/dev/null; rm -rf "$WORK_DIR" 2>/dev/null' EXIT

ALPINE_VERSION="3.21"
ALPINE_RELEASE="3.21.7"
ALPINE_URL="https://dl-cdn.alpinelinux.org/alpine/v${ALPINE_VERSION}/releases/x86_64/alpine-virt-${ALPINE_RELEASE}-x86_64.iso"
SYSLINUX_URL="https://dl-cdn.alpinelinux.org/alpine/v${ALPINE_VERSION}/main/x86_64/syslinux-6.04_pre1-r16.apk"
ALPINE_REPO_URL="https://dl-cdn.alpinelinux.org/alpine/v${ALPINE_VERSION}/main/x86_64"

DISK_SIZE_MB=256

echo "=== Alpine Filler Image Builder ==="
echo ""

for cmd in qemu-img curl guestfish; do
    if ! command -v "$cmd" &>/dev/null; then
        echo "ERROR: $cmd required"
        exit 1
    fi
done

MKISO=""
if command -v genisoimage &>/dev/null; then
    MKISO="genisoimage"
elif command -v mkisofs &>/dev/null; then
    MKISO="mkisofs"
else
    echo "ERROR: genisoimage or mkisofs required"
    exit 1
fi

# ── Step 1: Download Alpine virt ISO ──────────────────────────
echo "=== Step 1: Downloading Alpine ${ALPINE_RELEASE} virt ISO + syslinux ==="
curl -fL -o "$WORK_DIR/alpine.iso" "$ALPINE_URL"
echo "Downloaded ISO: $(du -h "$WORK_DIR/alpine.iso" | cut -f1)"
curl -fL -o "$WORK_DIR/syslinux.apk" "$SYSLINUX_URL"
echo "Downloaded syslinux: $(du -h "$WORK_DIR/syslinux.apk" | cut -f1)"

# Download openssl + deps for fast random fill (ChaCha20 ~500+ MB/s)
REPO_INDEX=$(curl -fsSL "${ALPINE_REPO_URL}/")
for pkg in libcrypto3 libssl3 openssl; do
    pkgfile=$(echo "$REPO_INDEX" | grep -oE "\"${pkg}-[0-9][^\"]+\.apk\"" | tr -d '"' | head -1)
    if [ -z "$pkgfile" ]; then
        echo "ERROR: ${pkg} not found in Alpine repo"
        exit 1
    fi
    curl -fL -o "$WORK_DIR/${pkg}.apk" "${ALPINE_REPO_URL}/${pkgfile}"
    echo "Downloaded ${pkg}: $(du -h "$WORK_DIR/${pkg}.apk" | cut -f1)"
done

# ── Step 2: Create seed ISO (kept for compatibility) ──────────
echo ""
echo "=== Step 2: Creating seed ISO ==="

mkdir -p "$WORK_DIR/cidata"
cat > "$WORK_DIR/cidata/meta-data" << 'META'
{"instance-id": "filler-vm", "local-hostname": "filler"}
META
cat > "$WORK_DIR/cidata/user-data" << 'USERDATA'
#!/bin/sh
# Fill /dev/sdb with pseudo-random data using openssl ChaCha20, then power off.
for i in $(seq 1 30); do [ -b /dev/sdb ] && break; sleep 1; done
if [ -b /dev/sdb ]; then
  DISK_SIZE=$(blockdev --getsize64 /dev/sdb)
  dd if=/dev/zero bs=1M count=$((DISK_SIZE / 1048576)) 2>/dev/null | openssl enc -chacha20 -pass pass:benchfill -nosalt -out /dev/sdb
  sync
fi
poweroff
USERDATA
"$MKISO" -output "$WORK_DIR/seed.iso" -volid cidata -joliet -rock \
    "$WORK_DIR/cidata/" 2>/dev/null
echo "Seed ISO: $(du -h "$WORK_DIR/seed.iso" | cut -f1)"

# ── Step 3: Extract files from ISO ───────────────────────────
echo ""
echo "=== Step 3: Extracting files from Alpine ISO ==="

guestfish --ro -a "$WORK_DIR/alpine.iso" <<EXTRACT
run
mount /dev/sda /
copy-out /apks $WORK_DIR/
copy-out /boot/vmlinuz-virt $WORK_DIR/
copy-out /boot/initramfs-virt $WORK_DIR/
EXTRACT
echo "Extracted kernel + APK packages"

# ── Step 4: Prepare rootfs on the host ────────────────────────
echo ""
echo "=== Step 4: Preparing Alpine rootfs ==="

ROOTFS="$WORK_DIR/rootfs"
mkdir -p "$ROOTFS"
APKDIR="$WORK_DIR/apks/x86_64"

# Extract essential packages (APK files are gzipped tarballs)
for pkg in musl libcap2 alpine-baselayout-data alpine-baselayout busybox busybox-binsh \
           busybox-suid busybox-openrc busybox-mdev-openrc \
           alpine-keys apk-tools alpine-release openrc \
           ; do
    pkgfile=$(ls "$APKDIR"/${pkg}-[0-9]*.apk 2>/dev/null | head -1)
    if [ -n "$pkgfile" ]; then
        tar -xzf "$pkgfile" -C "$ROOTFS" 2>/dev/null || true
        echo "  extracted: $(basename "$pkgfile")"
    fi
done

# Extract syslinux (downloaded separately, not on alpine-virt ISO)
tar -xzf "$WORK_DIR/syslinux.apk" -C "$ROOTFS" 2>/dev/null || true
echo "  extracted: syslinux (from repo)"

# Extract openssl + dependencies (for fast random fill)
for pkg in libcrypto3 libssl3 openssl; do
    if [ -f "$WORK_DIR/${pkg}.apk" ]; then
        tar -xzf "$WORK_DIR/${pkg}.apk" -C "$ROOTFS" 2>/dev/null || true
        echo "  extracted: ${pkg} (from repo)"
    fi
done

# Create directory structure
for dir in bin sbin usr/bin usr/sbin; do
    mkdir -p "$ROOTFS/$dir"
done

# Create the fill-disk script that runs on boot
mkdir -p "$ROOTFS/etc/local.d"
cat > "$ROOTFS/etc/local.d/fill-disk.start" << 'FILLSCRIPT'
#!/bin/sh
# Fill /dev/sdb with pseudo-random data using openssl ChaCha20, then power off.
# This runs as a local.d start script on boot.
{
    for i in $(seq 1 30); do
        [ -b /dev/sdb ] && break
        sleep 1
    done
    if [ -b /dev/sdb ]; then
        DISK_SIZE=$(blockdev --getsize64 /dev/sdb)
        dd if=/dev/zero bs=1M count=$((DISK_SIZE / 1048576)) 2>/dev/null | \
            openssl enc -chacha20 -pass pass:benchfill -nosalt -out /dev/sdb
        sync
    fi
    poweroff
} &
FILLSCRIPT
chmod +x "$ROOTFS/etc/local.d/fill-disk.start"

# Basic config
mkdir -p "$ROOTFS/boot" "$ROOTFS/etc/apk/keys" "$ROOTFS/etc/runlevels"/{sysinit,boot,default,shutdown}
cp "$WORK_DIR/vmlinuz-virt" "$ROOTFS/boot/"
cp "$WORK_DIR/initramfs-virt" "$ROOTFS/boot/"
cp "$APKDIR"/*.pub "$ROOTFS/etc/apk/keys/" 2>/dev/null || true

echo "filler" > "$ROOTFS/etc/hostname"
echo "/dev/sda1 / ext4 defaults 0 1" > "$ROOTFS/etc/fstab"

# Enable OpenRC services via symlinks
for svc in devfs dmesg mdev hwdrivers; do
    ln -sf "/etc/init.d/$svc" "$ROOTFS/etc/runlevels/sysinit/" 2>/dev/null || true
done
for svc in modules sysctl hostname bootmisc syslog local; do
    ln -sf "/etc/init.d/$svc" "$ROOTFS/etc/runlevels/boot/" 2>/dev/null || true
done
for svc in mount-ro killprocs savecache; do
    ln -sf "/etc/init.d/$svc" "$ROOTFS/etc/runlevels/shutdown/" 2>/dev/null || true
done

# Ensure all files are readable (bbsuid has restricted perms that break copy-in)
chmod -R u+r "$ROOTFS"

echo "Rootfs prepared ($(du -sh "$ROOTFS" 2>/dev/null | cut -f1))"

# ── Step 5: Build disk image ─────────────────────────────────
echo ""
echo "=== Step 5: Building disk image ==="

DISK_RAW="$WORK_DIR/alpine.raw"
qemu-img create -f raw "$DISK_RAW" ${DISK_SIZE_MB}M

# Phase 1: Partition, format, copy rootfs
guestfish -a "$DISK_RAW" --rw <<GF1
run
part-init /dev/sda mbr
part-add /dev/sda p 2048 -1
part-set-bootable /dev/sda 1 true
mkfs ext4 /dev/sda1
mount /dev/sda1 /
copy-in $ROOTFS/. /
mkdir-p /media/cdrom/apks
copy-in $APKDIR /media/cdrom/apks/
GF1

echo "Rootfs + APK packages copied to image"

# Phase 2: Install busybox symlinks + syslinux bootloader inside the guest
guestfish -a "$DISK_RAW" --rw <<'GF2'
run
mount /dev/sda1 /

# Install busybox symlinks
sh "/bin/busybox --install -s 2>/dev/null; echo busybox-symlinks-done"

# Install syslinux bootloader
mkdir-p /boot/syslinux
sh "printf 'DEFAULT alpine\nPROMPT 0\nTIMEOUT 10\n\nLABEL alpine\n  LINUX /boot/vmlinuz-virt\n  INITRD /boot/initramfs-virt\n  APPEND root=/dev/sda1 rootfstype=ext4 modules=ext4 quiet\n' > /boot/syslinux/syslinux.cfg; echo syslinux-cfg-done"
sh "extlinux --install /boot/syslinux; echo extlinux-done"
sh "dd if=/usr/share/syslinux/mbr.bin of=/dev/sda bs=440 count=1 conv=notrunc 2>/dev/null; echo mbr-done"

# Verify
sh "ls -la /boot/ && ls -la /etc/local.d/ && echo image-complete"
GF2

echo "Disk image built"

# ── Step 6: Compress raw image ───────────────────────────────
echo ""
echo "=== Step 6: Compressing raw disk image ==="

mkdir -p "$OUTPUT_DIR"
gzip -9 -c "$DISK_RAW" > "$OUTPUT_DIR/alpine-filler.raw.gz"
gzip -9 -c "$WORK_DIR/seed.iso" > "$OUTPUT_DIR/seed.iso.gz"

# Remove old images
rm -f "$OUTPUT_DIR/cirros-filler.vmdk.gz" "$OUTPUT_DIR/alpine-filler.vmdk.gz"

RAW_SIZE=$(du -h "$OUTPUT_DIR/alpine-filler.raw.gz" | cut -f1)
SEED_SIZE=$(du -h "$OUTPUT_DIR/seed.iso.gz" | cut -f1)
echo ""
echo "=== Done ==="
echo "Output:"
echo "  $OUTPUT_DIR/alpine-filler.raw.gz ($RAW_SIZE)"
echo "  $OUTPUT_DIR/seed.iso.gz ($SEED_SIZE)"

# ── Step 7: Quick boot test ──────────────────────────────────
if [[ "${SKIP_BOOT_TEST:-}" != "1" ]]; then
    echo ""
    echo "=== Step 7: Quick boot test with QEMU (15s) ==="
    KVM_ARG=""
    if [[ -r /dev/kvm && -w /dev/kvm ]]; then
        KVM_ARG="-enable-kvm"
    fi
    timeout 15 qemu-system-x86_64 -m 256 -nographic $KVM_ARG \
        -drive file="$DISK_RAW",format=raw,if=virtio \
        -serial mon:stdio 2>&1 | tail -30 || true
    echo ""
    echo "=== Boot test complete ==="
else
    echo ""
    echo "=== Step 7: Boot test skipped (SKIP_BOOT_TEST=1) ==="
fi
