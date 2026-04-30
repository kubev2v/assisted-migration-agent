package vmware

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math/rand"
	"strings"
	"time"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/soap"
	"github.com/vmware/govmomi/vim25/types"
	"go.uber.org/zap"
)

// DiskManager wraps govmomi's VirtualDiskManager to create, copy, and delete
// virtual disks on vSphere datastores. It is used by the forecaster to
// benchmark throughput between datastore pairs.
type DiskManager struct {
	gc *govmomi.Client
}

// NewDiskManager creates a new DiskManager.
func NewDiskManager(gc *govmomi.Client) *DiskManager {
	return &DiskManager{gc: gc}
}

// CreateDisk creates a virtual disk on the given datastore.
// The disk is created as thin-provisioned. The filler VM writes random data
// over it to defeat storage zero-block optimization. Thin provisioning allows
// accurate progress tracking by stat-ing the flat VMDK's physical size.
func (d *DiskManager) CreateDisk(ctx context.Context, datacenter *object.Datacenter, datastore, folder, name string, sizeGB int) error {
	vdm := object.NewVirtualDiskManager(d.gc.Client)

	spec := &types.FileBackedVirtualDiskSpec{
		VirtualDiskSpec: types.VirtualDiskSpec{
			AdapterType: string(types.VirtualDiskAdapterTypeLsiLogic),
			DiskType:    string(types.VirtualDiskTypeThin),
		},
		CapacityKb: int64(sizeGB) * 1024 * 1024, // GB to KB
	}

	path := fmt.Sprintf("[%s] %s/%s", datastore, folder, name)

	task, err := vdm.CreateVirtualDisk(ctx, path, datacenter, spec)
	if err != nil {
		return fmt.Errorf("failed to create virtual disk: %w", err)
	}

	if err := task.Wait(ctx); err != nil {
		return fmt.Errorf("virtual disk creation failed: %w", err)
	}

	return nil
}

// CopyDisk copies a virtual disk from source to destination and returns the
// wall-clock duration. This is the core benchmarking operation.
func (d *DiskManager) CopyDisk(ctx context.Context, datacenter *object.Datacenter, srcDS, srcPath, dstDS, dstPath string) (time.Duration, error) {
	vdm := object.NewVirtualDiskManager(d.gc.Client)

	src := fmt.Sprintf("[%s] %s", srcDS, srcPath)
	dst := fmt.Sprintf("[%s] %s", dstDS, dstPath)

	start := time.Now()

	task, err := vdm.CopyVirtualDisk(ctx, src, datacenter, dst, datacenter, nil, false)
	if err != nil {
		return 0, fmt.Errorf("failed to start disk copy: %w", err)
	}

	if err := task.Wait(ctx); err != nil {
		return time.Since(start), fmt.Errorf("disk copy failed: %w", err)
	}

	return time.Since(start), nil
}

// DeleteDisk deletes a virtual disk from a datastore.
func (d *DiskManager) DeleteDisk(ctx context.Context, datacenter *object.Datacenter, datastore, path string) error {
	vdm := object.NewVirtualDiskManager(d.gc.Client)

	fullPath := fmt.Sprintf("[%s] %s", datastore, path)

	task, err := vdm.DeleteVirtualDisk(ctx, fullPath, datacenter)
	if err != nil {
		return fmt.Errorf("failed to start disk deletion: %w", err)
	}

	if err := task.Wait(ctx); err != nil {
		return fmt.Errorf("disk deletion failed: %w", err)
	}

	return nil
}

// CreateDirectory creates a directory on a datastore.
func (d *DiskManager) CreateDirectory(ctx context.Context, datacenter *object.Datacenter, datastore, path string) error {
	fm := object.NewFileManager(d.gc.Client)
	fullPath := fmt.Sprintf("[%s] %s", datastore, path)

	return fm.MakeDirectory(ctx, fullPath, datacenter, false)
}

// DeleteDirectory deletes a directory and all its contents from a datastore.
func (d *DiskManager) DeleteDirectory(ctx context.Context, datacenter *object.Datacenter, datastore, path string) error {
	fm := object.NewFileManager(d.gc.Client)
	fullPath := fmt.Sprintf("[%s] %s", datastore, path)

	task, err := fm.DeleteDatastoreFile(ctx, fullPath, datacenter)
	if err != nil {
		return fmt.Errorf("failed to start directory deletion: %w", err)
	}

	if err := task.Wait(ctx); err != nil {
		return fmt.Errorf("directory deletion failed: %w", err)
	}

	return nil
}

// FindDatacenter finds a datacenter by name or returns the default one.
func (d *DiskManager) FindDatacenter(ctx context.Context, name string) (*object.Datacenter, error) {
	finder := find.NewFinder(d.gc.Client, true)

	if name == "" {
		return finder.DefaultDatacenter(ctx)
	}

	return finder.Datacenter(ctx, name)
}

// FindDatastore finds a datastore by name within a datacenter.
func (d *DiskManager) FindDatastore(ctx context.Context, datacenter *object.Datacenter, name string) (*object.Datastore, error) {
	finder := find.NewFinder(d.gc.Client, true)
	finder.SetDatacenter(datacenter)

	return finder.Datastore(ctx, name)
}

// FindCommonHost returns a resource pool and host that has access to both the
// source and target datastores. If no common host exists, it falls back to the
// first host attached to the source datastore (the copy may still work via
// vCenter-routed operations like CopyVirtualDisk).
func (d *DiskManager) FindCommonHost(ctx context.Context, datacenter *object.Datacenter, sourceDS, targetDS string) (*object.ResourcePool, *object.HostSystem, error) {
	log := zap.S().Named("disk-manager")

	srcDS, err := d.FindDatastore(ctx, datacenter, sourceDS)
	if err != nil {
		return nil, nil, fmt.Errorf("source datastore %q not found: %w", sourceDS, err)
	}
	tgtDS, err := d.FindDatastore(ctx, datacenter, targetDS)
	if err != nil {
		return nil, nil, fmt.Errorf("target datastore %q not found: %w", targetDS, err)
	}

	srcHosts, err := srcDS.AttachedHosts(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get hosts for source datastore %q: %w", sourceDS, err)
	}
	if len(srcHosts) == 0 {
		return nil, nil, fmt.Errorf("no hosts attached to source datastore %q", sourceDS)
	}

	tgtHosts, err := tgtDS.AttachedHosts(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get hosts for target datastore %q: %w", targetDS, err)
	}

	// Build set of target host references for intersection
	tgtSet := make(map[string]struct{}, len(tgtHosts))
	for _, h := range tgtHosts {
		tgtSet[h.Reference().Value] = struct{}{}
	}

	// Find first common host
	var selected *object.HostSystem
	for _, h := range srcHosts {
		if _, ok := tgtSet[h.Reference().Value]; ok {
			selected = h
			break
		}
	}

	if selected == nil {
		log.Warnw("no common host found between datastores, falling back to source-only host",
			"sourceDS", sourceDS, "targetDS", targetDS, "host", srcHosts[0].Reference().Value)
		selected = srcHosts[0]
	}

	pool, err := selected.ResourcePool(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get resource pool for host: %w", err)
	}

	return pool, selected, nil
}

// FindHostByName resolves a user-specified ESXi host name to a HostSystem object
// and returns its resource pool. Supports both short names (e.g. "esxi-01.example.com")
// and full inventory paths (e.g. "/datacenter/host/cluster/esxi-01.example.com").
func (d *DiskManager) FindHostByName(ctx context.Context, datacenter *object.Datacenter, hostName string) (*object.ResourcePool, *object.HostSystem, error) {
	finder := find.NewFinder(d.gc.Client, true)
	finder.SetDatacenter(datacenter)

	host, err := finder.HostSystem(ctx, hostName)
	if err != nil {
		return nil, nil, fmt.Errorf("host %q not found: %w", hostName, err)
	}

	pool, err := host.ResourcePool(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get resource pool for host %q: %w", hostName, err)
	}

	return pool, host, nil
}

// FindFolder returns the default VM folder for the datacenter.
func (d *DiskManager) FindFolder(ctx context.Context, datacenter *object.Datacenter) (*object.Folder, error) {
	finder := find.NewFinder(d.gc.Client, true)
	finder.SetDatacenter(datacenter)
	return finder.DefaultFolder(ctx)
}

// DatastoreExists checks whether a datastore is accessible.
func (d *DiskManager) DatastoreExists(ctx context.Context, datacenter *object.Datacenter, name string) error {
	ds, err := d.FindDatastore(ctx, datacenter, name)
	if err != nil {
		return fmt.Errorf("datastore %q not found: %w", name, err)
	}

	_, err = ds.AttachedHosts(ctx)
	if err != nil {
		if soap.IsSoapFault(err) {
			return fmt.Errorf("datastore %q is not accessible: %w", name, err)
		}
		return fmt.Errorf("failed to check datastore %q accessibility: %w", name, err)
	}

	return nil
}

// FillDiskRandom overwrites a VMDK's flat file with pseudo-random data via the
// datastore HTTP API. This defeats storage zero-block optimization (dedup,
// XCOPY short-circuit of all-zero blocks) so that benchmarks measure realistic
// throughput. The diskPath is datastore-relative, e.g. "dir/benchmark-disk.vmdk".
// onProgress is called periodically with the cumulative bytes uploaded so far.
func (d *DiskManager) FillDiskRandom(ctx context.Context, datacenter *object.Datacenter, datastoreName, diskPath string, sizeGB int, onProgress func(bytesWritten int64)) error {
	log := zap.S().Named("disk_manager")

	ds, err := d.FindDatastore(ctx, datacenter, datastoreName)
	if err != nil {
		return fmt.Errorf("datastore %q not found: %w", datastoreName, err)
	}

	// Derive flat VMDK path: "dir/foo.vmdk" → "dir/foo-flat.vmdk"
	flatPath := strings.TrimSuffix(diskPath, ".vmdk") + "-flat.vmdk"
	sizeBytes := int64(sizeGB) * 1024 * 1024 * 1024

	log.Infow("filling disk with random data",
		"datastore", datastoreName, "flatPath", flatPath, "sizeGB", sizeGB)

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	reader := io.LimitReader(&fastRandReader{rng: rng}, sizeBytes)

	// Wrap with progress tracking
	if onProgress != nil {
		reader = &progressReader{r: reader, onProgress: onProgress}
	}

	p := &soap.Upload{
		ContentLength: sizeBytes,
		Type:          "application/octet-stream",
		Method:        "PUT",
	}

	start := time.Now()
	if err := ds.Upload(ctx, reader, flatPath, p); err != nil {
		return fmt.Errorf("failed to upload random data to %s: %w", flatPath, err)
	}

	log.Infow("disk filled with random data",
		"datastore", datastoreName, "flatPath", flatPath,
		"sizeGB", sizeGB, "duration", time.Since(start).Round(time.Second))

	return nil
}

// progressReader wraps an io.Reader and reports cumulative bytes read.
type progressReader struct {
	r          io.Reader
	total      int64
	onProgress func(bytesWritten int64)
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.r.Read(p)
	if n > 0 {
		pr.total += int64(n)
		pr.onProgress(pr.total)
	}
	return n, err
}

// fastRandReader generates pseudo-random bytes using math/rand. Not
// cryptographically secure — its only purpose is producing non-zero data to
// defeat storage zero-block optimization during benchmarks.
type fastRandReader struct {
	rng *rand.Rand
}

func (r *fastRandReader) Read(p []byte) (int, error) {
	n := len(p)
	i := 0
	for ; i+8 <= n; i += 8 {
		binary.LittleEndian.PutUint64(p[i:], r.rng.Uint64())
	}
	if i < n {
		val := r.rng.Uint64()
		for ; i < n; i++ {
			p[i] = byte(val)
			val >>= 8
		}
	}
	return n, nil
}
