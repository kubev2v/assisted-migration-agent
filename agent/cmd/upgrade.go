package cmd

import (
	"encoding/json"

	"github.com/spf13/cobra"

	"github.com/kubev2v/assisted-migration-agent/internal/upgrade"
)

// NewUpgradeCommand probes support without application configuration or side effects.
func NewUpgradeCommand() *cobra.Command {
	command := &cobra.Command{Use: "upgrade", Short: "Inspect host-coordinated upgrade support", PersistentPreRunE: func(_ *cobra.Command, _ []string) error { return nil }}
	command.AddCommand(&cobra.Command{Use: "capabilities", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
			ContractVersion int      `json:"contract_version"`
			Features        []string `json:"features"`
		}{ContractVersion: upgrade.ContractVersion, Features: []string{"offline-migrations", "maintenance", "probation", "validation", "data-lock", "host-authorization"}})
	}})
	return command
}
