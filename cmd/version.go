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
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("Agent Version:   %s\n", cfg.Agent.Version)
			fmt.Printf("Git Commit:      %s\n", cfg.Agent.GitCommit)
			fmt.Printf("UI Git Commit:   %s\n", cfg.Agent.UIGitCommit)
		},
	}
}
