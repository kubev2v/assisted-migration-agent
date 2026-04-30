//go:build integration

package vmware_test

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/kubev2v/assisted-migration-agent/pkg/vmware"
)

// TestCopyVirtualDisk is a POC integration test that validates the
// CopyVirtualDisk approach for benchmarking datastore throughput.
//
// Required env vars:
//
//	VCENTER_URL       - vCenter URL (e.g., https://vcenter.example.com/sdk)
//	VCENTER_USER      - vCenter username
//	VCENTER_PASSWORD  - vCenter password
//	SOURCE_DATASTORE  - source datastore name
//	TARGET_DATASTORE  - target datastore name
//	DATACENTER        - datacenter name (optional, uses default if empty)
func TestCopyVirtualDisk(t *testing.T) {
	vcURL := os.Getenv("VCENTER_URL")
	vcUser := os.Getenv("VCENTER_USER")
	vcPass := os.Getenv("VCENTER_PASSWORD")
	srcDS := os.Getenv("SOURCE_DATASTORE")
	tgtDS := os.Getenv("TARGET_DATASTORE")
	dcName := os.Getenv("DATACENTER")

	if vcURL == "" || vcUser == "" || vcPass == "" || srcDS == "" || tgtDS == "" {
		t.Skip("skipping: set VCENTER_URL, VCENTER_USER, VCENTER_PASSWORD, SOURCE_DATASTORE, TARGET_DATASTORE")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	// Connect to vSphere
	gc, err := vmware.NewVsphereClient(ctx, vcURL, vcUser, vcPass, true)
	if err != nil {
		t.Fatalf("failed to connect to vSphere: %v", err)
	}
	defer gc.Logout(ctx)

	dm := vmware.NewDiskManager(gc)

	// Find datacenter
	dc, err := dm.FindDatacenter(ctx, dcName)
	if err != nil {
		t.Fatalf("failed to find datacenter: %v", err)
	}
	t.Logf("using datacenter: %s", dc.Name())

	// Verify both datastores exist
	if err := dm.DatastoreExists(ctx, dc, srcDS); err != nil {
		t.Fatalf("source datastore check failed: %v", err)
	}
	if err := dm.DatastoreExists(ctx, dc, tgtDS); err != nil {
		t.Fatalf("target datastore check failed: %v", err)
	}

	diskSizeGB := 1
	if v := os.Getenv("DISK_SIZE_GB"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			diskSizeGB = n
		}
	}
	tempDir := fmt.Sprintf("forecaster-poc-%d", time.Now().UnixNano())
	diskName := "benchmark-disk.vmdk"
	cloneName := "benchmark-disk-clone.vmdk"

	// Cleanup on exit
	defer func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cleanCancel()

		t.Log("cleaning up temporary files...")
		if err := dm.DeleteDirectory(cleanCtx, dc, srcDS, tempDir); err != nil {
			t.Logf("warning: failed to clean source dir: %v", err)
		}
		if srcDS != tgtDS {
			if err := dm.DeleteDirectory(cleanCtx, dc, tgtDS, tempDir); err != nil {
				t.Logf("warning: failed to clean target dir: %v", err)
			}
		}
	}()

	// Step 1: Create temp directory on source
	t.Logf("creating directory on source datastore %q", srcDS)
	if err := dm.CreateDirectory(ctx, dc, srcDS, tempDir); err != nil {
		t.Fatalf("failed to create source directory: %v", err)
	}

	// Create target directory if different datastore
	if srcDS != tgtDS {
		t.Logf("creating directory on target datastore %q", tgtDS)
		if err := dm.CreateDirectory(ctx, dc, tgtDS, tempDir); err != nil {
			t.Fatalf("failed to create target directory: %v", err)
		}
	}

	// Step 2: Create eager-zeroed thick disk
	t.Logf("creating %dGB eager-zeroed thick VMDK on %q", diskSizeGB, srcDS)
	createStart := time.Now()
	if err := dm.CreateDisk(ctx, dc, srcDS, tempDir, diskName, diskSizeGB); err != nil {
		t.Fatalf("failed to create disk: %v", err)
	}
	t.Logf("disk created in %v", time.Since(createStart))

	// Step 3: Copy disk and measure throughput
	srcPath := fmt.Sprintf("%s/%s", tempDir, diskName)
	dstPath := fmt.Sprintf("%s/%s", tempDir, cloneName)

	t.Logf("copying disk from [%s] %s to [%s] %s", srcDS, srcPath, tgtDS, dstPath)
	duration, err := dm.CopyDisk(ctx, dc, srcDS, srcPath, tgtDS, dstPath)
	if err != nil {
		t.Fatalf("disk copy failed: %v", err)
	}

	throughputMBps := float64(diskSizeGB*1024) / duration.Seconds()

	t.Logf("=== BENCHMARK RESULTS ===")
	t.Logf("  Source:     %s", srcDS)
	t.Logf("  Target:     %s", tgtDS)
	t.Logf("  Disk Size:  %d GB", diskSizeGB)
	t.Logf("  Duration:   %v (%.1f sec)", duration, duration.Seconds())
	t.Logf("  Throughput: %.1f MB/s", throughputMBps)

	if duration.Seconds() <= 0 {
		t.Error("duration should be positive")
	}
	if throughputMBps <= 0 {
		t.Error("throughput should be positive")
	}
}
