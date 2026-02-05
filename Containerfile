# Stage 1: Build the UI
FROM --platform=linux/amd64 node:22-slim AS ui-builder

WORKDIR /ui

# Copy package files first for better caching
COPY ui/package.json ui/package-lock.json ./

# Install dependencies
RUN npm ci

# Copy UI source
COPY ui/ ./

# Build production bundle
RUN npm run build

# Stage 2: Build the backend
FROM --platform=linux/amd64 golang:1.24 AS backend-builder

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
RUN make build GIT_COMMIT=${GIT_COMMIT}

# Stage 3: Final runtime image
FROM --platform=linux/amd64 registry.access.redhat.com/ubi10/ubi-minimal

RUN microdnf install -y ca-certificates tzdata && \
    microdnf clean all

WORKDIR /app

# Copy the binary from backend builder
COPY --from=backend-builder /app/bin/agent /app/agent

# Copy UI static files from ui builder
COPY --from=ui-builder /ui/dist /app/static

# Create data directory
RUN mkdir -p /var/lib/agent && chown -R 1001:0 /var/lib/agent

USER 1001

# Expose HTTP port
EXPOSE 8000

ENTRYPOINT ["/app/agent", "run", "--server-mode", "prod", "--server-statics-folder", "/app/static", "--data-folder", "/var/lib/agent", "--legacy-status-enabled"]
CMD []
