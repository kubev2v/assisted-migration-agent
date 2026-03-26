# Assisted Migration Agent nargaman test

An agent for collecting vSphere inventory data and reporting to the Migration Planner console.

## Build

```bash
make build
```

This produces the binary at `bin/agent`.

## Run

### Basic Usage

```bash
bin/agent run --agent-id <uuid> --source-id <uuid> --opa-policies-folder <path>
```

Required flags:
- `--agent-id` and `--source-id` must be valid UUIDs
- `--opa-policies-folder` must point to a directory containing OPA policy files for VM validation

### Examples

Run in disconnected mode (default):

```bash
bin/agent run \
  --agent-id 550e8400-e29b-41d4-a716-446655440000 \
  --source-id 6ba7b810-9dad-11d1-80b4-00c04fd430c8 \
  --opa-policies-folder /path/to/policies
```

Run in connected mode with authentication:

```bash
bin/agent run \
  --agent-id 550e8400-e29b-41d4-a716-446655440000 \
  --source-id 6ba7b810-9dad-11d1-80b4-00c04fd430c8 \
  --opa-policies-folder /path/to/policies \
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
| `--data-folder` | — | Path to persistent data folder (uses in-memory if not set) |
| `--opa-policies-folder` | *required* | Path to OPA policies folder for VM validation |
| `--num-workers` | `3` | Number of scheduler workers |
| `--version` | `v0.0.0` | Agent version to report to console |
| `--legacy-status-enabled` | `true` | Use legacy status like waiting-for-credentials |
| `--server-http-port` | `8000` | HTTP server port |
| `--server-mode` | `dev` | `dev` \| `prod` (prod enables HTTPS with self-signed certs) |
| `--server-statics-folder` | — | Path to static files (required when `--server-mode=prod`) |
| `--console-url` | `http://localhost:7443` | Migration planner console URL |
| `--console-update-interval` | `5s` | Status update interval |
| `--authentication-enabled` | `true` | Enable console authentication |
| `--authentication-jwt-filepath` | — | Path to JWT file (required when `--authentication-enabled`) |
| `--log-format` | `console` | `console` \| `json` |
| `--log-level` | `debug` | `debug` \| `info` \| `warn` \| `error` |

## Development

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
