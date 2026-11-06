package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
)

func NewMigrateCommand() *cobra.Command {
	var dataFolder, attemptID string
	command := &cobra.Command{
		Use: "migrate", Short: "Manage offline database migrations (agent must be stopped)",
		// Recovery has no application configuration or application logging. In
		// particular, the root logger must not write alongside JSON on stdout.
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error { return nil },
		SilenceUsage:      true,
	}
	command.PersistentFlags().StringVar(&dataFolder, "data-folder", "", "Existing persistent agent data directory")
	command.PersistentFlags().StringVar(&attemptID, "attempt-id", "", "Unique upgrade attempt identifier")
	for _, operation := range []string{"prepare", "apply", "rollback", "verify"} {
		request := migrations.Request{Operation: operation}
		var target string
		subcommand := &cobra.Command{
			Use: operation, Short: fmt.Sprintf("%s an offline upgrade migration attempt", operation),
			Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				request.AttemptID = attemptID
				result := migrations.Result{AttemptID: attemptID, Operation: request.Operation, DryRun: request.DryRun}
				recovery, err := migrations.NewRecovery(dataFolder)
				if err == nil {
					result, err = recovery.Run(cmd.Context(), request)
				}
				if err != nil {
					result.Error = err.Error()
				}
				if outputErr := json.NewEncoder(cmd.OutOrStdout()).Encode(result); outputErr != nil {
					return outputErr
				}
				return err
			},
		}
		switch operation {
		case "prepare":
			subcommand.Flags().StringVar(&request.PreviousImage, "previous-image", "", "Previous immutable image ID")
			subcommand.Flags().StringVar(&request.CandidateImage, "candidate-image", "", "Candidate immutable image ID")
		case "rollback":
			subcommand.Flags().BoolVar(&request.DryRun, "dry-run", false, "Read-only rollback plan; do not execute changes")
		case "verify":
			subcommand.Flags().StringVar(&target, "target", "baseline", "Verification target (baseline)")
			subcommand.PreRunE = func(cmd *cobra.Command, args []string) error {
				if target != "baseline" {
					return fmt.Errorf("unsupported verification target: %s", target)
				}
				return nil
			}
		}
		command.AddCommand(subcommand)
	}
	return command
}
