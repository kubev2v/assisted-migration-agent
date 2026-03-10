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
# Stage 5: Final runtime image
# =============================================================================
FROM --platform=linux/amd64 registry.access.redhat.com/ubi9/ubi-minimal

RUN microdnf install -y ca-certificates tzdata && \
    microdnf clean all

WORKDIR /app

# Copy the binary from backend builder
COPY --from=backend-builder /tmp/agent /app/agent

# Copy UI static files from ui builder
COPY --from=ui-builder /apps/agent-ui/dist /app/static

# Copy OPA policies
COPY --from=opa-builder /app/policies /app/policies

# Create data directory (mounted via AGENT_DATA_FOLDER)
RUN mkdir -p /var/lib/agent && \
    chown -R 1001:0 /app/static /app/policies /var/lib/agent

USER 1001

# Expose HTTP port (configurable via --server-http-port, default: 8000)
EXPOSE 8000

ENTRYPOINT ["/app/agent"]
CMD ["run"]
