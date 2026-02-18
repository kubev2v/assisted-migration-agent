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
	// Given a connected console status
	// When we convert it to API status
	// Then it should map to connected
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

	// Given a disconnected console status
	// When we convert it to API status
	// Then it should map to disconnected
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

	// Given a console status with an error
	// When we convert it to API status
	// Then it should include the error message
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
	// Given a VMSummary with all fields populated
	// When we convert it to API VM
	// Then it should map all fields correctly
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
		Expect(vm.Inspection.State).To(Equal(v1.VmInspectionStatusStateNotFound))
	})

	Context("IsMigratable", func() {
		// Given a VM that is migratable
		// When we convert it to API VM
		// Then Migratable should be true
		It("should set Migratable=true when VM is migratable", func() {
			summary := models.VMSummary{
				ID:           "vm-migratable",
				Name:         "Migratable VM",
				IsMigratable: true,
			}

			vm := v1.NewVMFromSummary(summary)

			Expect(vm.Migratable).NotTo(BeNil())
			Expect(*vm.Migratable).To(BeTrue())
		})

		// Given a VM that is not migratable
		// When we convert it to API VM
		// Then Migratable should be false
		It("should set Migratable=false when VM is not migratable", func() {
			summary := models.VMSummary{
				ID:           "vm-not-migratable",
				Name:         "Non-Migratable VM",
				IsMigratable: false,
			}

			vm := v1.NewVMFromSummary(summary)

			Expect(vm.Migratable).NotTo(BeNil())
			Expect(*vm.Migratable).To(BeFalse())
		})
	})

	Context("IsTemplate", func() {
		// Given a template VM
		// When we convert it to API VM
		// Then Template should be true
		It("should set Template=true when VM is a template", func() {
			summary := models.VMSummary{
				ID:         "vm-template",
				Name:       "Template VM",
				IsTemplate: true,
			}

			vm := v1.NewVMFromSummary(summary)

			Expect(vm.Template).NotTo(BeNil())
			Expect(*vm.Template).To(BeTrue())
		})

		// Given a regular VM
		// When we convert it to API VM
		// Then Template should be false
		It("should set Template=false when VM is not a template", func() {
			summary := models.VMSummary{
				ID:         "vm-regular",
				Name:       "Regular VM",
				IsTemplate: false,
			}

			vm := v1.NewVMFromSummary(summary)

			Expect(vm.Template).NotTo(BeNil())
			Expect(*vm.Template).To(BeFalse())
		})
	})
})

var _ = Describe("NewCollectorStatus", func() {
	Context("state mapping", func() {
		// Given a ready collector state
		// When we convert it to API status
		// Then it should map to ready
		It("should map ready state", func() {
			status := v1.NewCollectorStatus(models.CollectorStatus{State: models.CollectorStateReady})
			Expect(status.Status).To(Equal(v1.CollectorStatusStatusReady))
			Expect(status.Error).To(BeNil())
		})

		// Given a connecting collector state
		// When we convert it to API status
		// Then it should map to connecting
		It("should map connecting state", func() {
			status := v1.NewCollectorStatus(models.CollectorStatus{State: models.CollectorStateConnecting})
			Expect(status.Status).To(Equal(v1.CollectorStatusStatusConnecting))
		})

		// Given a connected collector state
		// When we convert it to API status
		// Then it should map to connected
		It("should map connected state", func() {
			status := v1.NewCollectorStatus(models.CollectorStatus{State: models.CollectorStateConnected})
			Expect(status.Status).To(Equal(v1.CollectorStatusStatusConnected))
		})

		// Given a collecting collector state
		// When we convert it to API status
		// Then it should map to collecting
		It("should map collecting state", func() {
			status := v1.NewCollectorStatus(models.CollectorStatus{State: models.CollectorStateCollecting})
			Expect(status.Status).To(Equal(v1.CollectorStatusStatusCollecting))
		})

		// Given a collected collector state
		// When we convert it to API status
		// Then it should map to collected
		It("should map collected state", func() {
			status := v1.NewCollectorStatus(models.CollectorStatus{State: models.CollectorStateCollected})
			Expect(status.Status).To(Equal(v1.CollectorStatusStatusCollected))
		})

		// Given an error collector state with error message
		// When we convert it to API status
		// Then it should map to error with message
		It("should map error state with message", func() {
			status := v1.NewCollectorStatus(models.CollectorStatus{
				State: models.CollectorStateError,
				Error: errors.New("connection refused"),
			})
			Expect(status.Status).To(Equal(v1.CollectorStatusStatusError))
			Expect(status.Error).NotTo(BeNil())
			Expect(*status.Error).To(Equal("connection refused"))
		})

		// Given an unknown collector state
		// When we convert it to API status
		// Then it should default to unknown state
		It("should default unknown state to unknown", func() {
			status := v1.NewCollectorStatus(models.CollectorStatus{State: "unknown"})
			Expect(string(status.Status)).To(Equal("unknown state"))
		})
	})
})

var _ = Describe("NewCollectorStatusWithError", func() {
	// Given a collector status and an error
	// When we create status with error
	// Then it should add error to status
	It("should add error to status", func() {
		status := v1.NewCollectorStatusWithError(
			models.CollectorStatus{State: models.CollectorStateReady},
			errors.New("custom error"),
		)
		Expect(status.Status).To(Equal(v1.CollectorStatusStatusReady))
		Expect(status.Error).NotTo(BeNil())
		Expect(*status.Error).To(Equal("custom error"))
	})

	// Given a collector status and nil error
	// When we create status with error
	// Then it should not add error
	It("should not add error when nil", func() {
		status := v1.NewCollectorStatusWithError(
			models.CollectorStatus{State: models.CollectorStateReady},
			nil,
		)
		Expect(status.Error).To(BeNil())
	})
})

var _ = Describe("NewVMDetailsFromModel", func() {
	Context("required fields", func() {
		// Given a VM with required fields
		// When we convert it to VMDetails
		// Then it should map all required fields
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
	})

	Context("optional string fields", func() {
		// Given a VM with optional string fields populated
		// When we convert it to VMDetails
		// Then it should include all optional fields
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

		// Given a VM with empty optional string fields
		// When we convert it to VMDetails
		// Then it should not include empty fields
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
	})

	Context("storage used", func() {
		// Given a VM with storage used greater than zero
		// When we convert it to VMDetails
		// Then it should include StorageUsed
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

		// Given a VM with zero storage used
		// When we convert it to VMDetails
		// Then it should not include StorageUsed
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
	})

	Context("boolean fields", func() {
		// Given a VM with boolean fields set
		// When we convert it to VMDetails
		// Then it should always include boolean fields
		It("should always include boolean fields", func() {
			vm := models.VM{
				ID:                    "vm-bool",
				Name:                  "Bool VM",
				PowerState:            "poweredOn",
				ConnectionState:       "connected",
				IsTemplate:            true,
				IsMigratable:          true,
				FaultToleranceEnabled: true,
				NestedHVEnabled:       false,
			}

			details := v1.NewVMDetailsFromModel(vm)

			Expect(details.Template).NotTo(BeNil())
			Expect(*details.Template).To(BeTrue())
			Expect(details.Migratable).NotTo(BeNil())
			Expect(*details.Migratable).To(BeTrue())
			Expect(details.FaultToleranceEnabled).NotTo(BeNil())
			Expect(*details.FaultToleranceEnabled).To(BeTrue())
			Expect(details.NestedHVEnabled).NotTo(BeNil())
			Expect(*details.NestedHVEnabled).To(BeFalse())
		})

		// Given a VM that is not migratable
		// When we convert it to VMDetails
		// Then Migratable should be false
		It("should set Migratable=false when VM is not migratable", func() {
			vm := models.VM{
				ID:              "vm-not-migratable",
				Name:            "Not Migratable VM",
				PowerState:      "poweredOn",
				ConnectionState: "connected",
				IsMigratable:    false,
			}

			details := v1.NewVMDetailsFromModel(vm)

			Expect(details.Migratable).NotTo(BeNil())
			Expect(*details.Migratable).To(BeFalse())
		})

		// Given a VM that is not a template
		// When we convert it to VMDetails
		// Then Template should be false
		It("should set Template=false when VM is not a template", func() {
			vm := models.VM{
				ID:              "vm-not-template",
				Name:            "Not Template VM",
				PowerState:      "poweredOn",
				ConnectionState: "connected",
				IsTemplate:      false,
			}

			details := v1.NewVMDetailsFromModel(vm)

			Expect(details.Template).NotTo(BeNil())
			Expect(*details.Template).To(BeFalse())
		})
	})

	Context("disks", func() {
		// Given a VM with disks
		// When we convert it to VMDetails
		// Then it should convert disks with capacity in bytes
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

		// Given a VM with a disk that has no key
		// When we convert it to VMDetails
		// Then it should not include disk key
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
	})

	Context("NICs", func() {
		// Given a VM with NICs
		// When we convert it to VMDetails
		// Then it should convert NICs correctly
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
	})

	Context("issues", func() {
		// Given a VM with issues
		// When we convert it to VMDetails
		// Then it should include issues
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

		// Given a VM with no issues
		// When we convert it to VMDetails
		// Then it should not include issues
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
})
