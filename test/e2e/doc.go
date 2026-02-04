// Package main provides end-to-end testing infrastructure for the assisted-migration-agent.
//
// # Components
//
// Actioner performs HTTP requests against the migration-planner backend API.
// It provides methods to create/delete sources, create assessments, and generate
// JWT tokens for agent authentication.
//
// DbReadWriter provides direct database access for test setup and verification.
// It can read sources, agents, and assessments, as well as create and retrieve
// private keys used for JWT signing.
//
// Observer collects HTTP requests intercepted by the Proxy. It runs in a background
// goroutine and accumulates requests that can be retrieved for test assertions.
// Each test should create a new Observer and close it in AfterEach.
//
// Proxy is a reverse proxy that sits between the agent and the backend.
// It forwards all requests to the target backend while cloning request/response
// data and sending it to a channel for observation.
//
// Stack manages the container lifecycle for test infrastructure using Podman.
// It can start/stop PostgreSQL, the backend service, and the agent container.
// Containers run in host network mode for simplified networking.
//
// PodmanRunner is a low-level wrapper around the Podman API for container operations.
// It handles starting, stopping, removing containers, and retrieving logs.
//
// # Test Architecture
//
// The test setup uses the following flow:
//
//	┌─────────┐      ┌─────────┐      ┌─────────┐
//	│  Agent  │─────▶│  Proxy  │─────▶│ Backend │
//	└─────────┘      └────┬────┘      └─────────┘
//	                      │
//	                      ▼
//	                 ┌──────────┐
//	                 │ Observer │
//	                 └──────────┘
//
// 1. The Proxy listens on a local port (e.g., :8080) and forwards requests to the backend.
// 2. The Agent is configured to use the Proxy URL instead of the backend directly.
// 3. Each request passing through the Proxy is cloned and sent to a channel.
// 4. The Observer reads from the channel and accumulates requests for assertions.
//
// # Test Flow
//
// BeforeAll:
//   - Start PostgreSQL container
//   - Create Proxy targeting the backend
//   - Start HTTP server with Proxy handler
//
// BeforeEach:
//   - Create new Observer listening to Proxy's request channel
//   - Use Actioner to set up test data (create source, generate JWT, etc.)
//   - Use DbReadWriter to insert private keys for JWT validation
//
// Test:
//   - Start Agent container with appropriate configuration
//   - Wait for agent activity
//   - Retrieve requests from Observer
//   - Assert on request count, paths, headers, etc.
//
// AfterEach:
//   - Stop Agent container
//   - Close Observer
//
// AfterAll:
//   - Shutdown Proxy server
//   - Stop PostgreSQL container
//
// # Actioner Usage
//
// The Actioner is used to simulate user actions via the backend API:
//
//	actioner := NewActioner("http://localhost:7443")
//
//	// Create a source
//	sourceID, err := actioner.CreateSource("my-source")
//
//	// Generate agent token using a private key from the database
//	key, _ := db.GetPrivateKey(ctx, orgID)
//	token, err := actioner.GenerateAgentToken(sourceID, key.ID, key.PrivateKey)
//
//	// Create an assessment linked to the source
//	assessmentID, err := actioner.CreateAssessment("my-assessment", &sourceID)
//
//	// Clean up
//	err = actioner.DeleteSource(sourceID)
//
// # Test Plan
//
// ## 1. Mode at Startup (no backend required)
//   - starting in connected mode: tests should see requests to console
//   - starting in disconnected mode: no requests to console
//
// ## 2. Mode Switching (no backend required)
//   - switch from connected to disconnected mode
//   - switch from disconnected to connected mode
//   - persist mode after agent restart (volume preserved, env says connected but persisted state wins)
//
// ## 3. Collector (no backend required, agent starts in disconnected mode, uses vcsim container)
//   - nominal case: vcsim is up, collection successful, inventory served at /inventory, collector state "collected"
//   - bad credentials: collector state should be "error"
//   - bad vCenter URL: collector state should be "error"
//   - recovery: bad credentials first, then correct credentials, should see error then "collected"
//   - persist collected state after agent restart (volume preserved, collector still "collected", inventory available)
package main
