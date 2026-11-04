package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
)

func NewMigrateCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "migrate",
		Short: "Manage offline database migrations (agent must be stopped)",
		// Recovery has no application configuration or application logging. In
		// particular, the root logger must not write alongside JSON on stdout.
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
		SilenceUsage: true,
	}

	command.PersistentFlags().String("data-folder", "", "Existing persistent agent data directory")
	command.PersistentFlags().String("attempt-id", "", "Unique upgrade attempt identifier")
	command.AddCommand(
		NewPrepareCommand(),
		NewApplyCommand(),
		NewRollbackCommand(),
		NewVerifyCommand(),
	)

	return command
}

func NewPrepareCommand() *cobra.Command {
	request := migrations.Request{
		Operation: "prepare",
	}
	command := &cobra.Command{
		Use:   "prepare",
		Short: "prepare an offline upgrade migration attempt",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMigrationCommand(cmd, request)
		},
	}

	command.Flags().StringVar(&request.PreviousImage, "previous-image", "", "Previous immutable image ID")
	command.Flags().StringVar(&request.CandidateImage, "candidate-image", "", "Candidate immutable image ID")

	return command
}

func NewApplyCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "apply",
		Short: "apply an offline upgrade migration attempt",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			request := migrations.Request{
				Operation: "apply",
			}
			return runMigrationCommand(cmd, request)
		},
	}
}

func NewRollbackCommand() *cobra.Command {
	request := migrations.Request{
		Operation: "rollback",
	}
	command := &cobra.Command{
		Use:   "rollback",
		Short: "rollback an offline upgrade migration attempt",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMigrationCommand(cmd, request)
		},
	}

	command.Flags().BoolVar(&request.DryRun, "dry-run", false, "Read-only rollback plan; do not execute changes")

	return command
}

func NewVerifyCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "verify",
		Short: "verify an offline upgrade migration attempt",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			request := migrations.Request{
				Operation: "verify",
			}
			return runMigrationCommand(cmd, request)
		},
	}

	command.Flags().String("target", "baseline", "Verification target (baseline)")

	return command
}

func runMigrationCommand(cmd *cobra.Command, request migrations.Request) error {
	dataFolder, err := cmd.Flags().GetString("data-folder")
	if err != nil {
		return err
	}
	attemptID, err := cmd.Flags().GetString("attempt-id")
	if err != nil {
		return err
	}
	request.AttemptID = attemptID

	result := migrations.Result{
		AttemptID: attemptID,
		Operation: request.Operation,
		DryRun:    request.DryRun,
	}
	if request.Operation == "verify" {
		var target string
		target, err = cmd.Flags().GetString("target")
		if err == nil && target != "baseline" {
			err = fmt.Errorf("unsupported verification target: %s", target)
		}
	}
	if err == nil {
		var recovery *migrations.Recovery
		recovery, err = migrations.NewRecovery(dataFolder)
		if err == nil {
			result, err = recovery.Run(cmd.Context(), request)
		}
	}
	if err != nil {
		result.Error = err.Error()
	}
	outputErr := json.NewEncoder(cmd.OutOrStdout()).Encode(result)
	if outputErr != nil {
		return outputErr
	}

	return err
}
