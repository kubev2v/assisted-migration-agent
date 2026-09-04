package services

import (
	"context"
	"fmt"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
)

// ComparisonService computes diffs between two collections.
// Each collection is queried independently via its own Store connection.
type ComparisonService struct {
	storeA *store.Store
	storeB *store.Store
	metaA  models.CollectionMeta
	metaB  models.CollectionMeta
}

func NewComparisonService(storeA, storeB *store.Store, metaA, metaB models.CollectionMeta) *ComparisonService {
	return &ComparisonService{storeA: storeA, storeB: storeB, metaA: metaA, metaB: metaB}
}

// Summary loads VM rows from both collections, computes all set differences in a
// single pass, and returns aggregates + diff counts for every dimension.
func (s *ComparisonService) Summary(ctx context.Context) (models.ComparisonSummary, error) {
	aRows, bRows, err := s.loadBothCollections(ctx)
	if err != nil {
		return models.ComparisonSummary{}, err
	}

	aClusters, err := s.storeA.VM().CountDistinctClusters(ctx)
	if err != nil {
		return models.ComparisonSummary{}, fmt.Errorf("counting clusters in collection A: %w", err)
	}
	bClusters, err := s.storeB.VM().CountDistinctClusters(ctx)
	if err != nil {
		return models.ComparisonSummary{}, fmt.Errorf("counting clusters in collection B: %w", err)
	}

	sets := computeDiffSets(aRows, bRows)

	aAgg := models.CollectionAggregate{
		ID:            s.metaA.ID,
		CreatedAt:     s.metaA.CreatedAt,
		TotalVMs:      len(aRows),
		Migratable:    countWhere(aRows, true),
		NonMigratable: countWhere(aRows, false),
		Clusters:      aClusters,
	}
	bAgg := models.CollectionAggregate{
		ID:            s.metaB.ID,
		CreatedAt:     s.metaB.CreatedAt,
		TotalVMs:      len(bRows),
		Migratable:    countWhere(bRows, true),
		NonMigratable: countWhere(bRows, false),
		Clusters:      bClusters,
	}

	return models.ComparisonSummary{
		A: aAgg,
		B: bAgg,
		TotalVMs: models.ComparisonDiffEntry{
			Delta:   bAgg.TotalVMs - aAgg.TotalVMs,
			OnlyInA: len(sets.TotalOnlyInA),
			OnlyInB: len(sets.TotalOnlyInB),
		},
		Migratable: models.ComparisonDiffEntry{
			Delta:   bAgg.Migratable - aAgg.Migratable,
			OnlyInA: len(sets.MigratableOnlyInA),
			OnlyInB: len(sets.MigratableOnlyInB),
		},
		NonMigratable: models.ComparisonDiffEntry{
			Delta:   bAgg.NonMigratable - aAgg.NonMigratable,
			OnlyInA: len(sets.NonMigratableOnlyInA),
			OnlyInB: len(sets.NonMigratableOnlyInB),
		},
		Clusters: models.ComparisonDiffEntry{
			Delta: bClusters - aClusters,
		},
	}, nil
}

// Diff returns paginated VM IDs for one dimension (onlyInA and onlyInB).
// Both sides use the same page/pageSize but are paginated independently.
func (s *ComparisonService) Diff(ctx context.Context, dimension models.ComparisonDimension, page, pageSize int) (models.ComparisonDiff, error) {
	aRows, bRows, err := s.loadBothCollections(ctx)
	if err != nil {
		return models.ComparisonDiff{}, err
	}

	sets := computeDiffSets(aRows, bRows)

	var onlyInA, onlyInB []string
	switch dimension {
	case models.DimensionTotal:
		onlyInA, onlyInB = sets.TotalOnlyInA, sets.TotalOnlyInB
	case models.DimensionMigratable:
		onlyInA, onlyInB = sets.MigratableOnlyInA, sets.MigratableOnlyInB
	case models.DimensionNonMigratable:
		onlyInA, onlyInB = sets.NonMigratableOnlyInA, sets.NonMigratableOnlyInB
	default:
		return models.ComparisonDiff{}, fmt.Errorf("comparison: unknown dimension %q", dimension)
	}

	return models.ComparisonDiff{
		Dimension: dimension,
		OnlyInA:   paginateIDs(onlyInA, page, pageSize),
		OnlyInB:   paginateIDs(onlyInB, page, pageSize),
	}, nil
}

// ── Private helpers ────────────────────────────────────────────────────────

func (s *ComparisonService) loadBothCollections(ctx context.Context) (aRows, bRows []models.VMComparisonRow, err error) {
	aRows, err = s.storeA.VM().ListForComparison(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("loading collection A VMs: %w", err)
	}
	bRows, err = s.storeB.VM().ListForComparison(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("loading collection B VMs: %w", err)
	}
	return aRows, bRows, nil
}

type diffSets struct {
	TotalOnlyInA         []string
	TotalOnlyInB         []string
	MigratableOnlyInA    []string
	MigratableOnlyInB    []string
	NonMigratableOnlyInA []string
	NonMigratableOnlyInB []string
}

// computeDiffSets builds all six ID lists in a single O(n+m) pass.
// "onlyInA for dimension D" means: VM satisfies D in A and does NOT satisfy D in B.
// Not satisfying D in B includes the case where the VM is absent from B entirely.
func computeDiffSets(aRows, bRows []models.VMComparisonRow) diffSets {
	bMap := make(map[string]bool, len(bRows))
	for _, r := range bRows {
		bMap[r.VMID] = r.Migratable
	}
	aMap := make(map[string]bool, len(aRows))
	for _, r := range aRows {
		aMap[r.VMID] = r.Migratable
	}

	var sets diffSets

	for _, r := range aRows {
		bMigratable, inB := bMap[r.VMID]
		if !inB {
			sets.TotalOnlyInA = append(sets.TotalOnlyInA, r.VMID)
			if r.Migratable {
				sets.MigratableOnlyInA = append(sets.MigratableOnlyInA, r.VMID)
			} else {
				sets.NonMigratableOnlyInA = append(sets.NonMigratableOnlyInA, r.VMID)
			}
			continue
		}
		// VM exists in both — a status flip is symmetric: record both perspectives here.
		if r.Migratable && !bMigratable {
			sets.MigratableOnlyInA = append(sets.MigratableOnlyInA, r.VMID)
			sets.NonMigratableOnlyInB = append(sets.NonMigratableOnlyInB, r.VMID)
		} else if !r.Migratable && bMigratable {
			sets.NonMigratableOnlyInA = append(sets.NonMigratableOnlyInA, r.VMID)
			sets.MigratableOnlyInB = append(sets.MigratableOnlyInB, r.VMID)
		}
	}

	// Only VMs absent from A remain; shared VMs were fully handled in the A loop above.
	for _, r := range bRows {
		if _, inA := aMap[r.VMID]; inA {
			continue
		}
		sets.TotalOnlyInB = append(sets.TotalOnlyInB, r.VMID)
		if r.Migratable {
			sets.MigratableOnlyInB = append(sets.MigratableOnlyInB, r.VMID)
		} else {
			sets.NonMigratableOnlyInB = append(sets.NonMigratableOnlyInB, r.VMID)
		}
	}

	return sets
}

func countWhere(rows []models.VMComparisonRow, migratable bool) int {
	n := 0
	for _, r := range rows {
		if r.Migratable == migratable {
			n++
		}
	}
	return n
}

func paginateIDs(ids []string, page, pageSize int) models.ComparisonDiffPage {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 1
	}
	total := len(ids)
	if total == 0 {
		// ensure VMIDs serialises as [] not null in JSON
		return models.ComparisonDiffPage{Total: 0, Page: page, PageCount: 0, VMIDs: []string{}}
	}
	pageCount := (total + pageSize - 1) / pageSize
	if page > pageCount {
		return models.ComparisonDiffPage{Total: total, Page: page, PageCount: pageCount, VMIDs: []string{}}
	}
	start := (page - 1) * pageSize
	end := min(start+pageSize, total)
	return models.ComparisonDiffPage{Total: total, Page: page, PageCount: pageCount, VMIDs: ids[start:end]}
}
