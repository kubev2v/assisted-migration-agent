package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/ecordell/optgen/helpers"
	"github.com/fatih/color"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jzelinskie/cobrautil/v2"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"go.uber.org/zap"

	v1 "github.com/tupyy/assisted-migration-agent/api/v1"
	"github.com/tupyy/assisted-migration-agent/internal/config"
	"github.com/tupyy/assisted-migration-agent/internal/handlers"
	"github.com/tupyy/assisted-migration-agent/internal/models"
	"github.com/tupyy/assisted-migration-agent/internal/server"
	"github.com/tupyy/assisted-migration-agent/internal/services"
	"github.com/tupyy/assisted-migration-agent/pkg/console"
	"github.com/tupyy/assisted-migration-agent/pkg/scheduler"
)

func NewRunCommand(cfg *config.Configuration) *cobra.Command {
	runCmd := &cobra.Command{
		Use:   "run",
		Short: "Run agent",
		Example: `  # Run agent in disconnected mode
  agent run --agent-id 550e8400-e29b-41d4-a716-446655440000 --source-id 6ba7b810-9dad-11d1-80b4-00c04fd430c8

  # Run agent in connected mode with authentication
  agent run --mode connected --agent-id 550e8400-e29b-41d4-a716-446655440000 --source-id 6ba7b810-9dad-11d1-80b4-00c04fd430c8 --authentication-enabled --authentication-jwt-filepath /path/to/jwt

  # Run agent in production mode
  agent run --agent-id 550e8400-e29b-41d4-a716-446655440000 --source-id 6ba7b810-9dad-11d1-80b4-00c04fd430c8 --server-mode prod --server-statics-folder /var/www/statics`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateConfiguration(cfg); err != nil {
				return err
			}

			zap.S().Infow("using configuration",
				"agent", helpers.Flatten(cfg.Agent.DebugMap()),
				"server", helpers.Flatten(cfg.Server.DebugMap()),
				"console", helpers.Flatten(cfg.Console.DebugMap()),
			)

			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGQUIT)
			wg := sync.WaitGroup{}
			wg.Add(1)

			// init scheduler
			sched := scheduler.NewScheduler(cfg.Agent.NumWorkers)
			defer sched.Close()

			// init console client
			consoleClient := console.NewConsoleClient(cfg.Console.URL)

			// init services
			var consoleSrv *services.Console
			if models.AgentMode(cfg.Agent.Mode) == models.AgentModeConnected {
				consoleSrv = services.NewConnectedConsoleService(cfg.Agent, sched, consoleClient)
			} else {
				consoleSrv = services.NewConsoleService(cfg.Agent, sched, consoleClient)
			}
			collectorSrv := services.NewCollectorService(sched)

			// init handlers
			h := handlers.New(consoleSrv, collectorSrv)

			srv, err := server.NewServer(cfg, func(router *gin.RouterGroup) {
				v1.RegisterHandlers(router, h)
			})
			if err != nil {
				zap.S().Errorw("failed to create http server", "error", err)
				return err
			}

			go func() {
				defer func() {
					wg.Done()
					cancel()
				}()
				zap.S().Infof("Starting HTTP server on port %d", cfg.Server.HTTPPort)

				if err := srv.Start(ctx); err != nil {
					if !errors.Is(err, http.ErrServerClosed) {
						zap.S().Errorw("failed to start http server", "error", err)
					}
				}
			}()

			go func() {
				<-ctx.Done()
				stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				srv.Stop(stopCtx)
			}()

			<-ctx.Done()
			wg.Wait()

			zap.S().Info("server shutdown")

			return nil
		},
	}

	registerFlags(runCmd, cfg)

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

	switch config.ServerModeType(cfg.Server.ServerMode) {
	case config.ServerModeProd, config.ServerModeDev:
	default:
		return fmt.Errorf("invalid server mode %q: must be %q or %q", cfg.Server.ServerMode, config.ServerModeProd, config.ServerModeDev)
	}

	if config.ServerModeType(cfg.Server.ServerMode) == config.ServerModeProd && cfg.Server.StaticsFolder == "" {
		return errors.New("statics folder must be set when server mode is production")
	}

	if cfg.Server.HTTPPort < 1 || cfg.Server.HTTPPort > 65535 {
		return fmt.Errorf("invalid http-port %d: must be between 1 and 65535", cfg.Server.HTTPPort)
	}

	if cfg.Agent.NumWorkers < 1 {
		return fmt.Errorf("invalid num-workers %d: must be at least 1", cfg.Agent.NumWorkers)
	}

	if cfg.Auth.Enabled && cfg.Auth.JWTFilePath == "" {
		return errors.New("authentication-jwt-filepath must be set when authentication is enabled")
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
	flagSet.StringVar(&config.Server.StaticsFolder, "server-statics-folder", config.Server.StaticsFolder, "Path to statics folder")
	flagSet.StringVar(&config.Server.ServerMode, "server-mode", config.Server.ServerMode, "Server mode: either prod or dev. If prod the statics folder must be set")
}

func registerAuthenticationFlags(flagSet *pflag.FlagSet, config *config.Configuration) {
	flagSet.BoolVar(&config.Auth.Enabled, "authentication-enabled", config.Auth.Enabled, "Enable authentication when connecting to console")
	flagSet.StringVar(&config.Auth.JWTFilePath, "authentication-jwt-filepath", config.Auth.JWTFilePath, "Path of the jwt file")
}

func registerAgentFlags(flagSet *pflag.FlagSet, config *config.Configuration) {
	flagSet.StringVar(&config.Agent.Mode, "mode", config.Agent.Mode, "Agent mode: connected or disconnected")
	flagSet.StringVar(&config.Agent.OpaPoliciesFolder, "opa-policies-folder", config.Agent.OpaPoliciesFolder, "Path to the OPA policies folder")
	flagSet.StringVar(&config.Agent.ID, "agent-id", config.Agent.ID, "Unique identifier (UUID) for this agent")
	flagSet.StringVar(&config.Agent.SourceID, "source-id", config.Agent.SourceID, "Source identifier (UUID) for this agent")
	flagSet.IntVar(&config.Agent.NumWorkers, "num-workers", config.Agent.NumWorkers, "Number of scheduler workers")
	flagSet.StringVar(&config.Agent.DataFolder, "data-folder", config.Agent.DataFolder, "Path to the persistent data folder")
}

func registerConsoleFlags(flagSet *pflag.FlagSet, config *config.Configuration) {
	flagSet.StringVar(&config.Console.URL, "console-url", config.Console.URL, "URL of console.redhat.com")
	flagSet.DurationVar(&config.Agent.UpdateInterval, "console-update-interval", config.Agent.UpdateInterval, "Interval for console status updates")
}
