# =============================================================================
# Stage 1: Fetch the UI
# =============================================================================
ARG AGENT_UI_IMAGE_TAG=latest
FROM --platform=linux/amd64 quay.io/assisted-migration/migration-planner-agent-ui:${AGENT_UI_IMAGE_TAG} AS ui-builder


# =============================================================================
# Stage 2: Build the backend
# =============================================================================
FROM --platform=linux/amd64 registry.access.redhat.com/ubi9/ubi-minimal AS backend-builder

# Install Go and build tools (needed for duckdb bindings)
RUN microdnf install -y golang git make gcc gcc-c++ && \
    microdnf clean all

WORKDIR /app

# Copy go module files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY main.go Makefile ./
COPY cmd/     ./cmd/
COPY internal/ ./internal/
COPY pkg/     ./pkg/
COPY api/     ./api/

# Build the binary
ARG GIT_COMMIT=unknown
RUN make build GIT_COMMIT=${GIT_COMMIT} BINARY_PATH=/tmp/agent


# =============================================================================
# Stage 3: Setup OPA policies
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
# Stage 4: Final runtime image
# =============================================================================
FROM --platform=linux/amd64 registry.access.redhat.com/ubi9/ubi-minimal

ARG GIT_COMMIT=unknown
ENV AGENT_VERSION=${GIT_COMMIT}

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
