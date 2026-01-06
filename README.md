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

### Agent

| Flag | Default | Description |
|------|---------|-------------|
| `--agent-id` | (required) | Unique identifier (UUID) for this agent |
| `--source-id` | (required) | Source identifier (UUID) for this agent |
| `--mode` | `disconnected` | Agent mode: `connected` or `disconnected` |
| `--data-folder` | (none) | Path to persistent data folder. If not set, uses in-memory database |
| `--opa-policies-folder` | (none) | Path to OPA policies folder for VM validation |
| `--num-workers` | `3` | Number of scheduler workers |
| `--version` | (none) | Agent version to report to console |

### Server

| Flag | Default | Description |
|------|---------|-------------|
| `--server-http-port` | `8080` | Port on which the HTTP server listens |
| `--server-mode` | `dev` | Server mode: `dev` or `prod`. Production mode enables HTTPS with self-signed certificates |
| `--server-statics-folder` | (none) | Path to static files folder. Required when server mode is `prod` |

### Console

| Flag | Default | Description |
|------|---------|-------------|
| `--console-url` | `http://localhost:7443` | URL of the migration planner console |
| `--console-update-interval` | `5s` | Interval for console status updates |

### Authentication

| Flag | Default | Description |
|------|---------|-------------|
| `--authentication-enabled` | `false` | Enable authentication when connecting to console |
| `--authentication-jwt-filepath` | (none) | Path to JWT file. Required when authentication is enabled |

### Logging

| Flag | Default | Description |
|------|---------|-------------|
| `--log-format` | `console` | Log format: `console` or `json` |
| `--log-level` | `debug` | Log level: `debug`, `info`, `warn`, `error` |

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
