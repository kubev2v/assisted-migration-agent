// Package config defines the configuration structure for the assisted-migration-agent.
//
// Configuration is organized into logical sections (Server, Agent, Console, Authentication)
// and uses code generation via optgen to create functional option helpers.
//
// # Configuration Structure
//
//	Configuration
//	├── Server         - HTTP server settings
//	├── Agent          - Agent behavior and identity
//	├── Console        - Console.redhat.com connection
//	├── Auth           - Authentication settings
//	├── LogFormat      - Logging format
//	└── LogLevel       - Logging verbosity
//
// # Server Configuration
//
//	┌──────────────────┬─────────┬────────────────────────────────────────┐
//	│ Field            │ Default │ Description                            │
//	├──────────────────┼─────────┼────────────────────────────────────────┤
//	│ ServerMode       │ "dev"   │ Server mode: "prod" or "dev"           │
//	│ HTTPPort         │ 8000    │ HTTP server listen port                │
//	│ StaticsFolder    │ ""      │ Path to static files for UI            │
//	└──────────────────┴─────────┴────────────────────────────────────────┘
//
// Server modes:
//   - prod: Production mode with stricter settings
//   - dev: Development mode with relaxed settings
//
// # Agent Configuration
//
//	┌─────────────────────┬────────────────┬──────────────────────────────────────┐
//	│ Field               │ Default        │ Description                          │
//	├─────────────────────┼────────────────┼──────────────────────────────────────┤
//	│ Mode                │ "disconnected" │ Initial agent mode                   │
//	│ ID                  │ ""             │ Agent UUID (required)                │
//	│ SourceID            │ ""             │ Source UUID (required)               │
//	│ Version             │ "v0.0.0"       │ Agent version string                 │
//	│ DataFolder          │ ""             │ Path to data storage (DuckDB)        │
//	│ OpaPoliciesFolder   │ ""             │ Path to OPA policy files             │
//	│ UpdateInterval      │ 5s             │ Console update frequency             │
//	│ LegacyStatusEnabled │ true           │ Use v1 agent status values           │
//	└─────────────────────┴────────────────┴──────────────────────────────────────┘
//
// Agent modes:
//   - connected: Agent sends updates to console.redhat.com
//   - disconnected: Agent operates in standalone mode
//
// # Console Configuration
//
//	┌───────┬─────────────────────────┬────────────────────────────────────────┐
//	│ Field │ Default                 │ Description                            │
//	├───────┼─────────────────────────┼────────────────────────────────────────┤
//	│ URL   │ "http://localhost:7443" │ Console API base URL                   │
//	└───────┴─────────────────────────┴────────────────────────────────────────┘
//
// # Authentication Configuration
//
//	┌─────────────┬─────────┬────────────────────────────────────────┐
//	│ Field       │ Default │ Description                            │
//	├─────────────┼─────────┼────────────────────────────────────────┤
//	│ Enabled     │ true    │ Enable JWT authentication              │
//	│ JWTFilePath │ ""      │ Path to JWT token file                 │
//	└─────────────┴─────────┴────────────────────────────────────────┘
//
// # Code Generation
//
// The package uses optgen to generate functional option helpers:
//
//	//go:generate go run github.com/ecordell/optgen -output zz_generated.configuration.go . Configuration Server Agent Console Authentication
//
// Generated helpers include:
//
//   - NewConfigurationWithOptions(...ConfigurationOption) - Create with options
//   - NewConfigurationWithOptionsAndDefaults(...ConfigurationOption) - Create with defaults + options
//   - WithServer(Server), WithAgent(Agent), etc. - Set nested structs
//   - DebugMap() - Returns map for debug logging (respects debugmap tags)
//
// # Usage Example
//
// Create configuration with defaults and overrides:
//
//	cfg := config.NewConfigurationWithOptionsAndDefaults(
//	    config.WithServer(config.Server{
//	        ServerMode: "prod",
//	        HTTPPort:   8080,
//	    }),
//	    config.WithAgent(config.Agent{
//	        ID:       "agent-uuid",
//	        SourceID: "source-uuid",
//	        Mode:     "connected",
//	    }),
//	    config.WithLogLevel("info"),
//	)
//
// Or create with individual options:
//
//	server := config.NewServerWithOptionsAndDefaults(
//	    config.WithHTTPPort(9000),
//	)
//
// # Debug Logging
//
// All fields are tagged with `debugmap:"visible"` allowing safe logging
// of configuration values via DebugMap():
//
//	log.Info("configuration loaded", zap.Any("config", cfg.DebugMap()))
//
// This produces a map suitable for structured logging without exposing
// sensitive values (if any were marked with `debugmap:"hidden"`).
package config
