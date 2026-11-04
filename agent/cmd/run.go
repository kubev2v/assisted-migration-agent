package cmd

import (
	"errors"
	"fmt"

	"github.com/fatih/color"
	"github.com/go-extras/cobraflags"
	"github.com/google/uuid"
	"github.com/jzelinskie/cobrautil/v2"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"go.uber.org/zap"

	"github.com/kubev2v/assisted-migration-agent/agent/app"
	"github.com/kubev2v/assisted-migration-agent/internal/config"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/util"
)

func NewRunCommand(cfg *config.Configuration) *cobra.Command {
	runCmd := &cobra.Command{
		Use:   "run",
		Short: "Run agent",
		Args:  cobra.NoArgs,
		Example: `  # Run agent in disconnected mode
  agent run --agent-id 550e8400-e29b-41d4-a716-446655440000 --source-id 6ba7b810-9dad-11d1-80b4-00c04fd430c8

  # Run agent in connected mode with authentication
  agent run --mode connected --agent-id 550e8400-e29b-41d4-a716-446655440000 --source-id 6ba7b810-9dad-11d1-80b4-00c04fd430c8 --authentication-enabled --authentication-jwt-filepath /path/to/jwt

  # Run agent in production mode
  agent run --agent-id 550e8400-e29b-41d4-a716-446655440000 --source-id 6ba7b810-9dad-11d1-80b4-00c04fd430c8 --server-mode prod --server-statics-folder /var/www/statics`,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return validateConfiguration(cfg)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return app.Run(cfg)
		},
	}

	registerFlags(runCmd, cfg)
	cobraflags.CobraOnInitialize("AGENT", runCmd)

	return runCmd
}

func registerFlags(cmd *cobra.Command, config *config.Configuration) {
	nfs := cobrautil.NewNamedFlagSets(cmd)

	serverFlagSet := nfs.FlagSet(color.New(color.FgBlue, color.Bold).Sprint("Server"))
	registerServerFlags(serverFlagSet, config)

	authenticationFlagSet := nfs.FlagSet(color.New(color.FgBlue, color.Bold).Sprint("Authentication"))
	registerAuthenticationFlags(authenticationFlagSet, config)

	agentFlagSet := nfs.FlagSet(color.New(color.FgBlue, color.Bold).Sprint("Agent"))
	registerAgentFlags(agentFlagSet, config)

	consoleFlagSet := nfs.FlagSet(color.New(color.FgBlue, color.Bold).Sprint("Console"))
	registerConsoleFlags(consoleFlagSet, config)

	nfs.AddFlagSets(cmd)
}

func validateConfiguration(cfg *config.Configuration) error {
	if valid, err := util.IsValidDirectory(cfg.Agent.DataFolder); !valid {
		return fmt.Errorf("data-folder parameter is invalid: %w", err)
	}
	if valid, err := util.IsValidDirectory(cfg.Agent.OpaPoliciesFolder); !valid {
		return fmt.Errorf("opa-policies-folder parameter is invalid: %w", err)
	}

	if config.ServerModeType(cfg.Server.ServerMode) == config.ServerModeProd {
		if valid, err := util.IsValidDirectory(cfg.Server.StaticsFolder); !valid {
			return fmt.Errorf("server-statics-folder parameter is invalid: %w", err)
		}
	}

	if cfg.Server.HTTPPort < 1 || cfg.Server.HTTPPort > 65535 {
		return fmt.Errorf("invalid http-port %d: must be between 1 and 65535", cfg.Server.HTTPPort)
	}

	if cfg.Auth.Enabled && cfg.Auth.JWTFilePath == "" {
		return errors.New("authentication-jwt-filepath must be set when authentication is enabled")
	}

	switch config.ServerModeType(cfg.Server.ServerMode) {
	case config.ServerModeProd, config.ServerModeDev:
	default:
		return fmt.Errorf("invalid server mode %q: must be %q or %q", cfg.Server.ServerMode, config.ServerModeProd, config.ServerModeDev)
	}

	// validate flags for rvtools mode
	// In rvtools mode:
	// - we don't care about agent-id and source-id.
	// - only v2 api
	// - connected / disconnected ignored. the agent is in disconnected only
	if cfg.Agent.RVToolsMode {
		if cfg.Agent.Mode == "connected" {
			zap.S().Info("In rvtools-mode, connected mode ignored. The agent is running only in disconnected mode")
		}

		if cfg.Agent.Version == "v1" {
			return fmt.Errorf("RVTools mode is available only in api v2")
		}

		cfg.Agent.Mode = "disconnected"
		// need to set random UUIDs here because console service.
		cfg.Agent.ID = uuid.NewString()
		cfg.Agent.SourceID = uuid.NewString()

		return nil
	}

	if err := validateUUID(cfg.Agent.ID, "agent-id"); err != nil {
		return err
	}
	if err := validateUUID(cfg.Agent.SourceID, "source-id"); err != nil {
		return err
	}

	switch models.AgentMode(cfg.Agent.Mode) {
	case models.AgentModeConnected, models.AgentModeDisconnected:
	default:
		return fmt.Errorf("invalid mode %q: must be %q or %q", cfg.Agent.Mode, models.AgentModeConnected, models.AgentModeDisconnected)
	}

	return nil
}

func validateUUID(value, name string) error {
	if value == "" {
		return fmt.Errorf("%s cannot be empty", name)
	}
	if _, err := uuid.Parse(value); err != nil {
		return fmt.Errorf("%s must be a valid UUID: %w", name, err)
	}
	return nil
}

func registerServerFlags(flagSet *pflag.FlagSet, config *config.Configuration) {
	flagSet.IntVar(&config.Server.HTTPPort, "server-http-port", config.Server.HTTPPort, "Port on which the HTTP server is listening")
	flagSet.StringVar(&config.Server.StaticsFolder, "server-statics-folder", config.Server.StaticsFolder, "Existing directory for static files (required in production mode)")
	flagSet.StringVar(&config.Server.ServerMode, "server-mode", config.Server.ServerMode, "Server mode: either prod or dev. If prod the statics folder must be set")
}

func registerAuthenticationFlags(flagSet *pflag.FlagSet, config *config.Configuration) {
	flagSet.BoolVar(&config.Auth.Enabled, "authentication-enabled", config.Auth.Enabled, "Enable authentication when connecting to console")
	flagSet.StringVar(&config.Auth.JWTFilePath, "authentication-jwt-filepath", config.Auth.JWTFilePath, "Path of the jwt file")
}

func registerAgentFlags(flagSet *pflag.FlagSet, config *config.Configuration) {
	flagSet.StringVar(&config.Agent.Mode, "mode", config.Agent.Mode, "Agent mode: connected or disconnected")
	flagSet.StringVar(&config.Agent.OpaPoliciesFolder, "opa-policies-folder", config.Agent.OpaPoliciesFolder, "Existing directory containing OPA policies")
	flagSet.StringVar(&config.Agent.ID, "agent-id", config.Agent.ID, "Unique identifier (UUID) for this agent")
	flagSet.StringVar(&config.Agent.SourceID, "source-id", config.Agent.SourceID, "Source identifier (UUID) for this agent")
	flagSet.StringVar(&config.Agent.Version, "version", config.Agent.Version, "Agent version to report to console")
	flagSet.StringVar(&config.Agent.DataFolder, "data-folder", config.Agent.DataFolder, "Path to the persistent data folder")
	flagSet.BoolVar(&config.Agent.AutoMigrate, "auto-migrate", config.Agent.AutoMigrate, "Apply database migrations on startup; disable after offline migration")
	flagSet.BoolVar(&config.Agent.RVToolsMode, "rvtools-mode", config.Agent.RVToolsMode, "RVTool mode: enabled or disabled (default: disable)")
}

func registerConsoleFlags(flagSet *pflag.FlagSet, config *config.Configuration) {
	flagSet.StringVar(&config.Console.URL, "console-url", config.Console.URL, "URL of console.redhat.com")
	flagSet.DurationVar(&config.Agent.UpdateInterval, "console-update-interval", config.Agent.UpdateInterval, "Interval for console status updates")
}
