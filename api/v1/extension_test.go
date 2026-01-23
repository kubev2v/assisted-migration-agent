package v1_test

import (
	"errors"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1 "github.com/kubev2v/assisted-migration-agent/api/v1"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
)

func TestExtension(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "API V1 Extension Suite")
}

var _ = Describe("AgentStatus.FromModel", func() {
	It("should map connected status", func() {
		model := models.AgentStatus{
			Console: models.ConsoleStatus{
				Current: models.ConsoleStatusConnected,
				Target:  models.ConsoleStatusConnected,
			},
		}

		var status v1.AgentStatus
		status.FromModel(model)

		Expect(status.ConsoleConnection).To(Equal(v1.AgentStatusConsoleConnectionConnected))
		Expect(status.Mode).To(Equal(v1.AgentStatusModeConnected))
		Expect(status.Error).To(BeNil())
	})

	It("should map disconnected status", func() {
		model := models.AgentStatus{
			Console: models.ConsoleStatus{
				Current: models.ConsoleStatusDisconnected,
				Target:  models.ConsoleStatusDisconnected,
			},
		}

		var status v1.AgentStatus
		status.FromModel(model)

		Expect(status.ConsoleConnection).To(Equal(v1.AgentStatusConsoleConnectionDisconnected))
		Expect(status.Mode).To(Equal(v1.AgentStatusModeDisconnected))
	})

	It("should include error when present", func() {
		model := models.AgentStatus{
			Console: models.ConsoleStatus{
				Current: models.ConsoleStatusDisconnected,
				Target:  models.ConsoleStatusConnected,
				Error:   errors.New("connection failed"),
			},
		}

		var status v1.AgentStatus
		status.FromModel(model)

		Expect(status.Error).NotTo(BeNil())
		Expect(*status.Error).To(Equal("connection failed"))
	})
})

var _ = Describe("NewVMFromSummary", func() {
	It("should convert VMSummary to VM", func() {
		summary := models.VMSummary{
			ID:         "vm-123",
			Name:       "Test VM",
			PowerState: "poweredOn",
			Cluster:    "cluster-1",
			Memory:     4096,
			DiskSize:   102400,
			IssueCount: 3,
		}

		vm := v1.NewVMFromSummary(summary)

		Expect(vm.Id).To(Equal("vm-123"))
		Expect(vm.Name).To(Equal("Test VM"))
		Expect(vm.VCenterState).To(Equal("poweredOn"))
		Expect(vm.Cluster).To(Equal("cluster-1"))
		Expect(vm.Memory).To(Equal(int64(4096)))
		Expect(vm.DiskSize).To(Equal(int64(102400)))
		Expect(vm.IssueCount).To(Equal(3))
		Expect(vm.Inspection.State).To(Equal(v1.InspectionStatusStatePending))
	})
})

var _ = Describe("NewCollectorStatus", func() {
	It("should map ready state", func() {
		status := v1.NewCollectorStatus(models.CollectorStatus{State: models.CollectorStateReady})
		Expect(status.Status).To(Equal(v1.CollectorStatusStatusReady))
		Expect(status.Error).To(BeNil())
	})

	It("should map connecting state", func() {
		status := v1.NewCollectorStatus(models.CollectorStatus{State: models.CollectorStateConnecting})
		Expect(status.Status).To(Equal(v1.CollectorStatusStatusConnecting))
	})

	It("should map connected state", func() {
		status := v1.NewCollectorStatus(models.CollectorStatus{State: models.CollectorStateConnected})
		Expect(status.Status).To(Equal(v1.CollectorStatusStatusConnected))
	})

	It("should map collecting state", func() {
		status := v1.NewCollectorStatus(models.CollectorStatus{State: models.CollectorStateCollecting})
		Expect(status.Status).To(Equal(v1.CollectorStatusStatusCollecting))
	})

	It("should map collected state", func() {
		status := v1.NewCollectorStatus(models.CollectorStatus{State: models.CollectorStateCollected})
		Expect(status.Status).To(Equal(v1.CollectorStatusStatusCollected))
	})

	It("should map error state with message", func() {
		status := v1.NewCollectorStatus(models.CollectorStatus{
			State: models.CollectorStateError,
			Error: errors.New("connection refused"),
		})
		Expect(status.Status).To(Equal(v1.CollectorStatusStatusError))
		Expect(status.Error).NotTo(BeNil())
		Expect(*status.Error).To(Equal("connection refused"))
	})

	It("should default unknown state to ready", func() {
		status := v1.NewCollectorStatus(models.CollectorStatus{State: "unknown"})
		Expect(status.Status).To(Equal(v1.CollectorStatusStatusReady))
	})
})

var _ = Describe("NewCollectorStatusWithError", func() {
	It("should add error to status", func() {
		status := v1.NewCollectorStatusWithError(
			models.CollectorStatus{State: models.CollectorStateReady},
			errors.New("custom error"),
		)
		Expect(status.Status).To(Equal(v1.CollectorStatusStatusReady))
		Expect(status.Error).NotTo(BeNil())
		Expect(*status.Error).To(Equal("custom error"))
	})

	It("should not add error when nil", func() {
		status := v1.NewCollectorStatusWithError(
			models.CollectorStatus{State: models.CollectorStateReady},
			nil,
		)
		Expect(status.Error).To(BeNil())
	})
})

var _ = Describe("NewVMDetailsFromModel", func() {
	It("should convert required fields", func() {
		vm := models.VM{
			ID:              "vm-456",
			Name:            "Production VM",
			PowerState:      "poweredOn",
			ConnectionState: "connected",
			CpuCount:        8,
			CoresPerSocket:  4,
			MemoryMB:        16384,
		}

		details := v1.NewVMDetailsFromModel(vm)

		Expect(details.Id).To(Equal("vm-456"))
		Expect(details.Name).To(Equal("Production VM"))
		Expect(details.PowerState).To(Equal("poweredOn"))
		Expect(details.ConnectionState).To(Equal("connected"))
		Expect(details.CpuCount).To(Equal(int32(8)))
		Expect(details.CoresPerSocket).To(Equal(int32(4)))
		Expect(details.MemoryMB).To(Equal(int32(16384)))
	})

	It("should convert optional string fields when present", func() {
		vm := models.VM{
			ID:                 "vm-789",
			Name:               "Test",
			PowerState:         "poweredOff",
			ConnectionState:    "connected",
			UUID:               "550e8400-e29b-41d4-a716-446655440000",
			Firmware:           "efi",
			Host:               "esxi-01.local",
			Datacenter:         "DC1",
			Cluster:            "Cluster1",
			Folder:             "/vms/production",
			GuestName:          "Red Hat Enterprise Linux 8",
			GuestID:            "rhel8_64Guest",
			HostName:           "server01",
			IPAddress:          "192.168.1.100",
			ToolsStatus:        "toolsOk",
			ToolsRunningStatus: "guestToolsRunning",
		}

		details := v1.NewVMDetailsFromModel(vm)

		Expect(details.Uuid).NotTo(BeNil())
		Expect(*details.Uuid).To(Equal("550e8400-e29b-41d4-a716-446655440000"))
		Expect(*details.Firmware).To(Equal("efi"))
		Expect(*details.Host).To(Equal("esxi-01.local"))
		Expect(*details.Datacenter).To(Equal("DC1"))
		Expect(*details.Cluster).To(Equal("Cluster1"))
		Expect(*details.Folder).To(Equal("/vms/production"))
		Expect(*details.GuestName).To(Equal("Red Hat Enterprise Linux 8"))
		Expect(*details.GuestId).To(Equal("rhel8_64Guest"))
		Expect(*details.HostName).To(Equal("server01"))
		Expect(*details.IpAddress).To(Equal("192.168.1.100"))
		Expect(*details.ToolsStatus).To(Equal("toolsOk"))
		Expect(*details.ToolsRunningStatus).To(Equal("guestToolsRunning"))
	})

	It("should not include optional string fields when empty", func() {
		vm := models.VM{
			ID:              "vm-empty",
			Name:            "Empty VM",
			PowerState:      "poweredOff",
			ConnectionState: "connected",
		}

		details := v1.NewVMDetailsFromModel(vm)

		Expect(details.Uuid).To(BeNil())
		Expect(details.Firmware).To(BeNil())
		Expect(details.Host).To(BeNil())
		Expect(details.Datacenter).To(BeNil())
		Expect(details.Cluster).To(BeNil())
		Expect(details.Folder).To(BeNil())
		Expect(details.GuestName).To(BeNil())
		Expect(details.GuestId).To(BeNil())
		Expect(details.HostName).To(BeNil())
		Expect(details.IpAddress).To(BeNil())
		Expect(details.ToolsStatus).To(BeNil())
		Expect(details.ToolsRunningStatus).To(BeNil())
	})

	It("should include StorageUsed when greater than zero", func() {
		vm := models.VM{
			ID:              "vm-storage",
			Name:            "Storage VM",
			PowerState:      "poweredOn",
			ConnectionState: "connected",
			StorageUsed:     1073741824,
		}

		details := v1.NewVMDetailsFromModel(vm)

		Expect(details.StorageUsed).NotTo(BeNil())
		Expect(*details.StorageUsed).To(Equal(int64(1073741824)))
	})

	It("should not include StorageUsed when zero", func() {
		vm := models.VM{
			ID:              "vm-no-storage",
			Name:            "No Storage VM",
			PowerState:      "poweredOn",
			ConnectionState: "connected",
			StorageUsed:     0,
		}

		details := v1.NewVMDetailsFromModel(vm)

		Expect(details.StorageUsed).To(BeNil())
	})

	It("should always include boolean fields", func() {
		vm := models.VM{
			ID:                    "vm-bool",
			Name:                  "Bool VM",
			PowerState:            "poweredOn",
			ConnectionState:       "connected",
			IsTemplate:            true,
			FaultToleranceEnabled: true,
			NestedHVEnabled:       false,
		}

		details := v1.NewVMDetailsFromModel(vm)

		Expect(details.IsTemplate).NotTo(BeNil())
		Expect(*details.IsTemplate).To(BeTrue())
		Expect(details.FaultToleranceEnabled).NotTo(BeNil())
		Expect(*details.FaultToleranceEnabled).To(BeTrue())
		Expect(details.NestedHVEnabled).NotTo(BeNil())
		Expect(*details.NestedHVEnabled).To(BeFalse())
	})

	It("should convert disks with capacity in bytes", func() {
		vm := models.VM{
			ID:              "vm-disks",
			Name:            "Disk VM",
			PowerState:      "poweredOn",
			ConnectionState: "connected",
			Disks: []models.Disk{
				{
					Key:      2000,
					File:     "[datastore1] vm/disk1.vmdk",
					Capacity: 100, // 100 MiB
					Shared:   false,
					RDM:      false,
					Bus:      "scsi",
					Mode:     "persistent",
				},
				{
					Key:      2001,
					File:     "[datastore1] vm/disk2.vmdk",
					Capacity: 200, // 200 MiB
					Shared:   true,
					RDM:      true,
					Bus:      "nvme",
					Mode:     "independent_persistent",
				},
			},
		}

		details := v1.NewVMDetailsFromModel(vm)

		Expect(details.Disks).To(HaveLen(2))

		disk1 := details.Disks[0]
		Expect(*disk1.Key).To(Equal(int32(2000)))
		Expect(*disk1.File).To(Equal("[datastore1] vm/disk1.vmdk"))
		Expect(*disk1.Capacity).To(Equal(int64(100 * 1024 * 1024))) // MiB to bytes
		Expect(*disk1.Shared).To(BeFalse())
		Expect(*disk1.Rdm).To(BeFalse())
		Expect(*disk1.Bus).To(Equal("scsi"))
		Expect(*disk1.Mode).To(Equal("persistent"))

		disk2 := details.Disks[1]
		Expect(*disk2.Capacity).To(Equal(int64(200 * 1024 * 1024)))
		Expect(*disk2.Shared).To(BeTrue())
		Expect(*disk2.Rdm).To(BeTrue())
	})

	It("should not include disk key when zero", func() {
		vm := models.VM{
			ID:              "vm-disk-no-key",
			Name:            "No Key VM",
			PowerState:      "poweredOn",
			ConnectionState: "connected",
			Disks: []models.Disk{
				{Key: 0, File: "disk.vmdk", Capacity: 50},
			},
		}

		details := v1.NewVMDetailsFromModel(vm)

		Expect(details.Disks).To(HaveLen(1))
		Expect(details.Disks[0].Key).To(BeNil())
	})

	It("should convert NICs", func() {
		vm := models.VM{
			ID:              "vm-nics",
			Name:            "NIC VM",
			PowerState:      "poweredOn",
			ConnectionState: "connected",
			NICs: []models.NIC{
				{MAC: "00:50:56:01:02:03", Network: "VM Network", Index: 0},
				{MAC: "00:50:56:04:05:06", Network: "Production", Index: 1},
			},
		}

		details := v1.NewVMDetailsFromModel(vm)

		Expect(details.Nics).To(HaveLen(2))

		nic1 := details.Nics[0]
		Expect(*nic1.Mac).To(Equal("00:50:56:01:02:03"))
		Expect(*nic1.Network).To(Equal("VM Network"))
		Expect(*nic1.Index).To(Equal(0))

		nic2 := details.Nics[1]
		Expect(*nic2.Mac).To(Equal("00:50:56:04:05:06"))
		Expect(*nic2.Network).To(Equal("Production"))
		Expect(*nic2.Index).To(Equal(1))
	})

	It("should include issues when present", func() {
		vm := models.VM{
			ID:              "vm-issues",
			Name:            "Issues VM",
			PowerState:      "poweredOn",
			ConnectionState: "connected",
			Issues:          []string{"ISSUE_001", "ISSUE_002", "ISSUE_003"},
		}

		details := v1.NewVMDetailsFromModel(vm)

		Expect(details.Issues).NotTo(BeNil())
		Expect(*details.Issues).To(HaveLen(3))
		Expect(*details.Issues).To(ContainElements("ISSUE_001", "ISSUE_002", "ISSUE_003"))
	})

	It("should not include issues when empty", func() {
		vm := models.VM{
			ID:              "vm-no-issues",
			Name:            "No Issues VM",
			PowerState:      "poweredOn",
			ConnectionState: "connected",
			Issues:          []string{},
		}

		details := v1.NewVMDetailsFromModel(vm)

		Expect(details.Issues).To(BeNil())
	})
})
