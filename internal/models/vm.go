package models

import (
	"github.com/kubev2v/assisted-migration-agent/internal/util"
	vsphere "github.com/kubev2v/forklift/pkg/controller/provider/model/vsphere"
)

type VM struct {
	ID                string
	Name              string
	State             string
	Datacenter        string
	Cluster           string
	DiskSize          int64 // in MB
	Memory            int64 // in MB
	Issues            []string
	InspectionState   string
	InspectionError   string
	InspectionResults []byte
}

func NewVMFromForklift(vm vsphere.VM, clusterName, datacenterName string) VM {
	var issues []string
	for _, c := range vm.Concerns {
		issues = append(issues, c.Label)
	}

	var diskSizeBytes int64
	for _, d := range vm.Disks {
		diskSizeBytes += d.Capacity
	}

	return VM{
		ID:         vm.ID,
		Name:       vm.Name,
		State:      vm.PowerState,
		Datacenter: datacenterName,
		Cluster:    clusterName,
		DiskSize:   util.ConvertBytesToMB(diskSizeBytes),
		Memory:     int64(vm.MemoryMB),
		Issues:     issues,
	}
}
