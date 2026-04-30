package vmware

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25"
	"github.com/vmware/govmomi/vim25/soap"
	"github.com/vmware/govmomi/vim25/types"
	"go.uber.org/zap"
)

const (
	fillerDiskSizeMB = 256

	defaultAssetsDir = "/app/assets"
	fillerImageFile  = "alpine-filler.raw.gz"
	seedISOFile      = "seed.iso.gz"
	assetsDirEnvVar  = "AGENT_ASSETS_DIR"
)

// AssetsDir returns the directory containing filler image assets.
// Reads from AGENT_ASSETS_DIR env var, defaulting to /app/assets.
func AssetsDir() string {
	if dir := os.Getenv(assetsDirEnvVar); dir != "" {
		return dir
	}
	return defaultAssetsDir
}

// FillerImagePaths holds datastore paths for the deployed filler image files.
type FillerImagePaths struct {
	BootVMDK string // datastore-relative path to boot VMDK
	SeedISO  string // datastore-relative path to cloud-init seed ISO
}

// DeployFillerImage creates a VMDK on the datastore, writes the Alpine boot
// image into its flat extent via HTTP PUT, and uploads the seed ISO.
// The Alpine image and seed ISO are read from the assets directory on disk
// (configured via AGENT_ASSETS_DIR env var, default: /app/assets).
func DeployFillerImage(ctx context.Context, c *vim25.Client, dc *object.Datacenter, ds *object.Datastore,
	pool *object.ResourcePool, folder *object.Folder, host *object.HostSystem) (paths FillerImagePaths, cleanup func(), err error) {

	log := zap.S().Named("filler-image")
	dsName := ds.Name()
	importDir := fmt.Sprintf("filler-image-%d", time.Now().UnixNano())
	vmdkName := "alpine-filler.vmdk"
	vmdkPath := fmt.Sprintf("%s/%s", importDir, vmdkName)
	fullVMDKPath := fmt.Sprintf("[%s] %s", dsName, vmdkPath)

	// Resolve asset file paths
	assetsDir := AssetsDir()
	fillerImagePath := filepath.Join(assetsDir, fillerImageFile)
	seedISOPath := filepath.Join(assetsDir, seedISOFile)

	// Verify assets exist before doing any vSphere work
	if _, err := os.Stat(fillerImagePath); err != nil {
		return paths, nil, fmt.Errorf("filler image not found at %s (set %s to override): %w", fillerImagePath, assetsDirEnvVar, err)
	}
	if _, err := os.Stat(seedISOPath); err != nil {
		return paths, nil, fmt.Errorf("seed ISO not found at %s (set %s to override): %w", seedISOPath, assetsDirEnvVar, err)
	}

	// Cleanup function deletes the whole filler-image directory
	fm := object.NewFileManager(c)
	cleanupFn := func() {
		cleanCtx := context.Background()
		dirPath := fmt.Sprintf("[%s] %s", dsName, importDir)
		task, err := fm.DeleteDatastoreFile(cleanCtx, dirPath, dc)
		if err != nil {
			log.Debugw("cleanup: failed to start filler image deletion", "error", err)
			return
		}
		if err := task.Wait(cleanCtx); err != nil {
			log.Debugw("cleanup: failed to delete filler image", "error", err)
		} else {
			log.Infow("filler image cleaned up", "path", dirPath)
		}
	}

	defer func() {
		if err != nil && cleanup != nil {
			cleanup()
			cleanup = nil
		}
	}()

	// Step 1: Create the filler-image directory on the datastore
	dirPath := fmt.Sprintf("[%s] %s", dsName, importDir)
	log.Infow("creating directory on datastore", "path", dirPath)
	if err := fm.MakeDirectory(ctx, dirPath, dc, true); err != nil {
		return paths, nil, fmt.Errorf("failed to create directory: %w", err)
	}

	// Step 2: Create a thin VMDK on the datastore (server-side, no upload)
	vdm := object.NewVirtualDiskManager(c)
	spec := &types.FileBackedVirtualDiskSpec{
		VirtualDiskSpec: types.VirtualDiskSpec{
			AdapterType: string(types.VirtualDiskAdapterTypeLsiLogic),
			DiskType:    string(types.VirtualDiskTypeThin),
		},
		CapacityKb: int64(fillerDiskSizeMB) * 1024, // MB to KB
	}

	log.Infow("creating thin VMDK on datastore", "path", fullVMDKPath, "sizeMB", fillerDiskSizeMB)
	task, err := vdm.CreateVirtualDisk(ctx, fullVMDKPath, dc, spec)
	if err != nil {
		return paths, cleanupFn, fmt.Errorf("failed to create filler VMDK: %w", err)
	}
	if err := task.Wait(ctx); err != nil {
		return paths, cleanupFn, fmt.Errorf("filler VMDK creation failed: %w", err)
	}
	cleanup = cleanupFn

	// Step 3: Upload the raw Alpine boot image into the flat VMDK extent
	flatPath := fmt.Sprintf("%s/%s", importDir, strings.TrimSuffix(vmdkName, ".vmdk")+"-flat.vmdk")
	rawSize := int64(fillerDiskSizeMB) * 1024 * 1024

	log.Infow("uploading raw boot image to flat VMDK", "flatPath", flatPath, "sizeMB", fillerDiskSizeMB)

	fillerFile, err := os.Open(fillerImagePath)
	if err != nil {
		return paths, cleanup, fmt.Errorf("failed to open filler image: %w", err)
	}
	defer func() { _ = fillerFile.Close() }()

	gz, err := gzip.NewReader(fillerFile)
	if err != nil {
		return paths, cleanup, fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer func() { _ = gz.Close() }()

	p := &soap.Upload{
		ContentLength: rawSize,
		Type:          "application/octet-stream",
		Method:        "PUT",
	}
	if err := ds.Upload(ctx, gz, flatPath, p); err != nil {
		return paths, cleanup, fmt.Errorf("failed to upload boot image: %w", err)
	}

	paths.BootVMDK = vmdkPath
	log.Infow("filler boot VMDK deployed", "path", vmdkPath)

	// Step 4: Upload seed ISO
	log.Infow("uploading seed ISO to datastore")
	seedDSPath := fmt.Sprintf("%s/seed.iso", importDir)

	tmpDir, err := os.MkdirTemp("", "filler-seed-*")
	if err != nil {
		return paths, cleanup, fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	isoTmpPath := tmpDir + "/seed.iso"
	if err := decompressGzFile(seedISOPath, isoTmpPath); err != nil {
		return paths, cleanup, fmt.Errorf("failed to decompress seed ISO: %w", err)
	}

	isoFile, err := os.Open(isoTmpPath)
	if err != nil {
		return paths, cleanup, fmt.Errorf("failed to open seed ISO: %w", err)
	}
	defer func() { _ = isoFile.Close() }()

	isoStat, err := isoFile.Stat()
	if err != nil {
		return paths, cleanup, fmt.Errorf("failed to stat seed ISO: %w", err)
	}
	isoUpload := soap.DefaultUpload
	isoUpload.ContentLength = isoStat.Size()
	if err := ds.Upload(ctx, isoFile, seedDSPath, &isoUpload); err != nil {
		return paths, cleanup, fmt.Errorf("failed to upload seed ISO: %w", err)
	}
	paths.SeedISO = seedDSPath
	log.Infow("seed ISO uploaded", "path", seedDSPath)

	return paths, cleanup, nil
}

// decompressGzFile decompresses a gzipped file to an output path.
func decompressGzFile(srcPath, outPath string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()

	gz, err := gzip.NewReader(src)
	if err != nil {
		return err
	}
	defer func() { _ = gz.Close() }()

	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	_, err = io.Copy(f, gz)
	return err
}
