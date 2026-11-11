package services

import (
	"context"
	"fmt"
	"maps"
	"strconv"

	"github.com/kubev2v/migration-planner-common/estimations/complexity"
	"github.com/kubev2v/migration-planner-common/estimations/engines"
	"github.com/kubev2v/migration-planner-common/estimations/estimation"
	"github.com/kubev2v/migration-planner-common/estimations/estimation/calculators"
	"github.com/kubev2v/migration-planner/pkg/inventory"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

type EstimationService struct {
	store            *store.Store
	inventoryBuilder InventoryBuilder
}

type estimationRun struct {
	engines map[engines.Schema]*estimation.Engine
	context models.EstimationContext
}

func NewEstimationService(st *store.Store, builder InventoryBuilder) *EstimationService {
	return &EstimationService{
		store:            st,
		inventoryBuilder: builder,
	}
}

func (s *EstimationService) CalculateMigrationComplexity(ctx context.Context, expression string) (*models.MigrationComplexityResult, error) {
	vmData, err := s.estimationInventory(ctx, expression)
	if err != nil {
		return nil, err
	}

	return s.buildComplexityResult(vmData)
}

func (s *EstimationService) CalculateMigrationEstimation(ctx context.Context, params models.MigrationEstimationRequestParams) (*models.MigrationEstimationResult, error) {
	run, err := newEstimationRun(params)
	if err != nil {
		return nil, err
	}

	vmData, err := s.estimationInventory(ctx, params.Expression)
	if err != nil {
		return nil, err
	}

	results := run.calculate(vmData.Total, float64(vmData.DiskGB.Total))

	return &models.MigrationEstimationResult{
		Estimation:        results,
		EstimationContext: run.context,
	}, nil
}

func (s *EstimationService) CalculateMigrationEstimationByComplexity(ctx context.Context, params models.MigrationEstimationRequestParams) (*models.MigrationEstimationByComplexityResult, error) {
	run, err := newEstimationRun(params)
	if err != nil {
		return nil, err
	}

	vmData, err := s.estimationInventory(ctx, params.Expression)
	if err != nil {
		return nil, err
	}

	result := &models.MigrationEstimationByComplexityResult{
		ComplexityByOSDisk: make([]models.OSDiskEstimationEntry, 0, len(complexity.OSScores)),
		ComplexityMatrix:   make(models.ComplexityMatrix, len(complexity.ComplexityMatrix)),
		EstimationContext: models.EstimationContext{
			Schemas:    run.context.Schemas,
			Parameters: run.context.Parameters,
		},
	}

	for _, score := range complexity.OSScores {
		bucket := vmData.ComplexityDistribution[strconv.Itoa(score)]
		entry := models.OSDiskEstimationEntry{
			Score:       score,
			VMCount:     bucket.VMCount,
			TotalSizeTB: bucket.TotalSizeTB,
		}

		if bucket.VMCount != 0 {
			// The parser reports TiB; calculators use GiB, as does DiskGB.Total.
			entry.Estimation = run.calculate(bucket.VMCount, bucket.TotalSizeTB*1024)
		}

		result.ComplexityByOSDisk = append(result.ComplexityByOSDisk, entry)
	}

	for osScore, diskScores := range complexity.ComplexityMatrix {
		result.ComplexityMatrix[osScore] = maps.Clone(diskScores)
	}

	return result, nil
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

func newEstimationRun(params models.MigrationEstimationRequestParams) (*estimationRun, error) {
	schemas := make([]engines.Schema, len(params.Schemas))
	for i, schema := range params.Schemas {
		schemas[i] = engines.Schema(schema)
	}

	engineMap, err := engines.BuildEngines(schemas)
	if err != nil {
		return nil, srvErrors.NewValidationError(err.Error())
	}

	err = engines.ValidateParams(params.Parameters)
	if err != nil {
		return nil, srvErrors.NewValidationError(err.Error())
	}

	resolvedSchemas := make([]string, 0, len(engineMap))
	for schema := range engineMap {
		resolvedSchemas = append(resolvedSchemas, string(schema))
	}

	return &estimationRun{
		engines: engineMap,
		context: models.EstimationContext{Schemas: resolvedSchemas, Parameters: engines.BuildBaseParams(params.Parameters...)},
	}, nil
}

func (s *EstimationService) estimationInventory(ctx context.Context, expression string) (*inventory.VMsData, error) {
	vms, _, err := NewVMService(s.store).List(ctx, VMListParams{Expression: expression})
	if err != nil {
		return nil, fmt.Errorf("listing VMs for estimation: %w", err)
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
		return nil, fmt.Errorf("building inventory for estimation: %w", err)
	}

	if inv.VCenter == nil {
		return nil, fmt.Errorf("inventory has no vcenter-level data")
	}

	return &inv.VCenter.VMs, nil
}

func (r *estimationRun) calculate(vmCount int, totalDiskGB float64) map[string]estimation.Result {
	params := make([]estimation.Param, 0, len(r.context.Parameters)+2)
	params = append(params, r.context.Parameters...)
	params = append(params,
		estimation.Param{Key: calculators.ParamVMCount, Value: vmCount},
		estimation.Param{Key: calculators.ParamTotalDiskGB, Value: totalDiskGB},
	)

	results := make(map[string]estimation.Result, len(r.engines))
	for _, schema := range r.context.Schemas {
		results[schema] = r.engines[engines.Schema(schema)].Estimate(params)
	}

	return results
}
