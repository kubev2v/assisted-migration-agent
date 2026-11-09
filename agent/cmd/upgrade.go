package cmd

import (
	"context"
	"encoding/json"
	"time"

	"github.com/spf13/cobra"

	"github.com/kubev2v/assisted-migration-agent/internal/upgrade"
)

// NewUpgradeCommand validates saved data offline, without normal application bootstrap.
func NewUpgradeCommand() *cobra.Command {
	command := &cobra.Command{
		Use: "upgrade", Short: "Validate saved data before starting an upgraded agent",
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error { return nil },
		SilenceUsage:      true,
	}
	var folder, attemptID, imageID string
	timeout := 5 * time.Minute
	validate := &cobra.Command{
		Use: "validate", Args: cobra.NoArgs,
		Short:        "Read and validate existing data against the drained source baseline (agent must be stopped)",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
			defer cancel()
			err := upgrade.Validate(ctx, folder, attemptID, imageID)
			result := struct {
				ContractVersion int    `json:"contract_version"`
				AttemptID       string `json:"attempt_id"`
				ImageID         string `json:"image_id"`
				Valid           bool   `json:"valid"`
				Error           string `json:"error,omitempty"`
			}{ContractVersion: upgrade.ContractVersion, AttemptID: attemptID, ImageID: imageID, Valid: err == nil}
			if err != nil {
				result.Error = err.Error()
			}
			if outputErr := json.NewEncoder(cmd.OutOrStdout()).Encode(result); outputErr != nil {
				return outputErr
			}
			return err
		},
	}
	validate.Flags().StringVar(&folder, "data-folder", "", "Existing persistent agent data directory")
	validate.Flags().StringVar(&attemptID, "attempt-id", "", "Drained source's upgrade attempt")
	validate.Flags().StringVar(&imageID, "image-id", "", "Immutable ID of the image executing validation")
	validate.Flags().DurationVar(&timeout, "timeout", timeout, "Deadline for read-only saved-data validation")
	command.AddCommand(validate)
	return command
}
