package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/kubev2v/assisted-migration-agent/internal/config"
)

func NewVersionCommand(cfg *config.Configuration) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "Agent Version:   %s\nGit Commit:      %s\nUI Git Commit:   %s\n", cfg.Agent.Version, cfg.Agent.GitCommit, cfg.Agent.UIGitCommit)
			return err
		},
	}
}
