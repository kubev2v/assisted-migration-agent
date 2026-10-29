# Assisted Migration Agent

An agent for collecting vSphere inventory data and reporting to the Migration Planner console.

## Build

```bash
make build
```

This produces the binary at `bin/agent`.

Agent-specific code lives under `agent/`: `main.go` is the CLI entry point,
`cmd/` defines its commands, and `app/` assembles the application. `api/v2/`
defines the HTTP contract, and `handlers/v2/` implements it. Shared
implementation remains in `internal/` and `pkg/`. The RVTools proof of
concept remains available as an Agent mode and uses `agent/Containerfile.rvtools`.

## Container images

`build-config/ui-image` pins `AGENT_UI_IMAGE_TAG` to a UI commit SHA for both
`agent/Containerfile` and `agent/Containerfile.rvtools`. Local Make targets and
all Agent/RVTools Konflux pipelines use this config:

```bash
make image
```

For a direct Podman/Buildah build, pass the config explicitly:

```bash
podman build --build-arg-file build-config/ui-image --build-arg-file build-config/min-upgrade-version -f agent/Containerfile .
podman build --build-arg-file build-config/ui-image -f agent/Containerfile.rvtools .
```

For local testing, override the pin with `make image AGENT_UI_IMAGE_TAG=<sha>`
or `AGENT_UI_IMAGE_TAG=<sha> make image`.

### Upgrade compatibility metadata

The appliance image's `min-upgrade-version` label identifies the oldest installed
image version supported as the starting point for an upgrade to that image.
It is separate from `org.opencontainers.image.version`, which identifies the
candidate image itself.

`build-config/min-upgrade-version` maintains one minimum supported version per
environment. Appliance builds select the default with `IMAGE_ENV` (`dev`, `stage`,
or `production`):

```bash
make image                      # dev
make image IMAGE_ENV=stage
make image IMAGE_ENV=production
```

Konflux selects `dev` for pushes to `main`, `stage` for pushes to `stable`, and
`production` for Git tag builds. Pull request builds use `dev`. All appliance
pipelines read `build-config/min-upgrade-version`; the RVTools image is unaffected.

Direct Podman/Buildah builds load both files under `build-config/` and use
`--build-arg IMAGE_ENV=stage` (default: `dev`). An explicit
`MIN_UPGRADE_VERSION=<version>` build argument overrides the selected default,
also available through `make image MIN_UPGRADE_VERSION=<version>`.

Preserve the complete version string for the environment:

| Environment | Version format |
|-------------|----------------|
| Dev | `main-release-<major>.<minor>-<short-sha>` |
| Stage | `stable-release-<major>.<minor>-<short-sha>` |
| Production | `v<major>.<minor>.<patch>` |

The Containerfile validates the effective minimum version, including overrides,
before setting the label. Empty values, wrong environment prefixes, malformed
versions, and numeric components with leading zeros fail the build. Dev and stage
commit SHAs must contain 7-40 lowercase hexadecimal digits.

The minimum is an explicitly maintained compatibility boundary. Do not derive it
from the candidate image's version or order commit SHAs lexically. Update
`build-config/min-upgrade-version` when the supported upgrade starting point changes.

On pushes to the UI repository's `main` or `stable` branch, its notification
workflow waits for the SHA-tagged image, then dispatches `agent_ui_updated` to
this repository. The receiver checks the SHA against the UI source branch head
and rejects stale notifications before updating `build-config/ui-image` or
creating a PR on the matching branch. These PRs are automatically approved and
set to merge by rebase after required checks pass. Within `build-config/`,
auto-approval covers only `ui-image`; changes to `min-upgrade-version` require
review. Merging updates the UI used by subsequent container builds.
The `MIGRATION_PLANNER_SYNC_APP_ID` and `MIGRATION_PLANNER_SYNC_APP_PRIVATE_KEY`
secrets must be available in both repositories. The sync GitHub App must be
installed on this repository with contents and pull request write access.

## RVTools Analyzer

The Analyzer runs independently of Agent IDs and the Migration Planner console. It has its own API under `/api/v1`; the Agent's `--rvtools-mode` and `/api/v2/collector/rvtools` remain available.

Build and run it locally with separate data and policy folders:

```bash
make build.analyzer
mkdir -p ./analyzer/data
bin/rvtools-analyzer run --data-folder ./analyzer/data --opa-policies-folder ./policies --server-http-port 8001
```

Upload an RVTools workbook and query the result:

```bash
curl -F 'files=@/path/to/rvtools.xlsx' http://localhost:8001/api/v1/collector/rvtools
curl http://localhost:8001/api/v1/collector
curl http://localhost:8001/api/v1/virtualmachines
```

`GET /api/v1/collector` reports when analysis is `collected`. The full API contract is in [analyzer/api/v1/openapi.yaml](analyzer/api/v1/openapi.yaml). Native runs need DuckDB's SQLite scanner extension available in the data folder or downloadable on first use.

Build the Analyzer image with `make image.analyzer`. It bundles the same UI image used by the Agent RVTools image for now, along with OPA policies and the DuckDB extension. The image serves UI files from `/app/static` and uses `/var/lib/analyzer` for persistent data. Set `ANALYZER_UI_IMAGE` and `ANALYZER_UI_IMAGE_TAG` to select a different UI image. The Agent image remains `make image`.

The current Agent UI bundle calls `/api/v2`, while Analyzer exposes `/api/v1`. Its page and assets load in the Analyzer image, but its data views need an Analyzer-compatible UI build.

## Run

### Basic Usage

```bash
bin/agent run --agent-id <uuid> --source-id <uuid> --opa-policies-folder <path> --data-folder <existing-directory>
```

Required flags:
- `--agent-id` and `--source-id` must be valid UUIDs
- `--opa-policies-folder` must point to a directory containing OPA policy files for VM validation
- `--data-folder` must point to an existing directory

### Examples

Run in disconnected mode (default):

```bash
bin/agent run \
  --agent-id 550e8400-e29b-41d4-a716-446655440000 \
  --source-id 6ba7b810-9dad-11d1-80b4-00c04fd430c8 \
  --opa-policies-folder /path/to/policies \
  --data-folder /path/to/data
```

Run in connected mode with authentication:

```bash
bin/agent run \
  --agent-id 550e8400-e29b-41d4-a716-446655440000 \
  --source-id 6ba7b810-9dad-11d1-80b4-00c04fd430c8 \
  --opa-policies-folder /path/to/policies \
  --data-folder /path/to/data \
  --mode connected \
  --authentication-enabled \
  --authentication-jwt-filepath /path/to/jwt
```

Run in production mode with persistent storage:

```bash
bin/agent run \
  --agent-id 550e8400-e29b-41d4-a716-446655440000 \
  --source-id 6ba7b810-9dad-11d1-80b4-00c04fd430c8 \
  --opa-policies-folder /var/lib/agent/policies \
  --server-mode prod \
  --server-statics-folder /var/www/statics \
  --data-folder /var/lib/agent
```

## Command Line Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--agent-id` | *required* | Unique identifier (UUID) for this agent |
| `--source-id` | *required* | Source identifier (UUID) for this agent |
| `--mode` | `disconnected` | `connected` \| `disconnected` |
| `--data-folder` | *required* | Existing directory for persistent data |
| `--opa-policies-folder` | *required* | Existing directory containing OPA policies for VM validation |
| `--version` | `v0.0.0` | Agent version to report to console |
| `--legacy-status-enabled` | `true` | Use legacy status like waiting-for-credentials |
| `--server-http-port` | `8000` | HTTP server port |
| `--server-mode` | `dev` | `dev` \| `prod` (prod enables HTTPS with self-signed certs) |
| `--server-statics-folder` | — | Existing directory of static files (required when `--server-mode=prod`) |
| `--console-url` | `http://localhost:7443` | Migration planner console URL |
| `--console-update-interval` | `5s` | Status update interval |
| `--authentication-enabled` | `true` | Enable console authentication |
| `--authentication-jwt-filepath` | — | Path to JWT file (required when `--authentication-enabled`) |
| `--log-format` | `console` | `console` \| `json` |
| `--log-level` | `debug` | `debug` \| `info` \| `warn` \| `error` |

## Development

### Local Setup

Before running the agent locally, set up the required dependencies:

```bash
# Download OPA policies (required for VM validation)
make setup-opa-policies

# Build the Alpine filler image (required for forecaster benchmarks)
# Prerequisites: qemu-img, curl, guestfish, genisoimage
make build-filler-image
```

When running locally, point the agent to the filler image assets:

```bash
AGENT_ASSETS_DIR=pkg/vmware/assets make run
```

> Both of these are handled automatically during container builds (see `agent/Containerfile`).

### Tests

Run tests:

```bash
make test
```

Run linter:

```bash
make lint
```

Format code:

```bash
make format
```

Run all validations:

```bash
make validate-all
```

## E2E Tests

E2E tests require a built agent container image. Before running tests, build the latest image:

```bash
make image
```

### Make Targets

| Target | Description |
|--------|-------------|
| `make e2e` | Run e2e tests (default: container mode) |
| `make e2e.container` | Run e2e tests in container mode using Podman |
| `make e2e.vm` | Run e2e tests in VM mode (infrastructure managed externally) |
| `make e2e.container.clean` | Remove all e2e test containers and volumes |

### E2E Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-infra-mode` | `container` | Infrastructure mode: `container` (Podman) or `vm` (externally managed) |
| `-agent-image` | — | Agent container image to test |
| `-backend-image` | — | Backend container image (migration-planner-api) |
| `-backend-agent-endpoint` | `http://localhost:7443` | Agent endpoint on backend (port 7443) |
| `-backend-user-endpoint` | `http://localhost:3443` | User endpoint on backend (port 3443) |
| `-agent-proxy-url` | `http://localhost:8080` | Agent proxy URL |
| `-agent-api-url` | `https://localhost:8000` | Agent local API URL |
| `-podman-socket` | `unix:///run/user/1000/podman/podman.sock` | Podman socket path |
| `-iso-path` | — | Path to directory containing `rhcos-live-iso.x86_64.iso` |

Containers are automatically kept running when tests fail, allowing you to inspect logs and debug issues. Use `make e2e.container.clean` to remove them after debugging.

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `E2E_AGENT_IMAGE` | `quay.io/kubev2v/assisted-migration-agent:latest` | Agent image to use |
| `E2E_BACKEND_IMAGE` | `quay.io/kubev2v/migration-planner-api:latest` | Backend image to use |
| `E2E_ISO_PATH` | Current directory | Path containing the RHCOS ISO |
| `E2E_INFRA_MODE` | `container` | Infrastructure mode |

### Examples

Run e2e tests with a custom agent image:

```bash
make image
E2E_AGENT_IMAGE=quay.io/myrepo/agent:dev make e2e
```

Clean up after failed tests:

```bash
make e2e.container.clean
```

## Testing Against Stage

To test the agent against the staging backend, you need to generate a JWT token and run the agent with authentication enabled.

### Generate JWT Token

Use the migration-planner CLI to generate a JWT:

```bash
../migration-planner/bin/planner sso token \
  --org 12345678 \
  --private-key ./privatekey \
  --username your-username \
  --source-id <source-uuid> \
  --kid <key-id> > ./jwt
```

### Run Agent with Stage Backend

The `--agent-id` can be any valid UUID. Generate one with `uuidgen`.

```bash
bin/agent run \
  --legacy-status-enabled \
  --agent-id $(uuidgen) \
  --source-id <source-uuid> \
  --opa-policies-folder ./policies \
  --data-folder . \
  --authentication-enabled \
  --authentication-jwt-filepath ./jwt \
  --console-url https://console.stage.redhat.com/api/migration-assessment/
```
