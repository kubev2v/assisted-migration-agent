package models

import "github.com/kubev2v/migration-planner-common/estimations/complexity"

type MigrationComplexityResult struct {
	ComplexityByDisk   []complexity.DiskComplexityEntry
	ComplexityByOS     []complexity.OSDifficultyEntry
	ComplexityByOSName []complexity.OSNameEntry
	DiskSizeRatings    map[string]complexity.Score
	OSRatings          map[string]complexity.Score
}
