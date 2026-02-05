/*
Package main provides end-to-end testing infrastructure for the assisted-migration-agent.

# Components

Actioner performs HTTP requests against the agent's local API.
It provides methods to get/set agent mode, start collector, get collector status,
and retrieve inventory.

BackendActioner performs HTTP requests against the migration-planner backend API.
It provides methods to create/delete sources and retrieve source details including
agent and inventory information.

DbReadWriter provides direct database access for test setup and verification.
It can read sources, agents, and assessments, as well as create and retrieve
private keys used for JWT signing.

Observer collects HTTP requests intercepted by the Proxy. It runs in a background
goroutine and accumulates requests that can be retrieved for test assertions.
Each test should create a new Observer and close it in AfterEach.

Proxy is a reverse proxy that sits between the agent and the backend.
It forwards all requests to the target backend while cloning request/response
data and sending it to a channel for observation.

Stack manages the container lifecycle for test infrastructure using Podman.
It can start/stop PostgreSQL, the backend service, vcsim, and the agent container.
Containers run in host network mode for simplified networking.

PodmanRunner is a low-level wrapper around the Podman API for container operations.
It handles starting, stopping, removing containers, and retrieving logs.

# Test Architecture

The test setup uses the following flow:

	┌─────────┐      ┌─────────┐      ┌─────────┐
	│  Agent  │─────▶│  Proxy  │─────▶│ Backend │
	└─────────┘      └────┬────┘      └─────────┘
	                      │
	                      ▼
	                 ┌──────────┐
	                 │ Observer │
	                 └──────────┘

1. The Proxy listens on a local port (e.g., :8080) and forwards requests to the backend.
2. The Agent is configured to use the Proxy URL instead of the backend directly.
3. Each request passing through the Proxy is cloned and sent to a channel.
4. The Observer reads from the channel and accumulates requests for assertions.

# Test Plan for Disconnected Environment

Prerequisites:
  - Postgres container running
  - Proxy running (to observe requests, no backend needed)
  - vcsim container running (for collector tests)

## 1. Mode at Startup

### 1.1 Start in Disconnected Mode

Given an agent configured to start in disconnected mode
When the agent starts up
Then no requests should be made to the console

### 1.2 Start in Connected Mode

Given an agent configured to start in connected mode
When the agent starts up
Then requests should be made to the console

## 2. Mode Switching

### 2.1 Switch from Connected to Disconnected

Given an agent running in connected mode making requests to console
When the agent mode is switched to disconnected
Then no new requests should be made to the console

### 2.2 Switch from Disconnected to Connected

Given an agent running in disconnected mode making no requests
When the agent mode is switched to connected
Then requests should start being made to the console

### 2.3 Persist Mode After Restart

Given an agent that was switched from connected to disconnected mode
When the agent container is restarted
Then the mode should persist as disconnected

## 3. Collector

### 3.1 Collect with Valid Credentials

Given an agent in disconnected mode with vcsim running
When valid vCenter credentials are provided to the collector
Then the collector should reach "collected" status and inventory should be available

### 3.2 Error with Bad Credentials

Given an agent in disconnected mode with vcsim running
When invalid credentials are provided to the collector
Then the collector should reach "error" status

### 3.3 Error with Bad vCenter URL

Given an agent in disconnected mode
When an unreachable vCenter URL is provided to the collector
Then the collector should reach "error" status

### 3.4 Recovery from Error

Given an agent in disconnected mode that failed collection with bad credentials
When valid credentials are provided on retry
Then the collector should recover and reach "collected" status

### 3.5 Persist Collected State After Restart

Given an agent that has successfully collected inventory
When the agent container is restarted
Then the collector status should persist as "collected" and inventory should still be available

# Test Plan for Connected Environment

Prerequisites:
  - Postgres container running
  - Backend container running with:
  - MIGRATION_PLANNER_AUTH=none (disables user authentication)
  - MIGRATION_PLANNER_AGENT_AUTH_ENABLED=false (disables agent authentication)
  - vcsim container running (for collector tests)
  - Source must be created via backend API before starting agent (agent needs source_id)

## 1. Mode at Startup

### 1.1 Register Agent on Startup

Given an agent configured in connected mode with a valid source ID
When the agent starts and registers with the backend
Then the source should have the agent attached with status "waiting-for-credentials"

## 2. Mode Switching

### 2.1 Register After Switching to Connected

Given an agent started in disconnected mode with a valid source ID
When the agent mode is switched to connected
Then the source should have the agent attached

## 3. Collector

### 3.1 Push Inventory to Backend

Given an agent in connected mode with valid vCenter credentials
When the collector successfully gathers inventory
Then the source should have the inventory populated
*/
package main
