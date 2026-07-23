# E2E Test Architecture

## Overview

The e2e tests validate the assisted-migration-agent's behavior in both **disconnected**
(agent runs standalone) and **connected** (agent talks to a migration-planner backend)
environments. The test binary is a standalone Go program (not `go test`) that uses
Ginkgo/Gomega and is driven by CLI flags.

## Component Diagram

```
                        ┌──────────────┐
                        │  OIDC Server │  (in-process)
                        │  :9090       │
                        │  ┌─────────┐ │
                        │  │ RSA key │ │
                        │  └─────────┘ │
                        │  /.well-known│
                        │  /certs      │
                        │  /token      │
                        └──────┬───────┘
                               │ JWKS URL
                               ▼
┌─────────┐  :8080   ┌─────────────┐  :7443/3443  ┌───────────┐    ┌──────────┐
│  Agent  │────────▶│    Proxy    │────────────▶│  Backend  │◀──│ Postgres │
│  :8000  │          │ (disconnect)│              │ (planner) │    │  :5432   │
└─────────┘          └──────┬──────┘              └───────────┘    └──────────┘
     │                      │                          ▲
     │  :8081        ┌──────┴──────┐                   │
     └─────────────▶│    Proxy    │───────────────────┘
       (connected)   │  (logging)  │
                     └──────┬──────┘
                            │
                       ┌────▼─────┐
                       │ Observer │  (collects requests for assertions)
                       └──────────┘

┌─────────┐
│  vcsim  │  :8989  (simulated vCenter)
└─────────┘
```

## Execution Flow

```
main.go
  ├── Parse flags (--infra-mode, --agent-image, --backend-image, ...)
  ├── Validate configuration
  ├── Create InfraManager (container or vm)
  └── RunSpecs → tests.go

tests.go (Ginkgo Ordered suite)
  ├── BeforeAll: StartPostgres
  │
  ├── Context "disconnected env"
  │   ├── BeforeAll: start Proxy on :8080 (between agent → backend)
  │   ├── Context "mode at startup"
  │   │   ├── BeforeEach: create Observer, AgentSvc
  │   │   ├── It: disconnected mode → no requests to console
  │   │   └── It: connected mode → requests to console
  │   ├── Context "mode switching"
  │   │   ├── It: connected → disconnected stops requests
  │   │   ├── It: disconnected → connected starts requests
  │   │   └── It: mode persists after restart
  │   └── Context "collector"
  │       ├── BeforeAll: StartVcsim
  │       ├── It: valid credentials → collected + inventory
  │       ├── It: bad credentials → error
  │       ├── It: bad URL → error
  │       ├── It: recovery from bad → good credentials
  │       └── It: collected state persists after restart
  │
  ├── Context "connected env"
  │   ├── BeforeAll: StartOIDC → StartBackend → NewPlannerServiceWithOIDC → start Proxy on :8081
  │   ├── Context "mode at startup"
  │   │   ├── BeforeEach: WithAuthUser → CreateSource
  │   │   └── It: agent registers with backend (status: waiting-for-credentials)
  │   ├── Context "mode switch"
  │   │   └── It: disconnected → connected registers agent
  │   └── Context "collector"
  │       ├── BeforeAll: StartVcsim
  │       └── It: collected inventory pushed to backend
  │
  └── AfterAll: StopPostgres
```

## Layers

### 1. Infrastructure (`infra/`)

**InfraManager** is the central abstraction. It owns the full lifecycle of all
external dependencies the tests need:

| Method             | Container mode (Podman)        | VM mode (external)      |
|--------------------|--------------------------------|-------------------------|
| `StartOIDC`        | In-process OIDC server         | In-process OIDC server  |
| `StopOIDC`         | Stops in-process server        | Stops in-process server |
| `GenerateToken`    | Signs JWT with OIDC private key| Signs JWT with OIDC key |
| `StartPostgres`    | Podman container               | No-op                   |
| `StartBackend`     | Podman container + OIDC config | No-op                   |
| `StartVcsim`       | Podman container               | No-op                   |
| `StartAgent`       | Podman container               | No-op                   |
| `Stop/Remove/...`  | Container lifecycle            | No-op                   |

The OIDC server is always in-process (both modes) because token generation is
needed regardless of how infra is deployed.

**Container mode**: `ContainerInfraManager` uses `PodmanRunner` to manage
containers via the Podman REST API over a unix socket. When the OIDC server is
active, `StartBackend` automatically configures the backend with:
- `MIGRATION_PLANNER_AUTH=rhsso`
- `MIGRATION_PLANNER_JWK_URL=<oidc-server>/openid-connect/certs`

**VM mode**: `VMInfraManager` is a no-op for container lifecycle (Postgres,
backend, vcsim, agent are deployed externally via Kind + `deploy/e2e.mk`).

**OIDCServer** (`oidc.go`): An in-process mock OIDC identity provider.
- Generates an RSA-2048 key pair at startup.
- Serves `/.well-known/openid-configuration` (OIDC discovery).
- Serves `/openid-connect/certs` (JWKS with the public key).
- Serves `POST /token` (HTTP API for token generation).
- `GenerateToken(username, orgID, email)` signs JWTs programmatically (RS256).
- This enables e2e tests to use the production `RHSSOAuthenticator` in the
  backend rather than `MIGRATION_PLANNER_AUTH=none`.

**Proxy** (`proxy.go`): In-process reverse proxy between agent and backend.
Clones request/response data to a channel without altering traffic.

**Observer** (`observer.go`): Reads from the Proxy's channel and accumulates
requests. Tests use `obs.Requests()` to assert on traffic patterns (e.g. "no
requests in disconnected mode").

### 2. Services (`service/`)

**AgentSvc** (`agent.go`): HTTP client for the agent's local API (`/api/v1/`).
- `Status()` — agent health/status
- `SetAgentMode(mode)` — switch connected/disconnected
- `StartCollector(url, user, pass)` — trigger vCenter collection
- `GetCollectorStatus()` — poll collector progress
- `Inventory()` — retrieve collected inventory

**PlannerSvc** (`service.go`, `source.go`, `assessment.go`): HTTP client for
the migration-planner backend. All requests carry a JWT in `X-Authorization`.

Token flow:
```
infraManager.GenerateToken ──▶ PlannerSvc.tokenGen (function reference)
                                      │
plannerSvc.WithAuthUser(u, o, e) ─────┘
    │
    ▼  calls tokenGen → gets JWT
    │
    returns new PlannerSvc with token in ServiceApi.jwtToken
    │
    .CreateSource("name")
    │
    ServiceApi.prepareRequest()
    │
    req.Header["X-Authorization"] = "Bearer <jwt>"
```

Usage pattern in tests:
```go
plannerSvc = service.NewPlannerServiceWithOIDC(backendURL, infraManager.GenerateToken)
userSvc    = plannerSvc.WithAuthUser("admin", "admin", "admin@example.com")
source, _  = userSvc.CreateSource("my-source")
source, _  = userSvc.GetSource(sourceID)
```

**ServiceApi** (`service_api.go`): Low-level HTTP client that handles URL
construction, JWT injection, and content-type headers. Shared by PlannerSvc.

### 3. Model & Utils

**model/auth.go**: `User` struct (username, organization, email).

**utils/auth.go**: Legacy token generation (reads RSA private key from disk).
Being replaced by `OIDCServer.GenerateToken` via the `WithAuthUser` pattern.

## Makefile Targets

| Target                 | Description                                     |
|------------------------|-------------------------------------------------|
| `make e2e`             | Run e2e tests (default: container mode)         |
| `make e2e.container`   | Run e2e tests in container mode (Podman)        |
| `make e2e.vm`          | Run e2e tests in VM mode (externally managed)   |
| `make e2e.container.clean` | Remove all e2e containers and volumes       |

## TODO

### Make current tests work properly with the current architecture

- [ ] Clean up legacy token generation (`utils/auth.go`, `GetToken`, `ParsePrivateKey`) now that OIDC server handles all token generation.
- [ ] Remove `model/auth.go` `User` struct and `DefaultPlannerService`/`NewPlannerService` constructors once all callers use `WithAuthUser`.
- [ ] Consider extracting default user constants ("admin", "admin", "admin@example.com") to avoid repetition in test contexts.
- [ ] Ensure disconnected env tests work end-to-end (proxy, observer, agent lifecycle).
- [ ] Ensure connected env tests work end-to-end (OIDC → backend → agent registration → collector → inventory push).

### VM-based agent deployment (libvirt)

In VM mode, `StartAgent` is **not** a no-op. The flow is:

1. The test creates a source via `PlannerSvc` (`userSvc.CreateSource(...)`).
2. The test requests the bootable ISO URL from the backend's image endpoint
   (`userSvc.GetImageUrl(sourceID)`).
3. The ISO URL is passed to `infraManager.StartAgent(cfg)` along with other
   agent config.
4. `VMInfraManager.StartAgent` downloads the ISO and deploys it as a VM using
   the **libvirt Go library** (`libvirt-go` / `libvirt.org/go/libvirt`).
   - Creates a storage volume from the ISO.
   - Defines and starts a libvirt domain (VM) booting from the ISO.
   - The agent runs inside the VM as it would in production (OVA-based).
5. `StopAgent` / `RemoveAgent` destroy the libvirt domain and clean up storage.
6. `RestartAgent` reboots the domain.

This means:
- [ ] `AgentConfig` needs an additional field for the ISO URL (or the image
  bytes/path) so the test can pass the downloaded ISO to `StartAgent`.
- [ ] `VMInfraManager` needs a libvirt connection (`libvirt.Connect`) initialized
  at construction time (e.g. `qemu:///system` or `qemu:///session`).
- [ ] Implement `VMInfraManager.StartAgent`: create storage pool/volume, define
  domain XML, start domain.
- [ ] Implement `VMInfraManager.StopAgent` / `RemoveAgent`: destroy domain,
  delete volume.
- [ ] Implement `VMInfraManager.RestartAgent`: `domain.Reboot()` or
  destroy + start.
- [ ] The test flow in connected env needs to be aware that in VM mode it must
  fetch the ISO before starting the agent:
  ```go
  // In BeforeEach (connected env):
  imageURL, err := userSvc.GetImageUrl(sourceID)
  agentCfg.ISOUrl = imageURL
  infraManager.StartAgent(agentCfg)
  ```
- [ ] Add libvirt Go dependency (`libvirt.org/go/libvirt`) and any build
  constraints needed (libvirt headers on the build host).
- [ ] Add `--libvirt-uri` flag to `main.go` for VM mode (default:
  `qemu:///session`).