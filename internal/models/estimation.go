package models

import (
	"github.com/kubev2v/migration-planner-common/estimations/complexity"
	"github.com/kubev2v/migration-planner-common/estimations/estimation"
)

type MigrationComplexityResult struct {
	ComplexityByDisk   []complexity.DiskComplexityEntry
	ComplexityByOS     []complexity.OSDifficultyEntry
	ComplexityByOSName []complexity.OSNameEntry
	DiskSizeRatings    map[string]complexity.Score
	OSRatings          map[string]complexity.Score
}

type MigrationEstimationRequestParams struct {
	Expression string
	Schemas    []string
	Parameters []estimation.Param
}

type EstimationContext struct {
	Schemas    []string
	Parameters []estimation.Param
}

type MigrationEstimationResult struct {
	Estimation        map[string]estimation.Result
	EstimationContext EstimationContext
}

type ComplexityMatrix map[int]map[int]int

type OSDiskEstimationEntry struct {
	Score       int
	VMCount     int
	TotalSizeTB float64
	Estimation  map[string]estimation.Result
}

type MigrationEstimationByComplexityResult struct {
	ComplexityByOSDisk []OSDiskEstimationEntry
	ComplexityMatrix   ComplexityMatrix
	EstimationContext  EstimationContext
}
