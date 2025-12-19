.PHONY: generate generate.proto build run help tidy tidy-check clean lint format check-format check-generate validate-all

PODMAN ?= podman
GIT_COMMIT=$(shell git rev-list -1 HEAD --abbrev-commit)

BINARY_NAME=agent
BINARY_PATH=bin/$(BINARY_NAME)
MAIN_PATH=./main.go

GOBASE=$(shell pwd)
GOBIN=$(GOBASE)/bin
GO_BUILD_FLAGS := ${GO_BUILD_FLAGS}

.EXPORT_ALL_VARIABLES:

help:
	@echo "Targets:"
	@echo "    build:           "
	@echo "    run:             "
	@echo "    clean:           clean up golangci-lint and other tools"
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

clean:
	@echo "🗑️ Removing $(BINARY_PATH)..."
	- rm -f $(BINARY_PATH)
	@echo "✅ Clean complete."

run:
	$(BINARY_PATH) run

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
	@$(GOIMPORTS) -w .
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

# Install ginkgo if not already available
$(GINKGO):
	@echo "📦 Installing ginkgo..."
	@go install -v github.com/onsi/ginkgo/v2/ginkgo@v2.22.0
	@echo "✅ 'ginkgo' installed successfully."

.PHONY: test
# Run unit tests using ginkgo
test: $(GINKGO)
	@echo "🧪 Running Unit tests..."
	@$(GINKGO) -v --show-node-events $(UNIT_TEST_GINKGO_OPTIONS) $(UNIT_TEST_PACKAGES)
	@echo "✅ All Unit tests passed successfully."


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
