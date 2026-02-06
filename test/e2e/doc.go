/*
Package main provides end-to-end testing infrastructure for the assisted-migration-agent.

# Package Structure

	test/e2e/
	├── main.go          Entry point: flags, config, InfraManager setup, Ginkgo runner
	├── tests.go         Ginkgo test specs (disconnected env, connected env, collector)
	├── doc.go           This file
	├── infra/           Infrastructure management
	│   ├── infra.go     InfraManager interface + AgentConfig + vcsim constants
	│   ├── container.go ContainerInfraManager (Podman-based)
	│   ├── vm.go        VMInfraManager (no-op, externally managed)
	│   ├── podman.go    PodmanRunner + ContainerConfig (low-level Podman API)
	│   ├── proxy.go     Reverse proxy (sits between agent and backend)
	│   └── observer.go  Request observer (collects proxy traffic for assertions)
	├── model/
	│   └── auth.go      User type for JWT auth
	├── service/
	│   ├── agent.go         AgentSvc — HTTP client for the agent's local API
	│   ├── interfaces.go    PlannerService interface
	│   ├── service_api.go   ServiceApi — HTTP client with JWT auth
	│   ├── service.go       PlannerSvc constructor
	│   ├── source.go        Source API methods on PlannerSvc
	│   └── assessment.go    Assessment API methods on PlannerSvc
	└── utils/
	    ├── auth.go      JWT token generation
	    ├── command.go   Shell command helpers
	    ├── file.go      File utilities
	    └── rvtools.go   RVTools helpers

# InfraManager

InfraManager is the central abstraction for infrastructure lifecycle:

	type InfraManager interface {
	    StartPostgres() / StopPostgres()
	    StartBackend()  / StopBackend()
	    StartVcsim()    / StopVcsim()
	    StartAgent(cfg) / StopAgent() / RestartAgent() / RemoveAgent()
	}

Two implementations:
  - ContainerInfraManager — uses Podman to start/stop containers (default).
  - VMInfraManager — no-op; infrastructure is managed externally (Kind + deploy/e2e.mk).

Selected via the -infra-mode flag ("container" or "vm").

# Proxy & Observer

The Proxy is an in-process reverse proxy that sits between the agent and the backend.
It forwards requests while cloning request/response data to a channel.
The Observer reads from that channel and accumulates requests for test assertions.

	┌─────────┐      ┌─────────┐      ┌─────────┐
	│  Agent  │─────▶│  Proxy  │─────▶│ Backend │
	└─────────┘      └────┬────┘      └─────────┘
	                      │
	                      ▼
	                 ┌──────────┐
	                 │ Observer │
	                 └──────────┘

In disconnected mode, the Proxy is used to verify that the agent does NOT contact
the backend. In connected mode, it is used for logging.

# Makefile Targets

	make e2e                  Run e2e tests (default: container mode)
	make e2e.container        Run e2e tests in container mode (Podman)
	make e2e.vm               Run e2e tests in VM mode (externally managed infra)
	make e2e.container.clean  Remove all e2e test containers and volumes
*/
package main
