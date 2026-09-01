package v2

import (
	"context"
	"fmt"

	"github.com/sirupsen/logrus"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
)

type BootRecoveryManager struct {
	store  *store.Store2
	logger *logrus.Logger
}

func NewBootRecoveryManager(
	store *store.Store2,
	logger *logrus.Logger,
) *BootRecoveryManager {
	return &BootRecoveryManager{
		store:  store,
		logger: logger,
	}
}

type orphanedTask struct {
	VMID string
	Mode string // 'standard' or 'v2v'
}

// inspectionRecoveryStore is the store surface boot recovery needs from either
// inspection track, so the cleanup loop can pick a store once instead of
// branching on task.Mode for every operation.
type inspectionRecoveryStore interface {
	PurgeConcerns(ctx context.Context, vmID string) error
	Update(ctx context.Context, vmID string, status models.InspectionStatus) error
}

// ReconcileOrphanedInspections is executed on agent boot to clean up crashed executions
func (b *BootRecoveryManager) ReconcileOrphanedInspections(ctx context.Context) error {
	b.logger.Info("Starting boot-time inspection recovery (standard + v2v)...")

	// 1. Query both standard and v2v tables for orphaned tasks
	query := `
		SELECT "VM ID", 'standard' as mode
		FROM vm_inspection_status
		WHERE status IN ('running', 'pending')
		UNION ALL
		SELECT "VM ID", 'v2v' as mode
		FROM vm_inspection_status_v2v
		WHERE status IN ('running', 'pending');
	`
	rows, err := b.store.Querier().QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to scan for orphaned inspections: %w", err)
	}
	defer rows.Close()

	var orphaned []orphanedTask
	for rows.Next() {
		var task orphanedTask
		if err := rows.Scan(&task.VMID, &task.Mode); err != nil {
			// Don't silently skip: a dropped row is an orphaned inspection left
			// stuck in running/pending while we'd otherwise report success.
			return fmt.Errorf("failed to scan orphaned inspection row: %w", err)
		}
		orphaned = append(orphaned, task)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterating orphaned inspection rows: %w", err)
	}

	if len(orphaned) == 0 {
		b.logger.Info("No orphaned inspection tasks found. System clean.")
		return nil
	}

	b.logger.Warnf("Identified %d orphaned inspection tasks. Cleaning up database state...", len(orphaned))
	b.logger.Warn("Note: orphaned vSphere snapshots (if any) require manual cleanup via vCenter")

	for _, task := range orphaned {
		b.logger.Infof("Cleaning up %s inspection for VM %s...", task.Mode, task.VMID)

		// Pick the store for this track once, then run the same cleanup on it.
		var st inspectionRecoveryStore = b.store.Inspection()
		if task.Mode == "v2v" {
			st = b.store.InspectionV2V()
		}

		// Purge concerns and update database state to error
		err = b.store.WithTx(ctx, func(txCtx context.Context) error {
			if err := st.PurgeConcerns(txCtx, task.VMID); err != nil {
				return fmt.Errorf("failed to purge %s concerns: %w", task.Mode, err)
			}

			// Write status transition indicating interruption
			status := models.InspectionStatus{
				State: models.InspectionStateError,
				Error: fmt.Errorf("inspection interrupted due to system reboot or agent container crash"),
			}

			if err := st.Update(txCtx, task.VMID, status); err != nil {
				return fmt.Errorf("failed to update %s status: %w", task.Mode, err)
			}

			return nil
		})

		if err != nil {
			b.logger.Errorf("Failed to update status for VM %s (%s) on recovery: %v", task.VMID, task.Mode, err)
		} else {
			b.logger.Infof("Successfully marked VM %s %s inspection as Failed/Interrupted", task.VMID, task.Mode)
		}
	}

	b.logger.Info("Boot-time inspection recovery completed successfully.")
	return nil
}
