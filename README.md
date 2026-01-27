# Assisted Migration Agent

An agent for collecting vSphere inventory data and reporting to the Migration Planner console.

## Build

```bash
make build
```

This produces the binary at `bin/agent`.

## Run

### Basic Usage

```bash
bin/agent run --agent-id <uuid> --source-id <uuid>
```

Both `--agent-id` and `--source-id` are required and must be valid UUIDs.

### Examples

Run in disconnected mode (default):

```bash
bin/agent run \
  --agent-id 550e8400-e29b-41d4-a716-446655440000 \
  --source-id 6ba7b810-9dad-11d1-80b4-00c04fd430c8
```

Run in connected mode with authentication:

```bash
bin/agent run \
  --agent-id 550e8400-e29b-41d4-a716-446655440000 \
  --source-id 6ba7b810-9dad-11d1-80b4-00c04fd430c8 \
  --mode connected \
  --authentication-enabled \
  --authentication-jwt-filepath /path/to/jwt
```

Run in production mode with persistent storage:

```bash
bin/agent run \
  --agent-id 550e8400-e29b-41d4-a716-446655440000 \
  --source-id 6ba7b810-9dad-11d1-80b4-00c04fd430c8 \
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
| `--opa-policies-folder` | — | Path to OPA policies folder for VM validation |
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
  --data-folder . \
  --authentication-enabled \
  --authentication-jwt-filepath ./jwt \
  --console-url https://console.stage.redhat.com/api/migration-assessment/
```
