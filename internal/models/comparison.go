package models

import "time"

// VMComparisonRow is a lightweight row returned by the store comparison query:
// one row per VM with its migratable status.
type VMComparisonRow struct {
	VMID       string
	Migratable bool
}

// CollectionMeta holds identifying metadata for one collection.
// Used to populate CollectionAggregate without requiring a live *store.Database.
type CollectionMeta struct {
	ID        string
	CreatedAt time.Time
}

// CollectionAggregate holds pre-computed counts for one collection.
type CollectionAggregate struct {
	ID            string
	CreatedAt     time.Time
	TotalVMs      int
	Migratable    int
	NonMigratable int
	Clusters      int
	Role          string // "baseline" or "comparison"
}

// ComparisonDiffEntry is the diff for one metric: the numeric delta (B-A)
// plus the symmetric set-difference counts.
type ComparisonDiffEntry struct {
	Delta   int
	OnlyInA int
	OnlyInB int
}

// ComparisonDiffPage is one paginated side of a drill-down result.
type ComparisonDiffPage struct {
	Total     int
	Page      int
	PageCount int
	VMIDs     []string
}

// ComparisonSummary is the full result returned by ComparisonService.Summary.
type ComparisonSummary struct {
	A             CollectionAggregate
	B             CollectionAggregate
	TotalVMs      ComparisonDiffEntry
	Migratable    ComparisonDiffEntry
	NonMigratable ComparisonDiffEntry
	Clusters      ComparisonDiffEntry
}

// ComparisonDiff is the result returned by ComparisonService.Diff.
type ComparisonDiff struct {
	Dimension ComparisonDimension
	OnlyInA   ComparisonDiffPage
	OnlyInB   ComparisonDiffPage
}

// ComparisonDimension identifies which metric to drill into.
type ComparisonDimension string

const (
	DimensionTotal         ComparisonDimension = "total"
	DimensionMigratable    ComparisonDimension = "migratable"
	DimensionNonMigratable ComparisonDimension = "non-migratable"
)

// IsValid reports whether d is a recognised dimension.
func (d ComparisonDimension) IsValid() bool {
	switch d {
	case DimensionTotal, DimensionMigratable, DimensionNonMigratable:
		return true
	}
	return false
}
