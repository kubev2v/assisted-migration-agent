package v2

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kubev2v/vm-migration-detective/pkg/vmdetect"
	"go.uber.org/zap"

	"github.com/kubev2v/assisted-migration-agent/internal/config"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/pkg/vmware"
	"github.com/kubev2v/assisted-migration-agent/pkg/work"
)

// inspectionBuilderFactory builds the per-VM WorkBuilder for one inspection track.
type inspectionBuilderFactory = func(vmID string) work.WorkBuilder2[models.InspectionStatus, models.InspectionResult]

// toInspectionConcerns maps detector concerns to the storage model. Track-neutral
// pure mapping shared by the standard and v2v builders.
func toInspectionConcerns(all []vmdetect.Concern) []models.VmInspectionConcern {
	if len(all) == 0 {
		return nil
	}
	concerns := make([]models.VmInspectionConcern, 0, len(all))
	for _, c := range all {
		concerns = append(concerns, models.VmInspectionConcern{
			Label:    c.Label,
			Category: string(c.Category),
			Msg:      c.Message,
		})
	}
	return concerns
}

// defaultStandardInspectionBuilderFactory builds standard inspection WorkUnits (virt-inspector only)
func defaultStandardInspectionBuilderFactory(
	store *store.Store2,
	operator vmware.VMOperator,
	detector *vmdetect.Detector,
	cfg *config.Agent,
) inspectionBuilderFactory {
	return func(vmID string) work.WorkBuilder2[models.InspectionStatus, models.InspectionResult] {
		log := zap.S().Named("standard_inspection_builder")

		units := []work.WorkUnit[models.InspectionStatus, models.InspectionResult]{
			// Unit 1: Validate Privileges on vCenter
			{
				// See the v2v builder for the full rationale: the pipeline calls EVERY
				// unit's Status() before the worker dispatches that unit, and the worker
				// is released between units, so writing "running" from Status() would show
				// not-yet-dispatched VMs (and units) as "running". Every unit persists its
				// status from Work() instead, so DB status only advances on real dispatch.
				Status: func() models.InspectionStatus {
					return models.InspectionStatus{State: models.InspectionStateRunning, Details: "validating privileges"}
				},
				Work: func(ctx context.Context, result models.InspectionResult) (models.InspectionResult, error) {
					status := models.InspectionStatus{State: models.InspectionStateRunning, Details: "validating privileges"}
					if err := store.Inspection().Update(context.Background(), vmID, status); err != nil {
						log.Errorw("failed to persist status", "vmId", vmID, "error", err)
					}
					log.Infow("validating VM privileges", "vmId", vmID)
					if err := operator.ValidatePrivileges(ctx, vmID, models.InspectorRequiredPrivileges); err != nil {
						log.Errorw("privilege validation failed", "vmId", vmID, "error", err)
						result.Err = err
						return result, err
					}
					log.Infow("privilege validation passed", "vmId", vmID)
					return result, nil
				},
			},
			// Unit 2: Create Snapshot (Referencing Shared Constant Name)
			{
				Status: func() models.InspectionStatus {
					return models.InspectionStatus{State: models.InspectionStateRunning, Details: "creating snapshot"}
				},
				Work: func(ctx context.Context, result models.InspectionResult) (models.InspectionResult, error) {
					status := models.InspectionStatus{State: models.InspectionStateRunning, Details: "creating snapshot"}
					if err := store.Inspection().Update(context.Background(), vmID, status); err != nil {
						log.Errorw("failed to persist status", "vmId", vmID, "error", err)
					}
					log.Infow("creating VM snapshot", "vmId", vmID)
					snapID, err := operator.CreateSnapshot(ctx, vmware.CreateSnapshotRequest{
						VmId:         vmID,
						SnapshotName: models.VirtInspectionSnapshotName,
					})
					if err != nil {
						log.Errorw("failed to create VM snapshot", "vmId", vmID, "error", err)
						result.Err = err
						return result, err
					}
					result.SnapshotId = snapID
					log.Infow("VM snapshot created", "vmId", vmID)
					return result, nil
				},
			},
			// Unit 3: Run Standard Inspection
			{
				Status: func() models.InspectionStatus {
					return models.InspectionStatus{State: models.InspectionStateRunning, Details: "running standard metadata inspection"}
				},
				Work: func(ctx context.Context, result models.InspectionResult) (models.InspectionResult, error) {
					status := models.InspectionStatus{State: models.InspectionStateRunning, Details: "running standard metadata inspection"}
					if err := store.Inspection().Update(context.Background(), vmID, status); err != nil {
						log.Errorw("failed to persist status", "vmId", vmID, "error", err)
					}
					log.Infow("running standard inspection", "vmId", vmID, "snapshotId", result.SnapshotId)
					detectResult, err := detector.Detect(vmdetect.DetectParams{
						Ctx:           ctx, // No timeout - same as pre-v7 behavior
						VMMoref:       vmID,
						SnapshotMoref: result.SnapshotId,
						RunVirtV2v:    false, // Standard inspection only
					})
					if err != nil {
						log.Errorw("standard inspection failed", "vmId", vmID, "snapshotId", result.SnapshotId, "error", err)
						result.Err = err
						return result, err
					}

					result.Concerns = toInspectionConcerns(detectResult.AllConcerns)

					log.Infow("standard inspection completed", "vmId", vmID, "concernCount", len(result.Concerns))
					return result, nil
				},
			},
			// Unit 4: Persist Results with safe transaction and stale concerns purge
			{
				Status: func() models.InspectionStatus {
					return models.InspectionStatus{State: models.InspectionStateRunning, Details: "persisting results"}
				},
				Work: func(ctx context.Context, result models.InspectionResult) (models.InspectionResult, error) {
					status := models.InspectionStatus{State: models.InspectionStateRunning, Details: "persisting results"}
					if err := store.Inspection().Update(context.Background(), vmID, status); err != nil {
						log.Errorw("failed to persist status", "vmId", vmID, "error", err)
					}
					log.Infow("persisting inspection results", "vmId", vmID, "concernCount", len(result.Concerns))
					err := store.WithTx(ctx, func(txCtx context.Context) error {
						// CRITICAL BUG FIX: Clear stale concerns first to prevent old failed runs from persisting
						if err := store.Inspection().PurgeConcerns(txCtx, vmID); err != nil {
							return fmt.Errorf("failed to clear stale concerns: %w", err)
						}

						// Insert newly identified concerns
						if len(result.Concerns) > 0 {
							if err := store.Inspection().InsertResult(txCtx, vmID, result.Concerns); err != nil {
								return fmt.Errorf("failed to insert fresh concerns: %w", err)
							}
						}

						return nil
					})
					if err != nil {
						log.Errorw("failed to persist inspection results", "vmId", vmID, "error", err)
						result.Err = err
						return result, err
					}
					log.Infow("inspection results persisted", "vmId", vmID)
					result.Completed = true
					return result, nil
				},
			},
		}

		// Cleanup handler that deletes snapshots on vCenter and updates status.
		// Both steps run on detached, bounded contexts so teardown still completes
		// (and the terminal status is persisted) even when the run context was
		// canceled or vCenter is degraded.
		finalize := func(_ context.Context, result models.InspectionResult) error {
			if result.SnapshotId != "" {
				log.Infow("removing VM snapshot", "vmId", vmID)
				req := vmware.RemoveSnapshotRequest{
					SnapshotId:  result.SnapshotId,
					Consolidate: true,
				}
				rmCtx, cancel := context.WithTimeout(context.Background(), finalizeSnapshotRemoveTimeout)
				if err := operator.RemoveSnapshot(rmCtx, req); err != nil {
					log.Errorw("failed to remove VM snapshot", "vmId", vmID, "error", err)
				}
				cancel()
			}

			status := models.TerminalStatus(result)

			upCtx, cancel := context.WithTimeout(context.Background(), finalizeStatusPersistTimeout)
			if err := store.Inspection().Update(upCtx, vmID, status); err != nil {
				log.Errorw("failed to persist terminal inspection status", "vmId", vmID, "state", status.State, "error", err)
			}
			cancel()

			return nil
		}

		return work.NewSliceWorkBuilder2(units, finalize)
	}
}

const (
	defaultV2VHealthCheckInterval = 30 * time.Second
	// defaultV2VHealthFailureThreshold is how many CONSECUTIVE liveness probe
	// failures must occur before the run is cancelled. vCenter frequently resets
	// idle connections with "connection reset by peer" while the session is still
	// alive, so a single failed probe must not kill a healthy (often self-retrying)
	// inspection. Only a sustained streak indicates a genuinely dead session.
	defaultV2VHealthFailureThreshold = 3
	// defaultV2VHealthProbeTimeout bounds each individual liveness probe. Without
	// it a black-holed connection (route dropped, firewalled) makes GetCurrentTime
	// block on the TCP round-trip indefinitely, parking the monitor loop inside the
	// probe so no failure is ever counted and the run hangs in "running" forever.
	// A hung probe past this deadline counts as a failure like any other.
	defaultV2VHealthProbeTimeout = 10 * time.Second

	// finalizeSnapshotRemoveTimeout bounds the vCenter snapshot removal during
	// teardown. Without it a degraded vCenter can block RemoveSnapshot on a TCP
	// round-trip for minutes; because pool Stop/Cancel wait for finalize, that
	// would serialize behind the pool and stall teardown.
	finalizeSnapshotRemoveTimeout = 60 * time.Second
	// finalizeStatusPersistTimeout bounds the terminal-status write. It runs on a
	// detached context so the terminal state is persisted even when the run's own
	// context was already canceled (user cancel or health guard).
	finalizeStatusPersistTimeout = 30 * time.Second
)

// errVCenterLivenessLost is the cancel cause the health guard attaches when it
// tears down a run because vCenter died. It lets the terminal-status logic
// distinguish a health-driven failure (→ error, with reason) from a genuine
// user-initiated cancel (→ canceled, no error).
var errVCenterLivenessLost = errors.New("vCenter liveness lost during v2v dry-run: session/connection died")

// monitorVCenterHealth periodically probes vCenter liveness while a v2v dry-run
// executes. It cancels the inspection context (killing the stuck
// virt-v2v-inspector process) only after defaultV2VHealthFailureThreshold
// consecutive probe failures; any success resets the streak. Returns when the
// context is done.
func monitorVCenterHealth(
	ctx context.Context,
	cancel context.CancelCauseFunc,
	operator vmware.VMOperator,
	cfg *config.Agent,
	vmID string,
	log *zap.SugaredLogger,
) {
	interval := defaultV2VHealthCheckInterval
	if cfg != nil && cfg.V2VHealthCheckInterval > 0 {
		interval = cfg.V2VHealthCheckInterval
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	consecutiveFailures := 0

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Bound each probe: a black-holed connection would otherwise block
			// GetCurrentTime indefinitely, parking this loop and defeating the guard.
			probeCtx, probeCancel := context.WithTimeout(ctx, defaultV2VHealthProbeTimeout)
			err := operator.Ping(probeCtx)
			probeCancel()
			if err == nil {
				consecutiveFailures = 0
				continue
			}

			// Cancellation of the parent context (the run itself ended) trips Ping;
			// only act on real failures. A probe-timeout leaves parent ctx.Err() nil,
			// so a hung vCenter still counts as a failure below.
			if ctx.Err() != nil {
				return
			}

			consecutiveFailures++
			log.Warnw("vCenter liveness probe failed during v2v dry-run",
				"vmId", vmID, "consecutiveFailures", consecutiveFailures,
				"threshold", defaultV2VHealthFailureThreshold, "error", err)

			if consecutiveFailures >= defaultV2VHealthFailureThreshold {
				log.Errorw("vCenter liveness lost during v2v dry-run; cancelling",
					"vmId", vmID, "consecutiveFailures", consecutiveFailures)
				cancel(fmt.Errorf("%w (last probe: %v)", errVCenterLivenessLost, err))
				return
			}
		}
	}
}

// runVCenterGuarded runs fn under the vCenter liveness guard: a cancelable
// context watched by monitorVCenterHealth. If the guard tears the context down
// because vCenter died, the returned error is promoted to the liveness cause so
// TerminalStatus classifies it as an error (with reason) rather than a bare
// cancel. Used for the long / hang-prone vCenter operations (snapshot create and
// the dry-run) where a black-holed connection would otherwise wedge the run in
// "running" forever with no automatic teardown.
func runVCenterGuarded(
	ctx context.Context,
	operator vmware.VMOperator,
	cfg *config.Agent,
	vmID string,
	log *zap.SugaredLogger,
	fn func(ctx context.Context) error,
) error {
	guardCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	go monitorVCenterHealth(guardCtx, cancel, operator, cfg, vmID, log)

	err := fn(guardCtx)
	if err != nil {
		if cause := context.Cause(guardCtx); errors.Is(cause, errVCenterLivenessLost) {
			return cause
		}
	}
	return err
}

// defaultV2VInspectionBuilderFactory builds v2v inspection WorkUnits (virt-v2v-inspector)
func defaultV2VInspectionBuilderFactory(
	store *store.Store2,
	operator vmware.VMOperator,
	detector *vmdetect.Detector,
	cfg *config.Agent,
) inspectionBuilderFactory {
	return func(vmID string) work.WorkBuilder2[models.InspectionStatus, models.InspectionResult] {
		log := zap.S().Named("v2v_inspection_builder")

		units := []work.WorkUnit[models.InspectionStatus, models.InspectionResult]{
			// Unit 1: Validate Privileges on vCenter
			{
				// The pipeline calls every unit's Status() BEFORE it submits that unit's
				// Work to the shared scheduler (pipeline2.go), and the single pool worker
				// is RELEASED between units. So a parked VM can advance through the early
				// units in the gaps while another VM holds the worker for the long dry-run,
				// reach a later unit, and have that unit's Status() flip it to "running <that
				// stage>" in the DB while it is really still waiting for the worker slot.
				// To keep DB status truthful, EVERY unit (not just this one) persists its
				// "running" status from Work() — which only runs once the worker actually
				// dispatches it — and Status() stays a pure in-memory progress read.
				Status: func() models.InspectionStatus {
					return models.InspectionStatus{State: models.InspectionStateRunning, Details: "validating privileges"}
				},
				Work: func(ctx context.Context, result models.InspectionResult) (models.InspectionResult, error) {
					status := models.InspectionStatus{State: models.InspectionStateRunning, Details: "validating privileges"}
					if err := store.InspectionV2V().Update(context.Background(), vmID, status); err != nil {
						log.Errorw("failed to persist status", "vmId", vmID, "error", err)
					}
					log.Infow("validating VM privileges", "vmId", vmID)
					if err := operator.ValidatePrivileges(ctx, vmID, models.InspectorRequiredPrivileges); err != nil {
						log.Errorw("privilege validation failed", "vmId", vmID, "error", err)
						result.Err = err
						return result, err
					}
					log.Infow("privilege validation passed", "vmId", vmID)
					return result, nil
				},
			},
			// Unit 2: Create V2V Snapshot
			{
				Status: func() models.InspectionStatus {
					return models.InspectionStatus{State: models.InspectionStateRunning, Details: "creating v2v snapshot"}
				},
				Work: func(ctx context.Context, result models.InspectionResult) (models.InspectionResult, error) {
					status := models.InspectionStatus{State: models.InspectionStateRunning, Details: "creating v2v snapshot"}
					if err := store.InspectionV2V().Update(context.Background(), vmID, status); err != nil {
						log.Errorw("failed to persist status", "vmId", vmID, "error", err)
					}
					log.Infow("creating VM snapshot for v2v", "vmId", vmID)
					// Guarded: a hung/black-holed vCenter during snapshot create
					// would otherwise block indefinitely with the run stuck "running".
					var snapID string
					err := runVCenterGuarded(ctx, operator, cfg, vmID, log, func(gctx context.Context) error {
						var e error
						snapID, e = operator.CreateSnapshot(gctx, vmware.CreateSnapshotRequest{
							VmId:         vmID,
							SnapshotName: models.V2VInspectionSnapshotName,
						})
						return e
					})
					if err != nil {
						log.Errorw("failed to create VM snapshot", "vmId", vmID, "error", err)
						result.Err = err
						return result, err
					}
					result.SnapshotId = snapID
					log.Infow("VM snapshot created for v2v", "vmId", vmID)
					return result, nil
				},
			},
			// Unit 3: Run V2V Inspection with Dynamic Timeout
			{
				Status: func() models.InspectionStatus {
					return models.InspectionStatus{State: models.InspectionStateRunning, Details: "running V2V translation dry-run"}
				},
				Work: func(ctx context.Context, result models.InspectionResult) (models.InspectionResult, error) {
					status := models.InspectionStatus{State: models.InspectionStateRunning, Details: "running V2V translation dry-run"}
					if err := store.InspectionV2V().Update(context.Background(), vmID, status); err != nil {
						log.Errorw("failed to persist status", "vmId", vmID, "error", err)
					}
					// No hard wall-clock timeout: v2v dry-runs vary wildly with disk
					// size, SAN congestion and LUKS checks, so a fixed deadline causes
					// false-positive failures on healthy-but-slow workloads. Instead we
					// run under a cancelable context guarded by a vCenter liveness ticker
					// that cancels only when the session/connection actually dies. If the
					// guard fires, runVCenterGuarded promotes the error to the liveness
					// cause so it is classified as error (with reason), not bare cancel.
					log.Infow("running v2v inspection", "vmId", vmID, "snapshotId", result.SnapshotId)
					var detectResult *vmdetect.DetectResult
					err := runVCenterGuarded(ctx, operator, cfg, vmID, log, func(gctx context.Context) error {
						var e error
						detectResult, e = detector.Detect(vmdetect.DetectParams{
							Ctx:           gctx,
							VMMoref:       vmID,
							SnapshotMoref: result.SnapshotId,
							RunVirtV2v:    true, // V2V inspection
						})
						return e
					})
					if err != nil {
						log.Errorw("v2v inspection failed", "vmId", vmID, "snapshotId", result.SnapshotId, "error", err)
						result.Err = err
						return result, err
					}

					result.Concerns = toInspectionConcerns(detectResult.AllConcerns)

					log.Infow("v2v inspection completed", "vmId", vmID, "concernCount", len(result.Concerns))
					return result, nil
				},
			},
			// Unit 4: Persist V2V Results
			{
				Status: func() models.InspectionStatus {
					return models.InspectionStatus{State: models.InspectionStateRunning, Details: "persisting v2v results"}
				},
				Work: func(ctx context.Context, result models.InspectionResult) (models.InspectionResult, error) {
					status := models.InspectionStatus{State: models.InspectionStateRunning, Details: "persisting v2v results"}
					if err := store.InspectionV2V().Update(context.Background(), vmID, status); err != nil {
						log.Errorw("failed to persist status", "vmId", vmID, "error", err)
					}
					log.Infow("persisting v2v inspection results", "vmId", vmID, "concernCount", len(result.Concerns))
					err := store.WithTx(ctx, func(txCtx context.Context) error {
						// Clear stale v2v concerns
						if err := store.InspectionV2V().PurgeConcerns(txCtx, vmID); err != nil {
							return fmt.Errorf("failed to clear stale v2v concerns: %w", err)
						}

						// Insert new v2v concerns
						if len(result.Concerns) > 0 {
							if err := store.InspectionV2V().InsertResult(txCtx, vmID, result.Concerns); err != nil {
								return fmt.Errorf("failed to insert v2v concerns: %w", err)
							}
						}

						return nil
					})
					if err != nil {
						log.Errorw("failed to persist v2v inspection results", "vmId", vmID, "error", err)
						result.Err = err
						return result, err
					}
					log.Infow("v2v inspection results persisted", "vmId", vmID)
					result.Completed = true
					return result, nil
				},
			},
		}

		// Cleanup handler for v2v. Detached, bounded contexts so a user cancel /
		// health-guard teardown or a degraded vCenter cannot block teardown or
		// prevent the terminal status from being persisted.
		finalize := func(_ context.Context, result models.InspectionResult) error {
			if result.SnapshotId != "" {
				log.Infow("removing v2v VM snapshot", "vmId", vmID)
				req := vmware.RemoveSnapshotRequest{
					SnapshotId:  result.SnapshotId,
					Consolidate: true,
				}
				rmCtx, cancel := context.WithTimeout(context.Background(), finalizeSnapshotRemoveTimeout)
				if err := operator.RemoveSnapshot(rmCtx, req); err != nil {
					log.Errorw("failed to remove v2v VM snapshot", "vmId", vmID, "error", err)
				}
				cancel()
			}

			status := models.TerminalStatus(result)

			upCtx, cancel := context.WithTimeout(context.Background(), finalizeStatusPersistTimeout)
			if err := store.InspectionV2V().Update(upCtx, vmID, status); err != nil {
				log.Errorw("failed to persist terminal v2v inspection status", "vmId", vmID, "state", status.State, "error", err)
			}
			cancel()

			return nil
		}

		return work.NewSliceWorkBuilder2(units, finalize)
	}
}
