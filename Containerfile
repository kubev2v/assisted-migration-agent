# Stage 1: Fetch the UI
ARG AGENT_UI_IMAGE_TAG=latest
FROM --platform=linux/amd64 quay.io/assisted-migration/migration-planner-agent-ui:${AGENT_UI_IMAGE_TAG} AS ui-builder

# Stage 2: Build the backend
FROM --platform=linux/amd64 registry.access.redhat.com/ubi9/ubi-minimal AS backend-builder

# Install Go and build tools (needed for duckdb bindings)
RUN microdnf install -y golang git make gcc gcc-c++ && \
    microdnf clean all

WORKDIR /app

# Copy go module files first for better caching
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY main.go ./
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY pkg/ ./pkg/
COPY api/ ./api/
COPY Makefile ./

# Get git commit SHA for build
ARG GIT_COMMIT=unknown
RUN make build GIT_COMMIT=${GIT_COMMIT} BINARY_PATH=/tmp/agent

# Stage 3: Final runtime image
FROM --platform=linux/amd64 registry.access.redhat.com/ubi9/ubi-minimal

RUN microdnf install -y ca-certificates tzdata && \
    microdnf clean all

WORKDIR /app

# Copy the binary from backend builder
COPY --from=backend-builder /tmp/agent /app/agent

# Copy UI static files from ui builder
COPY --from=ui-builder /apps/agent-ui/dist /app/static
RUN chown -R 1001:0 /app/static

# Create data directory (mounted via AGENT_DATA_FOLDER)
RUN mkdir -p /var/lib/agent && chown -R 1001:0 /var/lib/agent

USER 1001

# Expose HTTP port (configurable via --server-http-port, default: 8000)
EXPOSE 8000

ENTRYPOINT ["/app/agent"]
CMD ["run"]
