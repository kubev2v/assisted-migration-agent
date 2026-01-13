package store

import (
	"context"
	"database/sql"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"
	parsermodels "github.com/kubev2v/migration-planner/pkg/duckdb_parser/models"
)

type VMStore struct {
	parser *duckdb_parser.Parser
}

func NewVMStore(parser *duckdb_parser.Parser) *VMStore {
	return &VMStore{parser: parser}
}

func (s *VMStore) Get(ctx context.Context, id string) (*models.VM, error) {
	vms, err := s.parser.VMs(ctx, duckdb_parser.Filters{}, duckdb_parser.Options{})
	if err != nil {
		return nil, err
	}

	for _, vm := range vms {
		if vm.ID == id {
			result := vmFromParser(vm)
			return &result, nil
		}
	}

	return nil, sql.ErrNoRows
}

func (s *VMStore) List(ctx context.Context, opts ...ListOption) ([]models.VM, error) {
	parserVMs, err := s.parser.VMs(ctx, duckdb_parser.Filters{}, duckdb_parser.Options{})
	if err != nil {
		return nil, err
	}

	vms := make([]models.VM, 0, len(parserVMs))
	for _, pvm := range parserVMs {
		vms = append(vms, vmFromParser(pvm))
	}

	return vms, nil
}

func (s *VMStore) Count(ctx context.Context, opts ...ListOption) (int, error) {
	return s.parser.VMCount(ctx, duckdb_parser.Filters{})
}

func vmFromParser(pvm parsermodels.VM) models.VM {
	issues := make([]string, 0, len(pvm.Concerns))
	for _, c := range pvm.Concerns {
		issues = append(issues, c.Label)
	}

	disks := make([]models.Disk, 0, len(pvm.Disks))
	var totalDiskCapacityMiB int64
	for _, d := range pvm.Disks {
		disks = append(disks, models.Disk{
			File:     d.File,
			Capacity: d.Capacity,
			Shared:   d.Shared,
			RDM:      d.RDM,
			Bus:      d.Bus,
			Mode:     d.Mode,
		})
		totalDiskCapacityMiB += d.Capacity
	}

	nics := make([]models.NIC, 0, len(pvm.NICs))
	for i, n := range pvm.NICs {
		nics = append(nics, models.NIC{
			MAC:     n.MAC,
			Network: n.Network,
			Index:   i,
		})
	}

	return models.VM{
		ID:                    pvm.ID,
		Name:                  pvm.Name,
		UUID:                  pvm.UUID,
		Firmware:              pvm.Firmware,
		PowerState:            pvm.PowerState,
		ConnectionState:       pvm.ConnectionState,
		Host:                  pvm.Host,
		Folder:                pvm.Folder,
		Datacenter:            pvm.Datacenter,
		Cluster:               pvm.Cluster,
		CpuCount:              pvm.CpuCount,
		CoresPerSocket:        pvm.CoresPerSocket,
		MemoryMB:              pvm.MemoryMB,
		GuestName:             pvm.GuestName,
		HostName:              pvm.HostName,
		IPAddress:             pvm.IpAddress,
		DiskSize:              totalDiskCapacityMiB,
		StorageUsed:           int64(pvm.StorageUsed),
		IsTemplate:            pvm.IsTemplate,
		FaultToleranceEnabled: pvm.FaultToleranceEnabled,
		Disks:                 disks,
		NICs:                  nics,
		Issues:                issues,
	}
}

type ListOption func(duckdb_parser.Filters, duckdb_parser.Options) (duckdb_parser.Filters, duckdb_parser.Options)

type SortParam struct {
	Field string
	Desc  bool
}

func ByDatacenters(datacenters ...string) ListOption {
	return func(f duckdb_parser.Filters, o duckdb_parser.Options) (duckdb_parser.Filters, duckdb_parser.Options) {
		return f, o
	}
}

func ByClusters(clusters ...string) ListOption {
	return func(f duckdb_parser.Filters, o duckdb_parser.Options) (duckdb_parser.Filters, duckdb_parser.Options) {
		if len(clusters) > 0 {
			f.Cluster = clusters[0]
		}
		return f, o
	}
}

func ByStatus(statuses ...string) ListOption {
	return func(f duckdb_parser.Filters, o duckdb_parser.Options) (duckdb_parser.Filters, duckdb_parser.Options) {
		if len(statuses) > 0 {
			f.PowerState = statuses[0]
		}
		return f, o
	}
}

func ByIssues(issues ...string) ListOption {
	return func(f duckdb_parser.Filters, o duckdb_parser.Options) (duckdb_parser.Filters, duckdb_parser.Options) {
		return f, o
	}
}

func ByDiskSizeRange(min, max int64) ListOption {
	return func(f duckdb_parser.Filters, o duckdb_parser.Options) (duckdb_parser.Filters, duckdb_parser.Options) {
		return f, o
	}
}

func ByMemorySizeRange(min, max int64) ListOption {
	return func(f duckdb_parser.Filters, o duckdb_parser.Options) (duckdb_parser.Filters, duckdb_parser.Options) {
		return f, o
	}
}

func WithLimit(limit uint64) ListOption {
	return func(f duckdb_parser.Filters, o duckdb_parser.Options) (duckdb_parser.Filters, duckdb_parser.Options) {
		o.Limit = int(limit)
		return f, o
	}
}

func WithOffset(offset uint64) ListOption {
	return func(f duckdb_parser.Filters, o duckdb_parser.Options) (duckdb_parser.Filters, duckdb_parser.Options) {
		o.Offset = int(offset)
		return f, o
	}
}

func WithDefaultSort() ListOption {
	return func(f duckdb_parser.Filters, o duckdb_parser.Options) (duckdb_parser.Filters, duckdb_parser.Options) {
		return f, o
	}
}

func WithSort(sorts []SortParam) ListOption {
	return func(f duckdb_parser.Filters, o duckdb_parser.Options) (duckdb_parser.Filters, duckdb_parser.Options) {
		return f, o
	}
}
