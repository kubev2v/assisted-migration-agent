package test

import (
	"context"
	"database/sql"
)

type VM struct {
	ID              string
	Name            string
	PowerState      string
	ConnectionState string
	Cluster         string
	Datacenter      string
	Host            string
	Folder          string
	Firmware        string
	UUID            string
	Memory          int32
	CPUs            int32
	GuestName       string
	DNSName         string
	IPAddress       string
	StorageUsed     int32
	IsTemplate      bool
	FTEnabled       bool
}

type Disk struct {
	VMID        string
	CapacityMiB int64
	Path        string
	DiskMode    string
	Shared      bool
	RDM         bool
	Controller  string
}

type NIC struct {
	VMID    string
	Network string
	MAC     string
}

type Concern struct {
	VMID      string
	ConcernID string
	Label     string
}

var VMs = []VM{
	{"vm-001", "web-server-1", "poweredOn", "connected", "production", "DC1", "esxi-01.local", "/vms/web", "bios", "uuid-001", 4096, 2, "Red Hat Enterprise Linux 8", "web1.local", "192.168.1.101", 50, false, false},
	{"vm-002", "web-server-2", "poweredOn", "connected", "production", "DC1", "esxi-01.local", "/vms/web", "bios", "uuid-002", 4096, 2, "Red Hat Enterprise Linux 8", "web2.local", "192.168.1.102", 55, false, false},
	{"vm-003", "db-server-1", "poweredOn", "connected", "production", "DC1", "esxi-02.local", "/vms/db", "efi", "uuid-003", 16384, 8, "Red Hat Enterprise Linux 9", "db1.local", "192.168.1.201", 800, false, false},
	{"vm-004", "db-server-2", "poweredOff", "connected", "production", "DC1", "esxi-02.local", "/vms/db", "efi", "uuid-004", 16384, 8, "Red Hat Enterprise Linux 9", "db2.local", "192.168.1.202", 750, false, true},
	{"vm-005", "app-server-1", "poweredOn", "connected", "staging", "DC1", "esxi-03.local", "/vms/app", "bios", "uuid-005", 8192, 4, "CentOS 8", "app1.local", "192.168.2.101", 120, false, false},
	{"vm-006", "app-server-2", "poweredOn", "connected", "staging", "DC1", "esxi-03.local", "/vms/app", "bios", "uuid-006", 8192, 4, "CentOS 8", "app2.local", "192.168.2.102", 115, false, false},
	{"vm-007", "cache-server-1", "suspended", "connected", "staging", "DC1", "esxi-03.local", "/vms/cache", "bios", "uuid-007", 2048, 2, "Ubuntu 22.04", "cache1.local", "192.168.2.201", 30, false, false},
	{"vm-008", "dev-server-1", "poweredOn", "connected", "development", "DC2", "esxi-04.local", "/vms/dev", "bios", "uuid-008", 4096, 2, "Fedora 38", "dev1.local", "192.168.3.101", 80, false, false},
	{"vm-009", "dev-server-2", "poweredOff", "connected", "development", "DC2", "esxi-04.local", "/vms/dev", "bios", "uuid-009", 4096, 2, "Fedora 38", "dev2.local", "192.168.3.102", 75, false, false},
	{"vm-010", "test-server-1", "poweredOn", "connected", "development", "DC2", "esxi-04.local", "/vms/test", "bios", "uuid-010", 2048, 1, "Alpine Linux", "test1.local", "192.168.3.201", 20, true, false},
}

var Disks = []Disk{
	{"vm-001", 100, "[datastore1] vm-001/disk1.vmdk", "persistent", false, false, "SCSI"},
	{"vm-002", 100, "[datastore1] vm-002/disk1.vmdk", "persistent", false, false, "SCSI"},
	{"vm-003", 500, "[datastore1] vm-003/disk1.vmdk", "persistent", false, false, "SCSI"},
	{"vm-003", 500, "[datastore1] vm-003/disk2.vmdk", "persistent", false, false, "SCSI"},
	{"vm-004", 1000, "[datastore1] vm-004/disk1.vmdk", "persistent", true, false, "SCSI"},
	{"vm-005", 200, "[datastore1] vm-005/disk1.vmdk", "persistent", false, false, "SCSI"},
	{"vm-006", 200, "[datastore1] vm-006/disk1.vmdk", "persistent", false, false, "SCSI"},
	{"vm-007", 50, "[datastore1] vm-007/disk1.vmdk", "independent_persistent", false, true, "SCSI"},
	{"vm-008", 150, "[datastore1] vm-008/disk1.vmdk", "persistent", false, false, "NVME"},
	{"vm-009", 150, "[datastore1] vm-009/disk1.vmdk", "persistent", false, false, "NVME"},
	{"vm-010", 80, "[datastore1] vm-010/disk1.vmdk", "persistent", false, false, "SCSI"},
}

var NICs = []NIC{
	{"vm-001", "VM Network", "00:50:56:01:01:01"},
	{"vm-002", "VM Network", "00:50:56:01:02:01"},
	{"vm-003", "Production", "00:50:56:01:03:01"},
	{"vm-003", "Management", "00:50:56:01:03:02"},
	{"vm-007", "Staging", "00:50:56:01:07:01"},
}

var Concerns = []Concern{
	{"vm-003", "concern-001", "High memory usage"},
	{"vm-003", "concern-002", "Outdated VMware Tools"},
	{"vm-004", "concern-003", "No backup configured"},
	{"vm-007", "concern-004", "Suspended state"},
	{"vm-007", "concern-005", "Network disconnected"},
	{"vm-007", "concern-006", "Storage warning"},
}

// InsertVMs inserts all test VM data into the database.
func InsertVMs(ctx context.Context, db *sql.DB) error {
	for _, vm := range VMs {
		_, err := db.ExecContext(ctx, `
			INSERT INTO vinfo (
				"VM ID", "VM", "Powerstate", "Connection state", "Cluster", "Datacenter",
				"Host", "Folder ID", "Firmware", "SMBIOS UUID", "Memory", "CPUs",
				"OS according to the configuration file", "DNS Name", "Primary IP Address",
				"In Use MiB", "Template", "FT State"
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, vm.ID, vm.Name, vm.PowerState, vm.ConnectionState, vm.Cluster, vm.Datacenter,
			vm.Host, vm.Folder, vm.Firmware, vm.UUID, vm.Memory, vm.CPUs,
			vm.GuestName, vm.DNSName, vm.IPAddress, vm.StorageUsed, vm.IsTemplate, vm.FTEnabled)
		if err != nil {
			return err
		}
	}

	for _, vm := range VMs {
		_, err := db.ExecContext(ctx, `
			INSERT INTO vcpu ("VM ID", "Sockets", "Cores p/s")
			VALUES (?, ?, ?)
		`, vm.ID, 1, vm.CPUs)
		if err != nil {
			return err
		}
	}

	for _, disk := range Disks {
		_, err := db.ExecContext(ctx, `
			INSERT INTO vdisk ("VM ID", "Capacity MiB", "Path", "Disk Mode", "Sharing mode", "Raw", "Controller")
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, disk.VMID, disk.CapacityMiB, disk.Path, disk.DiskMode, disk.Shared, disk.RDM, disk.Controller)
		if err != nil {
			return err
		}
	}

	for _, nic := range NICs {
		_, err := db.ExecContext(ctx, `
			INSERT INTO vnetwork ("VM ID", "Network", "Mac Address")
			VALUES (?, ?, ?)
		`, nic.VMID, nic.Network, nic.MAC)
		if err != nil {
			return err
		}
	}

	for _, c := range Concerns {
		_, err := db.ExecContext(ctx, `
			INSERT INTO concerns ("VM_ID", "Concern_ID", "Label", "Category", "Assessment")
			VALUES (?, ?, ?, 'Warning', 'Needs attention')
		`, c.VMID, c.ConcernID, c.Label)
		if err != nil {
			return err
		}
	}

	return nil
}
