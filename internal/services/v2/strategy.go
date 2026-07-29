package v2

import (
	"context"
	"time"

	"github.com/vmware/govmomi/object"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
)

type BenchmarkStrategy interface {
	Name() string
	Setup(ctx context.Context, dc *object.Datacenter, pair models.DatastorePair) error
	FillDisk(ctx context.Context, dc *object.Datacenter, pair models.DatastorePair,
		srcDiskPath string, diskSizeGB int, onProgress func(bytesWritten int64)) error
	RunBenchmark(ctx context.Context, dc *object.Datacenter, pair models.DatastorePair,
		srcDiskPath, dstDiskPath string, diskSizeGB int) (BenchmarkResult, error)
	Teardown(ctx context.Context) error
	SelectedHost() string
}

type BenchmarkResult struct {
	Duration time.Duration
}
