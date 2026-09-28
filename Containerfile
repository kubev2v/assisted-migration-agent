# =============================================================================
# Stage 1: Fetch the UI
# =============================================================================
ARG AGENT_UI_IMAGE=quay.io/redhat-user-workloads/assisted-migration-tenant/migration-planner-agent-ui
ARG AGENT_UI_IMAGE_TAG=latest
# Copy architecture-independent UI static files from the AMD64 UI image.
FROM --platform=linux/amd64 ${AGENT_UI_IMAGE}:${AGENT_UI_IMAGE_TAG} AS ui-builder


# =============================================================================
# Stage 2: Extract UI metadata
# =============================================================================
FROM registry.access.redhat.com/ubi9/ubi-minimal AS ui-metadata-extractor

ARG AGENT_UI_IMAGE=quay.io/redhat-user-workloads/assisted-migration-tenant/migration-planner-agent-ui
ARG AGENT_UI_IMAGE_TAG=latest

# Install skopeo to inspect the UI image
RUN microdnf install -y skopeo && microdnf clean all

# Extract UI git commit from the UI image labels and save to file
RUN echo "Inspecting image: ${AGENT_UI_IMAGE}:${AGENT_UI_IMAGE_TAG}" && \
    UI_GIT_COMMIT=$(skopeo inspect --format '{{index .Labels "vcs-ref"}}' docker://${AGENT_UI_IMAGE}:${AGENT_UI_IMAGE_TAG} || echo "unknown") && \
    echo "UI Git Commit: ${UI_GIT_COMMIT}" && \
    echo -n "${UI_GIT_COMMIT}" > /tmp/ui-git-commit.txt


# =============================================================================
# Stage 3: Build the backend
# =============================================================================
FROM registry.access.redhat.com/ubi9/go-toolset AS backend-builder

# Copy go module files first for better caching
COPY go.mod go.sum ./
RUN go mod download

USER 0
COPY . .
# Copy UI git commit from metadata extractor stage
COPY --from=ui-metadata-extractor /tmp/ui-git-commit.txt /tmp/ui-git-commit.txt

ARG PLANNER_AGENT_GIT_COMMIT=unknown
ARG PLANNER_AGENT_VERSION=v0.0.0

# Build the agent with UI git commit
RUN UI_GIT_COMMIT=$(cat /tmp/ui-git-commit.txt) && \
    echo "Building with UI_GIT_COMMIT=${UI_GIT_COMMIT}" && \
    make build PLANNER_AGENT_GIT_COMMIT=${PLANNER_AGENT_GIT_COMMIT} PLANNER_AGENT_VERSION=${PLANNER_AGENT_VERSION} UI_GIT_COMMIT=${UI_GIT_COMMIT} BINARY_PATH=/tmp/agent


# =============================================================================
# Stage 4: Setup OPA policies
# =============================================================================
FROM registry.access.redhat.com/ubi9/ubi-minimal AS opa-builder

RUN microdnf install -y wget tar gzip ca-certificates tzdata && \
    microdnf clean all

WORKDIR /app

# Download and extract OPA policies from forklift
RUN mkdir -p /app/policies /app/forklift && \
    cd /app/forklift && \
    wget https://github.com/kubev2v/forklift/archive/main.tar.gz && \
    tar -xzf main.tar.gz --strip-components=1 && \
    find validation/policies/io/konveyor/forklift/vmware \
        -name "*.rego" ! -name "*_test.rego" \
        -exec cp {} /app/policies/ \;


# =============================================================================
# Stage 5: Build Alpine filler image for forecaster benchmarks
# =============================================================================
FROM registry.access.redhat.com/ubi9/ubi AS filler-builder

# Fetch the CentOS Stream signing key and verify it against a pinned SHA-256 before trusting it
ARG CENTOS_GPG_KEY_SHA256=146059788b214d7ba0dd70c1cf21111e594c6cfde201da8a9a88fe7101be8a78
RUN if [ "$(uname -m)" = x86_64 ]; then \
    curl -fsSL -o /etc/pki/rpm-gpg/RPM-GPG-KEY-CentOS-Official https://www.centos.org/keys/RPM-GPG-KEY-CentOS-Official && \
    echo "${CENTOS_GPG_KEY_SHA256}  /etc/pki/rpm-gpg/RPM-GPG-KEY-CentOS-Official" | sha256sum -c - && \
    echo -e '[centos-stream-baseos]\nname=CentOS Stream 9 - BaseOS\nbaseurl=https://mirror.stream.centos.org/9-stream/BaseOS/x86_64/os/\ngpgcheck=1\ngpgkey=file:///etc/pki/rpm-gpg/RPM-GPG-KEY-CentOS-Official\nenabled=1' > /etc/yum.repos.d/centos-stream-baseos.repo && \
    echo -e '[centos-stream-appstream]\nname=CentOS Stream 9 - AppStream\nbaseurl=https://mirror.stream.centos.org/9-stream/AppStream/x86_64/os/\ngpgcheck=1\ngpgkey=file:///etc/pki/rpm-gpg/RPM-GPG-KEY-CentOS-Official\nenabled=1' > /etc/yum.repos.d/centos-stream-appstream.repo; \
    fi

RUN if [ "$(uname -m)" = x86_64 ]; then \
    dnf install -y --allowerasing qemu-img libguestfs-tools genisoimage && dnf clean all; \
    fi

ENV LIBGUESTFS_BACKEND=direct

COPY scripts/build-filler-image.sh /tmp/
RUN mkdir -p /tmp/filler-assets && \
    if [ "$(uname -m)" = x86_64 ]; then \
    FILLER_OUTPUT_DIR=/tmp/filler-assets SKIP_BOOT_TEST=1 bash /tmp/build-filler-image.sh; \
    fi


# =============================================================================
# Stage 6: Download DuckDB extensions (for air-gapped environments)
# =============================================================================
FROM registry.access.redhat.com/ubi9/ubi-minimal AS duckdb-extensions

RUN microdnf install -y wget gzip && \
    microdnf clean all

WORKDIR /extensions

# Download the target architecture's sqlite_scanner extension, verified against a pinned SHA-256
ARG DUCKDB_VERSION=v1.4.3
RUN case "$(uname -m)" in \
      x86_64) platform=linux_amd64; checksum=75ead0bb623cd67c8c7e40d30f96e5fc2823fd3df90eb2ecd289aae177b15820 ;; \
      aarch64) platform=linux_arm64; checksum=03aa6ee4f8c4044d179b08632f3836cfd1cc426a50b8d687444847adb801888b ;; \
      *) echo "unsupported architecture: $(uname -m)" >&2; exit 1 ;; \
    esac && \
    wget -q "https://extensions.duckdb.org/${DUCKDB_VERSION}/${platform}/sqlite_scanner.duckdb_extension.gz" && \
    echo "${checksum}  sqlite_scanner.duckdb_extension.gz" | sha256sum -c - && \
    gunzip sqlite_scanner.duckdb_extension.gz

# =============================================================================
# Stage 7: Final runtime image
# =============================================================================
FROM registry.access.redhat.com/ubi9/ubi

# Add CentOS Stream 9 repos for virt-v2v and dependencies
# Fetch the CentOS Stream signing key and verify it against a pinned SHA-256 before trusting it
ARG CENTOS_GPG_KEY_SHA256=146059788b214d7ba0dd70c1cf21111e594c6cfde201da8a9a88fe7101be8a78
RUN if [ "$(uname -m)" = x86_64 ]; then \
    curl -fsSL -o /etc/pki/rpm-gpg/RPM-GPG-KEY-CentOS-Official https://www.centos.org/keys/RPM-GPG-KEY-CentOS-Official && \
    echo "${CENTOS_GPG_KEY_SHA256}  /etc/pki/rpm-gpg/RPM-GPG-KEY-CentOS-Official" | sha256sum -c - && \
    echo -e '[centos-stream-baseos]\nname=CentOS Stream 9 - BaseOS\nbaseurl=https://mirror.stream.centos.org/9-stream/BaseOS/x86_64/os/\ngpgcheck=1\ngpgkey=file:///etc/pki/rpm-gpg/RPM-GPG-KEY-CentOS-Official\nenabled=1' > /etc/yum.repos.d/centos-stream-baseos.repo && \
    echo -e '[centos-stream-appstream]\nname=CentOS Stream 9 - AppStream\nbaseurl=https://mirror.stream.centos.org/9-stream/AppStream/x86_64/os/\ngpgcheck=1\ngpgkey=file:///etc/pki/rpm-gpg/RPM-GPG-KEY-CentOS-Official\nenabled=1' > /etc/yum.repos.d/centos-stream-appstream.repo && \
    echo -e '[centos-stream-crb]\nname=CentOS Stream 9 - CRB\nbaseurl=https://mirror.stream.centos.org/9-stream/CRB/x86_64/os/\ngpgcheck=1\ngpgkey=file:///etc/pki/rpm-gpg/RPM-GPG-KEY-CentOS-Official\nenabled=1' > /etc/yum.repos.d/centos-stream-crb.repo; \
    fi

RUN if [ "$(uname -m)" = x86_64 ]; then \
    dnf install -y ca-certificates tzdata \
    libguestfs \
    libguestfs-tools \
    libguestfs-tools-c \
    virt-v2v \
    qemu-kvm \
    nbdkit \
    nbdkit-vddk-plugin \
    && rm -rf /usr/share/virtio-win; \
    else dnf install -y ca-certificates tzdata; \
    fi && dnf clean all

WORKDIR /app

# Copy the binary from backend builder
COPY --from=backend-builder /tmp/agent /app/agent

# Copy filler image assets (Alpine boot image + seed ISO for forecaster)
COPY --from=filler-builder /tmp/filler-assets /app/assets

# Copy UI static files from ui builder
COPY --from=ui-builder /apps/agent-ui/dist /app/static

# Copy OPA policies
COPY --from=opa-builder /app/policies /app/policies

# Copy DuckDB extensions
COPY --from=duckdb-extensions /extensions /app/extensions

# Create data directory (mounted via AGENT_DATA_FOLDER)
RUN mkdir -p /var/lib/agent /app/.cache/libvirt && \
    chown -R 1001:0 /app/static /app/policies /app/extensions /app/assets /var/lib/agent /app/.cache/libvirt

ENV LIBGUESTFS_BACKEND=direct

# Create entrypoint script
RUN printf '#!/bin/sh\n\
# Copy DuckDB extensions to persistent data folder\n\
if [ -d /app/extensions ] && [ -d /var/lib/agent ]; then\n\
    cp --remove-destination /app/extensions/*.duckdb_extension /var/lib/agent/ || exit 1\n\
fi\n\
exec /app/agent "$@"\n' > /app/entrypoint.sh && chmod +x /app/entrypoint.sh

USER 1001

# Expose HTTP port (configurable via --server-http-port, default: 8000)
EXPOSE 8000

ENTRYPOINT ["/app/entrypoint.sh"]
CMD ["run"]
