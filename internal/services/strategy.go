package services

import (
	"context"
	"time"

	"github.com/vmware/govmomi/object"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
)

// BenchmarkStrategy defines how a single disk copy benchmark is executed.
type BenchmarkStrategy interface {
	// Name returns the method identifier recorded in BenchmarkRun.Method.
	Name() string

	// Setup is called once before benchmark iterations start for a pair.
	Setup(ctx context.Context, dc *object.Datacenter, pair models.DatastorePair) error

	// FillDisk fills the source disk with random data to defeat storage
	// zero-block optimization. Called once per pair before iterations start.
	// onProgress is called periodically with the number of bytes written so far.
	FillDisk(ctx context.Context, dc *object.Datacenter, pair models.DatastorePair,
		srcDiskPath string, diskSizeGB int, onProgress func(bytesWritten int64)) error

	// RunBenchmark copies a disk from srcDiskPath to dstDiskPath and returns
	// the wall-clock duration. The paths are datastore-relative (e.g. "dir/file.vmdk").
	RunBenchmark(ctx context.Context, dc *object.Datacenter, pair models.DatastorePair,
		srcDiskPath, dstDiskPath string, diskSizeGB int) (BenchmarkResult, error)

	// Teardown is called after all iterations complete for a pair.
	Teardown(ctx context.Context) error

	// SelectedHost returns the name of the ESXi host chosen during Setup.
	SelectedHost() string
}

// BenchmarkResult holds the outcome of a single benchmark copy.
type BenchmarkResult struct {
	Duration time.Duration
}
