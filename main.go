package main

import (
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/kubev2v/assisted-migration-agent/cmd"
	"github.com/kubev2v/assisted-migration-agent/internal/config"
	"github.com/kubev2v/assisted-migration-agent/pkg/logger"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "agent",
		Short: "Assisted Migration Agent",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
		},
	}

	// default configuration
	cfg := config.NewConfigurationWithOptionsAndDefaults(
		config.WithServer(config.Server{
			HTTPPort:   8000,
			ServerMode: "dev",
		}),
		config.WithAgent(config.Agent{
			ID: uuid.NewString(),
		}),
		config.WithAgent(config.Agent{
			NumWorkers:     3,
			Mode:           "disconnected",
			UpdateInterval: 5 * time.Second,
		}),
		config.WithAuth(config.Authentication{Enabled: false}),
		config.WithLogFormat("console"),
		config.WithLogLevel("debug"),
	)
	registerLoggingFlags(rootCmd, cfg)

	if err := validateConfig(cfg); err != nil {
		fmt.Printf("%s", err)
		os.Exit(1)
	}

	logger := logger.Init(cfg.LogFormat, cfg.LogLevel)
	defer func() { _ = logger.Sync() }()

	undo := zap.ReplaceGlobals(logger)
	defer undo()

	rootCmd.AddCommand(cmd.NewRunCommand(cfg))

	if err := rootCmd.Execute(); err != nil {
		fmt.Printf("%s", err)
		os.Exit(1)
	}
}

func validateConfig(cfg *config.Configuration) error {
	switch cfg.LogFormat {
	case "console":
	case "json":
	default:
		return fmt.Errorf("invalid log-format: %s", cfg.LogFormat)
	}

	if _, err := zapcore.ParseLevel(cfg.LogLevel); err != nil {
		return fmt.Errorf("invalid log level %s", cfg.LogLevel)
	}

	return nil
}

func registerLoggingFlags(cmd *cobra.Command, config *config.Configuration) {
	cmd.PersistentFlags().StringVar(&config.LogFormat, "log-format", config.LogFormat, "format of the logs: console or json")
	cmd.PersistentFlags().StringVar(&config.LogLevel, "log-level", config.LogLevel, "log level")
}
