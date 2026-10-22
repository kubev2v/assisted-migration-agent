package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/kubev2v/assisted-migration-agent/internal/config"
	"github.com/kubev2v/assisted-migration-agent/pkg/logger"
)

func NewRootCommand(version, gitCommit, uiGitCommit string) *cobra.Command {
	cfg := config.NewConfigurationWithOptionsAndDefaults(
		config.WithServer(config.Server{
			HTTPPort:   8000,
			ServerMode: "dev",
		}),
		config.WithAgent(config.Agent{
			Version:        version,
			GitCommit:      gitCommit,
			UIGitCommit:    uiGitCommit,
			Mode:           "disconnected",
			UpdateInterval: 5 * time.Second,
		}),
		config.WithAuth(config.Authentication{Enabled: false}),
		config.WithLogFormat("console"),
		config.WithLogLevel("debug"),
	)
	root := &cobra.Command{
		Use:   "agent",
		Short: "Assisted Migration Agent",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if err := validateLoggingConfig(cfg); err != nil {
				return err
			}
			zap.ReplaceGlobals(logger.Init(cfg.LogFormat, cfg.LogLevel))
			return nil
		},
	}
	root.PersistentFlags().StringVar(&cfg.LogFormat, "log-format", cfg.LogFormat, "format of the logs: console or json")
	root.PersistentFlags().StringVar(&cfg.LogLevel, "log-level", cfg.LogLevel, "log level")
	root.AddCommand(NewRunCommand(cfg), NewVersionCommand(cfg))
	return root
}

func validateLoggingConfig(cfg *config.Configuration) error {
	if cfg.LogFormat != "console" && cfg.LogFormat != "json" {
		return fmt.Errorf("invalid log-format: %s", cfg.LogFormat)
	}
	if _, err := zapcore.ParseLevel(cfg.LogLevel); err != nil {
		return fmt.Errorf("invalid log level %s", cfg.LogLevel)
	}
	return nil
}
