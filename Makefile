.PHONY: generate generate.proto build build.e2e e2e e2e.container e2e.vm e2e.container.clean run help tidy tidy-check clean lint format check-format check-generate validate-all image

PODMAN ?= podman
GIT_COMMIT=$(shell git rev-list -1 HEAD --abbrev-commit)

BINARY_NAME=agent
BINARY_PATH=bin/$(BINARY_NAME)
MAIN_PATH=./main.go

IMAGE_NAME ?= assisted-migration-agent
IMAGE_TAG ?= latest
AGENT_UI_IMAGE_TAG ?= latest

GOBASE=$(shell pwd)
GOBIN=$(GOBASE)/bin
GO_BUILD_FLAGS := ${GO_BUILD_FLAGS}

.EXPORT_ALL_VARIABLES:

help:
	@echo "Targets:"
	@echo "    build:           build the agent binary"
	@echo "    build.e2e:       build the e2e test binary"
	@echo "    e2e:             run e2e tests (default: container mode)"
	@echo "    e2e.container:   run e2e tests in container mode (Podman)"
	@echo "    e2e.vm:          run e2e tests in VM mode (externally managed infra)"
	@echo "    e2e.container.clean: remove all e2e test containers and volumes"
	@echo "    image:           build container image"
	@echo "    run.image:       run container image locally (requires AGENT_ID and SOURCE_ID)"
	@echo "    run:             run the agent"
	@echo "    run.ui:          start React dev server"
	@echo "    clean:           clean up binaries and tools"
	@echo "    generate:        "
	@echo "    generate.proto:  "
	@echo "    check-generate:  "
	@echo "    validate-all:    run all validations (lint, format check, tidy check)"
	@echo "    lint:            run golangci-lint"
	@echo "    format:          format Go code using gofmt and goimports"
	@echo "    check-format:    check that formatting does not introduce changes"
	@echo "    tidy:            tidy go mod"
	@echo "    tidy-check:      check that go.mod and go.sum are tidy"

# Build the application
build:
	@echo "Building $(BINARY_NAME)..."
	go build -ldflags="-X main.sha=${GIT_COMMIT}" -o $(BINARY_PATH) $(MAIN_PATH)
	@echo "Build complete: $(BINARY_PATH)"

build.e2e:
	@echo "Building e2e binary..."
	go build -tags "exclude_graphdriver_btrfs containers_image_openpgp" -o bin/e2e ./test/e2e
	@echo "Build complete: bin/e2e"

E2E_AGENT_IMAGE ?= $(IMAGE_NAME):$(IMAGE_TAG)
E2E_BACKEND_IMAGE ?= quay.io/kubev2v/migration-planner-api:latest
E2E_ISO_PATH ?= $(CURDIR)
E2E_INFRA_MODE ?= container

e2e: build.e2e
	@echo "🧪 Running e2e tests (infra-mode=$(E2E_INFRA_MODE))..."
	./bin/e2e -infra-mode=$(E2E_INFRA_MODE) -agent-image=$(E2E_AGENT_IMAGE) -backend-image=$(E2E_BACKEND_IMAGE) --ginkgo.v -iso-path=$(E2E_ISO_PATH)

e2e.container: build.e2e
	@echo "🧪 Running e2e tests (container mode)..."
	./bin/e2e -infra-mode=container -agent-image=$(E2E_AGENT_IMAGE) -backend-image=$(E2E_BACKEND_IMAGE) --ginkgo.v -iso-path=$(E2E_ISO_PATH)

e2e.vm: build.e2e
	@echo "🧪 Running e2e tests (VM mode)..."
	./bin/e2e -infra-mode=vm --ginkgo.v

e2e.container.clean:
	$(PODMAN) rm --force test-planner || true
	$(PODMAN) rm --force test-planner-db || true
	$(PODMAN) rm --force test-planner-agent || true
	$(PODMAN) rm --force test-vcsim || true
	$(PODMAN) volume rm --force test-agent-data || true

# Build container image
image:
	@echo "📦 Building container image $(IMAGE_NAME):$(IMAGE_TAG)..."
	$(PODMAN) build --build-arg GIT_COMMIT=$(GIT_COMMIT) --build-arg AGENT_UI_IMAGE_TAG=$(AGENT_UI_IMAGE_TAG) -t $(IMAGE_NAME):$(IMAGE_TAG) -f Containerfile .
	@echo "✅ Image built: $(IMAGE_NAME):$(IMAGE_TAG)"

# Run container image locally
# Usage: make run.image AGENT_ID=<uuid> SOURCE_ID=<uuid>
# Example: make run.image AGENT_ID=550e8400-e29b-41d4-a716-446655440000 SOURCE_ID=6ba7b810-9dad-11d1-80b4-00c04fd430c8
AGENT_ID ?=
SOURCE_ID ?=
CONTAINER_NAME ?= migration-planner-agent
run.image: image
	@if [ -z "$(AGENT_ID)" ] || [ -z "$(SOURCE_ID)" ]; then \
		echo "❌ Error: AGENT_ID and SOURCE_ID are required"; \
		echo "Usage: make run.image AGENT_ID=<uuid> SOURCE_ID=<uuid>"; \
		exit 1; \
	fi
	@echo "🛑 Stopping existing container if running..."
	@$(PODMAN) stop $(CONTAINER_NAME) 2>/dev/null || true
	@$(PODMAN) rm $(CONTAINER_NAME) 2>/dev/null || true
	@echo "🚀 Starting container $(CONTAINER_NAME)..."
	@echo "   Image: $(IMAGE_NAME):$(IMAGE_TAG)"
	@echo "   Agent ID: $(AGENT_ID)"
	@echo "   Source ID: $(SOURCE_ID)"
	@echo "   UI available at: https://localhost:8000"
	$(PODMAN) run -d \
		--name $(CONTAINER_NAME) \
		-p 8000:8000 \
		$(IMAGE_NAME):$(IMAGE_TAG) \
		run \
		--agent-id $(AGENT_ID) \
		--source-id $(SOURCE_ID) \
		--server-mode prod \
		--server-statics-folder /app/static \
		--data-folder /var/lib/agent \
		--console-url http://host.containers.internal:7443
	@echo "✅ Container started. View logs with: podman logs -f $(CONTAINER_NAME)"

clean:
	@echo "🗑️ Removing $(BINARY_PATH)..."
	- rm -f $(BINARY_PATH)
	@echo "✅ Clean complete."

run:
	$(BINARY_PATH) run

run.ui:
	cd $(CURDIR)/ui && npm run start

generate:
	@echo "Generating code..."
	go generate ./...
	@$(MAKE) format
	@echo "Code generation complete."

# Generate protobuf code using buf in container
generate.proto:
	@echo "Generating protobuf code with buf in container..."
	$(PODMAN) run --rm \
		-v $(CURDIR)/api/v1/:/workspace \
		-w /workspace \
		bufbuild/buf:latest \
		generate
	@echo "Protobuf generation complete."

tidy:
	@echo "🧹 Tidying go modules..."
	git ls-files go.mod '**/*go.mod' -z | xargs -0 -I{} bash -xc 'cd $$(dirname {}) && go mod tidy'
	@echo "✅ Go modules tidied successfully."

# Check that go mod tidy does not introduce changes
tidy-check: tidy
	@echo "🔍 Checking if go.mod and go.sum are tidy..."
	@git diff --quiet go.mod go.sum || (echo "❌ Detected uncommitted changes after tidy. Run 'make tidy' and commit the result." && git diff go.mod go.sum && exit 1)
	@echo "✅ go.mod and go.sum are tidy."

##################### "make lint" support start ##########################
GOLANGCI_LINT_VERSION := v1.64.8
GOLANGCI_LINT := $(GOBIN)/golangci-lint

# Download golangci-lint locally if not already present
$(GOLANGCI_LINT):
	@echo "📦 Installing golangci-lint $(GOLANGCI_LINT_VERSION)..."
	@mkdir -p $(GOBIN)
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | \
		sh -s -- -b $(GOBIN) $(GOLANGCI_LINT_VERSION)
	@echo "✅ 'golangci-lint' installed successfully."

# Run linter
lint: $(GOLANGCI_LINT)
	@echo "🔍 Running golangci-lint..."
	@$(GOLANGCI_LINT) run --timeout=5m
	@echo "✅ Lint passed successfully!"
##################### "make lint" support end   ##########################

##################### "make format" support start ##########################
GOIMPORTS := $(GOBIN)/goimports

# Install goimports if not already available
$(GOIMPORTS):
	@echo "📦 Installing goimports..."
	@mkdir -p $(GOBIN)
	@go install golang.org/x/tools/cmd/goimports@latest
	@echo "✅ 'goimports' installed successfully."

# Format Go code using gofmt and goimports
format: $(GOIMPORTS)
	@echo "🧹 Formatting Go code..."
	@gofmt -s -w .
	@$(GOIMPORTS) -local github.com/kubev2v/assisted-migration-agent -w .
	@echo "✅ Format complete."

# Check that formatting does not introduce changes
check-format: format
	@echo "🔍 Checking if formatting is up to date..."
	@git diff --quiet || (echo "❌ Detected uncommitted changes after format. Run 'make format' and commit the result." && git status && exit 1)
	@echo "✅ All formatted files are up to date."
##################### "make format" support end   ##########################

# Check if generate changes the repo
check-generate: generate
	@echo "🔍 Checking if generated files are up to date..."
	@git diff --quiet || (echo "❌ Detected uncommitted changes after generate. Run 'make generate' and commit the result." && git status && exit 1)
	@echo "✅ All generated files are up to date."

validate-all: lint check-format tidy-check check-generate

##################### tests support start ##########################
GINKGO := $(GOBIN)/ginkgo
UNIT_TEST_PACKAGES := ./...
UNIT_TEST_GINKGO_OPTIONS ?= 
VCSIM_CONTAINER_NAME := vcsim-test
VCSIM_PORT := 8989
VCSIM_IMAGE := vmware/vcsim:latest


# Install ginkgo if not already available
$(GINKGO):
	@echo "📦 Installing ginkgo..."
	@go install -v github.com/onsi/ginkgo/v2/ginkgo@v2.22.0
	@echo "✅ 'ginkgo' installed successfully."

.PHONY: test vcsim
# Run unit tests using ginkgo
test: $(GINKGO) vcsim
	@echo "🧪 Running Unit tests..."
	@$(GINKGO) -v --show-node-events $(UNIT_TEST_GINKGO_OPTIONS) $(UNIT_TEST_PACKAGES)
	@echo "✅ All Unit tests passed successfully."

# Start vcsim container for testing
vcsim:
	@echo "🛑 Stopping vcsim container..."
	@$(PODMAN) stop $(VCSIM_CONTAINER_NAME) 2>/dev/null || true
	@$(PODMAN) rm $(VCSIM_CONTAINER_NAME) 2>/dev/null || true
	@echo "✅ vcsim stopped"

	@echo "🚀 Starting vcsim container using $(PODMAN)..."
	@$(PODMAN) run -d --name $(VCSIM_CONTAINER_NAME) --rm -p $(VCSIM_PORT):$(VCSIM_PORT) \
		$(VCSIM_IMAGE) -l :$(VCSIM_PORT) -dc 1 -cluster 1 -ds 1 -host 1 -vm 3

################################################################################
# Emoji Legend for Makefile Targets
#
# Action Type        | Emoji | Description
# -------------------|--------|------------------------------------------------
# Install tool        📦     Installing a dependency or binary
# Running task        ⚙️     Executing tasks like generate, build, etc.
# Linting/validation  🔍     Checking format, lint, static analysis, etc.
# Formatting          🧹     Formatting source code
# Success/complete    ✅     Task completed successfully
# Failure/alert       ❌     An error or failure occurred
# Teardown/cleanup    🗑️     Stopping, removing, or cleaning up resources
################################################################################
