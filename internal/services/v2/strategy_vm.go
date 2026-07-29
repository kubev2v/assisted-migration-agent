package v2

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/types"
	"go.uber.org/zap"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/pkg/vmware"
)

type vmStrategy struct {
	dm       *vmware.DiskManager
	gc       *govmomi.Client
	paths    vmware.FillerImagePaths
	cleanup  func()
	pool     *object.ResourcePool
	host     *object.HostSystem
	hostName string
	folder   *object.Folder
}

func newVMStrategy(dm *vmware.DiskManager, gc *govmomi.Client) BenchmarkStrategy {
	return &vmStrategy{
		dm: dm,
		gc: gc,
	}
}

func (s *vmStrategy) Name() string { return "vm_native" }

func (s *vmStrategy) SelectedHost() string {
	return s.hostName
}

func (s *vmStrategy) Setup(ctx context.Context, dc *object.Datacenter, pair models.DatastorePair) error {
	log := zap.S().Named("vm-strategy")

	var pool *object.ResourcePool
	var host *object.HostSystem
	var err error
	if pair.Host != "" {
		pool, host, err = s.dm.FindHostByName(ctx, dc, pair.Host)
	} else {
		pool, host, err = s.dm.FindCommonHost(ctx, dc, pair.SourceDatastore, pair.TargetDatastore)
	}
	if err != nil {
		return fmt.Errorf("failed to find host: %w", err)
	}
	s.pool = pool
	s.host = host

	hostName, err := host.ObjectName(ctx)
	if err != nil {
		return fmt.Errorf("failed to resolve host name: %w", err)
	}
	s.hostName = hostName

	log.Infow("selected host for benchmark", "pair", pair.Name, "host", hostName)

	folder, err := s.dm.FindFolder(ctx, dc)
	if err != nil {
		return fmt.Errorf("failed to find VM folder: %w", err)
	}
	s.folder = folder

	ds, err := s.dm.FindDatastore(ctx, dc, pair.SourceDatastore)
	if err != nil {
		return fmt.Errorf("failed to find datastore: %w", err)
	}

	log.Infow("deploying Alpine filler image", "datastore", pair.SourceDatastore)
	paths, cleanup, err := vmware.DeployFillerImage(ctx, s.gc.Client, dc, ds, pool, folder, host)
	if err != nil {
		return fmt.Errorf("failed to deploy filler image: %w", err)
	}
	s.paths = paths
	s.cleanup = cleanup

	log.Infow("filler image deployed", "vmdk", paths.BootVMDK, "seed", paths.SeedISO)
	return nil
}

func (s *vmStrategy) FillDisk(ctx context.Context, dc *object.Datacenter, pair models.DatastorePair,
	srcDiskPath string, diskSizeGB int, onProgress func(bytesWritten int64)) error {

	log := zap.S().Named("vm-strategy")
	srcDS := pair.SourceDatastore

	srcDsObj, err := s.dm.FindDatastore(ctx, dc, srcDS)
	if err != nil {
		return fmt.Errorf("source datastore not found: %w", err)
	}

	vmName := fmt.Sprintf("forecaster-filler-%d", time.Now().UnixNano())
	bootDiskPath := fmt.Sprintf("[%s] %s", srcDS, s.paths.BootVMDK)
	benchDiskPath := fmt.Sprintf("[%s] %s", srcDS, srcDiskPath)
	seedISOPath := fmt.Sprintf("[%s] %s", srcDS, s.paths.SeedISO)

	log.Infow("creating filler VM", "name", vmName, "bootDisk", bootDiskPath,
		"benchDisk", benchDiskPath, "seedISO", seedISOPath)

	srcDsRef := srcDsObj.Reference()
	unit1 := int32(1)
	ideCtrlKey := int32(200)
	spec := types.VirtualMachineConfigSpec{
		Name:    vmName,
		GuestId: string(types.VirtualMachineGuestOsIdentifierOtherLinux64Guest),
		Files: &types.VirtualMachineFileInfo{
			VmPathName: fmt.Sprintf("[%s]", srcDS),
		},
		NumCPUs:  1,
		MemoryMB: 256,
		DeviceChange: []types.BaseVirtualDeviceConfigSpec{
			&types.VirtualDeviceConfigSpec{
				Operation: types.VirtualDeviceConfigSpecOperationAdd,
				Device: &types.VirtualLsiLogicController{
					VirtualSCSIController: types.VirtualSCSIController{
						SharedBus: types.VirtualSCSISharingNoSharing,
						VirtualController: types.VirtualController{
							VirtualDevice: types.VirtualDevice{
								Key: 1000,
							},
							BusNumber: 0,
						},
					},
				},
			},
			&types.VirtualDeviceConfigSpec{
				Operation: types.VirtualDeviceConfigSpecOperationAdd,
				Device: &types.VirtualIDEController{
					VirtualController: types.VirtualController{
						VirtualDevice: types.VirtualDevice{
							Key: ideCtrlKey,
						},
						BusNumber: 0,
					},
				},
			},
			&types.VirtualDeviceConfigSpec{
				Operation: types.VirtualDeviceConfigSpecOperationAdd,
				Device: &types.VirtualDisk{
					VirtualDevice: types.VirtualDevice{
						Key: 2000,
						Backing: &types.VirtualDiskFlatVer2BackingInfo{
							VirtualDeviceFileBackingInfo: types.VirtualDeviceFileBackingInfo{
								FileName:  bootDiskPath,
								Datastore: &srcDsRef,
							},
							DiskMode: string(types.VirtualDiskModePersistent),
						},
						ControllerKey: 1000,
						UnitNumber:    new(int32),
					},
					CapacityInBytes: -1,
				},
			},
			&types.VirtualDeviceConfigSpec{
				Operation: types.VirtualDeviceConfigSpecOperationAdd,
				Device: &types.VirtualDisk{
					VirtualDevice: types.VirtualDevice{
						Key: 2001,
						Backing: &types.VirtualDiskFlatVer2BackingInfo{
							VirtualDeviceFileBackingInfo: types.VirtualDeviceFileBackingInfo{
								FileName:  benchDiskPath,
								Datastore: &srcDsRef,
							},
							DiskMode: string(types.VirtualDiskModePersistent),
						},
						ControllerKey: 1000,
						UnitNumber:    &unit1,
					},
					CapacityInBytes: -1,
				},
			},
			&types.VirtualDeviceConfigSpec{
				Operation: types.VirtualDeviceConfigSpecOperationAdd,
				Device: &types.VirtualCdrom{
					VirtualDevice: types.VirtualDevice{
						Key: 3000,
						Backing: &types.VirtualCdromIsoBackingInfo{
							VirtualDeviceFileBackingInfo: types.VirtualDeviceFileBackingInfo{
								FileName:  seedISOPath,
								Datastore: &srcDsRef,
							},
						},
						ControllerKey: ideCtrlKey,
						UnitNumber:    new(int32),
					},
				},
			},
		},
	}

	createTask, err := s.folder.CreateVM(ctx, spec, s.pool, s.host)
	if err != nil {
		return fmt.Errorf("failed to create filler VM: %w", err)
	}

	createResult, err := createTask.WaitForResult(ctx)
	if err != nil {
		return fmt.Errorf("filler VM creation failed: %w", err)
	}

	vmRef, ok := createResult.Result.(types.ManagedObjectReference)
	if !ok {
		return fmt.Errorf("unexpected CreateVM result type: %T", createResult.Result)
	}
	vm := object.NewVirtualMachine(s.gc.Client, vmRef)

	defer func() {
		destroyCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		powerState, err := vm.PowerState(destroyCtx)
		if err == nil && powerState == types.VirtualMachinePowerStatePoweredOn {
			powerOffTask, err := vm.PowerOff(destroyCtx)
			if err == nil {
				_ = powerOffTask.Wait(destroyCtx)
			}
		}

		devices, err := vm.Device(destroyCtx)
		if err == nil {
			for _, dev := range devices {
				if _, ok := dev.(*types.VirtualDisk); ok {
					if err := vm.RemoveDevice(destroyCtx, true, dev); err != nil {
						log.Debugw("failed to detach disk before destroy", "error", err)
					}
				}
			}
		}

		destroyTask, err := vm.Destroy(destroyCtx)
		if err == nil {
			_ = destroyTask.Wait(destroyCtx)
		}
		log.Infow("filler VM destroyed")
	}()

	log.Infow("powering on filler VM (Alpine will fill disk and power off)")
	powerOnTask, err := vm.PowerOn(ctx)
	if err != nil {
		return fmt.Errorf("failed to power on filler VM: %w", err)
	}
	if err := powerOnTask.Wait(ctx); err != nil {
		return fmt.Errorf("filler VM power on failed: %w", err)
	}

	totalBytes := int64(diskSizeGB) * 1024 * 1024 * 1024
	flatDiskPath := strings.TrimSuffix(srcDiskPath, ".vmdk") + "-flat.vmdk"
	start := time.Now()
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	fillTimeout := time.Duration(diskSizeGB)*30*time.Second + 3*time.Minute
	deadline := time.NewTimer(fillTimeout)
	defer deadline.Stop()

	log.Infow("waiting for filler VM to complete", "timeout", fillTimeout)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("filler VM did not power off within %v", fillTimeout)
		case <-ticker.C:
			powerState, err := vm.PowerState(ctx)
			if err != nil {
				log.Warnw("failed to check power state", "error", err)
				continue
			}

			if powerState == types.VirtualMachinePowerStatePoweredOff {
				log.Infow("filler VM powered off — disk fill complete",
					"elapsed", time.Since(start).Round(time.Second))
				if onProgress != nil {
					onProgress(totalBytes)
				}

				if fi, err := srcDsObj.Stat(ctx, flatDiskPath); err == nil {
					physicalBytes := fi.GetFileInfo().FileSize
					logicalBytes := totalBytes
					pct := 0.0
					if logicalBytes > 0 {
						pct = float64(physicalBytes) / float64(logicalBytes) * 100
					}
					log.Infow("benchmark disk usage after fill",
						"physicalMB", physicalBytes/(1024*1024),
						"logicalMB", logicalBytes/(1024*1024),
						"utilization", fmt.Sprintf("%.1f%%", pct))
				} else {
					log.Warnw("failed to stat benchmark disk", "path", flatDiskPath, "error", err)
				}

				return nil
			}

			if onProgress != nil {
				if fi, err := srcDsObj.Stat(ctx, flatDiskPath); err == nil {
					physicalBytes := fi.GetFileInfo().FileSize
					if physicalBytes > totalBytes {
						physicalBytes = totalBytes
					}
					onProgress(physicalBytes)
				}
			}
		}
	}
}

func (s *vmStrategy) RunBenchmark(ctx context.Context, dc *object.Datacenter, pair models.DatastorePair,
	srcDiskPath, dstDiskPath string, _ int) (BenchmarkResult, error) {

	duration, err := s.dm.CopyDisk(ctx, dc, pair.SourceDatastore, srcDiskPath, pair.TargetDatastore, dstDiskPath)
	if err != nil {
		return BenchmarkResult{Duration: duration}, err
	}

	return BenchmarkResult{Duration: duration}, nil
}

func (s *vmStrategy) Teardown(_ context.Context) error {
	if s.cleanup != nil {
		s.cleanup()
		s.cleanup = nil
	}
	return nil
}
