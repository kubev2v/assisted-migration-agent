package services

import (
	"context"
	"fmt"

	"github.com/kubev2v/migration-planner/pkg/inventory"

	"github.com/kubev2v/migration-planner-common/estimations/complexity"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

type EstimationService struct {
	store            *store.Store
	inventoryBuilder InventoryBuilder
}

func NewEstimationService(st *store.Store, builder InventoryBuilder) *EstimationService {
	return &EstimationService{
		store:            st,
		inventoryBuilder: builder,
	}
}

func (s *EstimationService) CalculateMigrationComplexity(ctx context.Context, expression string) (*models.MigrationComplexityResult, error) {
	vms, _, err := NewVMService(s.store).List(ctx, VMListParams{Expression: expression})
	if err != nil {
		return nil, fmt.Errorf("listing VMs for complexity estimation: %w", err)
	}

	if len(vms) == 0 {
		return nil, srvErrors.NewValidationError(fmt.Sprintf("no vms found for the provided expression: %s", expression))
	}

	vmIDs := make([]string, 0, len(vms))
	for _, vm := range vms {
		vmIDs = append(vmIDs, vm.ID)
	}

	// TODO: We should add the inventory service the mechanism to build the inventories. both group and estimation have the parser to build it.
	// that's should be replace with inventory service
	inv, err := s.inventoryBuilder.BuildInventory(ctx, vmIDs)
	if err != nil {
		return nil, fmt.Errorf("building inventory for complexity estimation: %w", err)
	}

	return s.buildComplexityResult(&inv.VCenter.VMs)
}

// buildComplexityResult converts cluster inventory data into complexity breakdowns.
func (s *EstimationService) buildComplexityResult(vmData *inventory.VMsData) (*models.MigrationComplexityResult, error) {
	var osEntries []complexity.VMOsEntry
	var diskEntries []complexity.DiskTierInput

	if vmData.OSInfo == nil || vmData.Total <= 0 {
		return nil, fmt.Errorf("inventory has missing data")
	}

	diskSource := vmData.DiskComplexityTiers
	if len(diskSource) == 0 {
		diskSource = vmData.DiskSizeTiers
	}

	if len(diskSource) == 0 {
		return nil, srvErrors.NewValidationError("inventory has missing data")
	}

	osEntries = make([]complexity.VMOsEntry, 0, len(vmData.OSInfo))
	for name, info := range vmData.OSInfo {
		osEntries = append(osEntries, complexity.VMOsEntry{Name: name, Count: info.Count})
	}

	diskEntries = make([]complexity.DiskTierInput, 0, len(diskSource))
	for label, tier := range diskSource {
		diskEntries = append(diskEntries, complexity.DiskTierInput{
			Label:       label,
			VMCount:     tier.VMCount,
			TotalSizeTB: tier.TotalSizeTB,
		})
	}

	return &models.MigrationComplexityResult{
		ComplexityByDisk:   complexity.DiskBreakdown(diskEntries),
		ComplexityByOS:     complexity.OSBreakdown(osEntries),
		ComplexityByOSName: complexity.OSNameBreakdown(osEntries),
		DiskSizeRatings:    complexity.DiskSizeRangeRatings(),
		OSRatings:          complexity.OSRatings(osEntries),
	}, nil
}
