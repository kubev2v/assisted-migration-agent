# =============================================================================
# Stage 1: Fetch the UI
# =============================================================================
ARG AGENT_UI_IMAGE=quay.io/redhat-user-workloads/assisted-migration-tenant/migration-planner-agent-ui
ARG AGENT_UI_IMAGE_TAG=latest
FROM --platform=linux/amd64 ${AGENT_UI_IMAGE}:${AGENT_UI_IMAGE_TAG} AS ui-builder


# =============================================================================
# Stage 2: Extract UI metadata
# =============================================================================
FROM --platform=linux/amd64 registry.access.redhat.com/ubi9/ubi-minimal AS ui-metadata-extractor

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
FROM --platform=linux/amd64 registry.access.redhat.com/ubi9/go-toolset AS backend-builder

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
FROM --platform=linux/amd64 registry.access.redhat.com/ubi9/ubi-minimal AS opa-builder

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
FROM --platform=linux/amd64 registry.access.redhat.com/ubi9/ubi AS filler-builder

RUN echo -e '[centos-stream-baseos]\nname=CentOS Stream 9 - BaseOS\nbaseurl=https://mirror.stream.centos.org/9-stream/BaseOS/x86_64/os/\ngpgcheck=0\nenabled=1' > /etc/yum.repos.d/centos-stream-baseos.repo && \
    echo -e '[centos-stream-appstream]\nname=CentOS Stream 9 - AppStream\nbaseurl=https://mirror.stream.centos.org/9-stream/AppStream/x86_64/os/\ngpgcheck=0\nenabled=1' > /etc/yum.repos.d/centos-stream-appstream.repo

RUN dnf install -y --allowerasing qemu-img libguestfs-tools genisoimage && \
    dnf clean all

ENV LIBGUESTFS_BACKEND=direct

COPY scripts/build-filler-image.sh /tmp/
RUN FILLER_OUTPUT_DIR=/tmp/filler-assets SKIP_BOOT_TEST=1 bash /tmp/build-filler-image.sh


# =============================================================================
# Stage 6: Download DuckDB extensions (for air-gapped environments)
# =============================================================================
FROM --platform=linux/amd64 registry.access.redhat.com/ubi9/ubi-minimal AS duckdb-extensions

RUN microdnf install -y wget gzip && \
    microdnf clean all

WORKDIR /extensions

# Download sqlite_scanner extension for DuckDB v1.4.3 linux_amd64
ARG DUCKDB_VERSION=v1.4.3
RUN wget -q "https://extensions.duckdb.org/${DUCKDB_VERSION}/linux_amd64/sqlite_scanner.duckdb_extension.gz" && \
    gunzip sqlite_scanner.duckdb_extension.gz

# =============================================================================
# Stage 7: Final runtime image
# =============================================================================
FROM --platform=linux/amd64 registry.access.redhat.com/ubi9/ubi

# Add CentOS Stream 10 repos for virt-v2v and dependencies
RUN echo -e '[centos-stream-baseos]\nname=CentOS Stream 9 - BaseOS\nbaseurl=https://mirror.stream.centos.org/9-stream/BaseOS/x86_64/os/\ngpgcheck=0\nenabled=1' > /etc/yum.repos.d/centos-stream-baseos.repo && \
    echo -e '[centos-stream-appstream]\nname=CentOS Stream 9 - AppStream\nbaseurl=https://mirror.stream.centos.org/9-stream/AppStream/x86_64/os/\ngpgcheck=0\nenabled=1' > /etc/yum.repos.d/centos-stream-appstream.repo && \
    echo -e '[centos-stream-crb]\nname=CentOS Stream 9 - CRB\nbaseurl=https://mirror.stream.centos.org/9-stream/CRB/x86_64/os/\ngpgcheck=0\nenabled=1' > /etc/yum.repos.d/centos-stream-crb.repo

RUN dnf install -y ca-certificates tzdata \
    libguestfs \
    libguestfs-tools \
    libguestfs-tools-c \
    virt-v2v \
    qemu-kvm \
    nbdkit \
    nbdkit-vddk-plugin \
    && rm -rf /usr/share/virtio-win # ~1G of unneeded files \
    && dnf clean all

WORKDIR /app

# Copy the binary from backend builder
COPY --from=backend-builder /tmp/agent /app/agent

# Copy filler image assets (Alpine boot image + seed ISO for forecaster)
COPY --from=filler-builder /tmp/filler-assets/alpine-filler.raw.gz /app/assets/
COPY --from=filler-builder /tmp/filler-assets/seed.iso.gz /app/assets/

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
    cp -n /app/extensions/*.duckdb_extension /var/lib/agent/ 2>/dev/null || true\n\
fi\n\
exec /app/agent "$@"\n' > /app/entrypoint.sh && chmod +x /app/entrypoint.sh

USER 1001

# Expose HTTP port (configurable via --server-http-port, default: 8000)
EXPOSE 8000

ENTRYPOINT ["/app/entrypoint.sh"]
CMD ["run"]
