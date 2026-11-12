package v2_test

import (
	"encoding/json"
	"errors"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v2 "github.com/kubev2v/assisted-migration-agent/agent/api/v2"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
)

func TestExtension(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "API V2 Extension Suite")
}

var _ = Describe("NewVirtualMachineDetailFromModel", func() {
	It("includes a zero NIC count in JSON", func() {
		detail := v2.NewVirtualMachineDetailFromModel(models.VM{})
		raw, err := json.Marshal(detail)
		Expect(err).NotTo(HaveOccurred())
		var fields map[string]json.RawMessage
		Expect(json.Unmarshal(raw, &fields)).To(Succeed())
		Expect(string(fields["nicCount"])).To(Equal("0"))
	})

	It("preserves the source NIC count without adapter details", func() {
		detail := v2.NewVirtualMachineDetailFromModel(models.VM{NICCount: 3})
		Expect(detail.NicCount).To(Equal(int32(3)))
		Expect(detail.Nics).To(BeEmpty())
	})

	It("preserves multiple adapters and their network fields", func() {
		vm := models.VM{
			NICCount: 2,
			NICs: []models.NIC{
				{Adapter: "Vmxnet3", Network: "Production", MAC: "00:50:56:AA:BB:01", Index: 0},
				{Adapter: "E1000", Network: "Backup", MAC: "00:50:56:AA:BB:02", Index: 1},
			},
		}

		detail := v2.NewVirtualMachineDetailFromModel(vm)

		Expect(detail.NicCount).To(Equal(vm.NICCount))
		Expect(detail.Nics).To(HaveLen(2))
		for i, nic := range vm.NICs {
			Expect(detail.Nics[i].Adapter).To(HaveValue(Equal(nic.Adapter)))
			Expect(detail.Nics[i].Network).To(HaveValue(Equal(nic.Network)))
			Expect(detail.Nics[i].Mac).To(HaveValue(Equal(nic.MAC)))
			Expect(detail.Nics[i].Index).To(HaveValue(Equal(nic.Index)))
		}
	})

	DescribeTable("serializes an optional annotation using the singular JSON field", func(annotation string) {
		detail := v2.NewVirtualMachineDetailFromModel(models.VM{Annotation: annotation})
		raw, err := json.Marshal(detail)
		Expect(err).NotTo(HaveOccurred())
		var fields map[string]any
		Expect(json.Unmarshal(raw, &fields)).To(Succeed())
		Expect(fields).NotTo(HaveKey("annotations"))
		if annotation == "" {
			Expect(fields).NotTo(HaveKey("annotation"))
		} else {
			Expect(fields).To(HaveKeyWithValue("annotation", annotation))
		}
	},
		Entry("reported annotation", "Database VM notes"),
		Entry("unavailable annotation", ""),
	)

	It("serializes empty metadata as an object", func() {
		detail := v2.NewVirtualMachineDetailFromModel(models.VM{})
		raw, err := json.Marshal(detail)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(raw)).To(ContainSubstring(`"metadata":{}`))
	})

	It("preserves metadata keys and values", func() {
		detail := v2.NewVirtualMachineDetailFromModel(models.VM{Metadata: map[string][]string{
			"Environment": {"Production", "QA"}, "Owner": {"Finance"}, "Team": {"Payments; QA"},
		}})
		Expect(detail.Metadata).NotTo(BeNil())
		entries := *detail.Metadata
		Expect(entries).To(HaveLen(3))
		raw, err := json.Marshal(entries)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(raw)).To(MatchJSON(`{"Environment":["Production","QA"],"Owner":["Finance"],"Team":["Payments; QA"]}`))
		Expect(detail.Labels).To(BeNil())
	})
	It("should include inspectionStatus when state is not not_started", func() {
		vm := models.VM{
			ID:              "vm-1",
			Name:            "test-vm",
			PowerState:      "poweredOn",
			ConnectionState: "connected",
			CpuCount:        2,
			CoresPerSocket:  1,
			MemoryMB:        4096,
			InspectionStatus: models.InspectionStatus{
				State:   models.InspectionStateError,
				Details: "VDDK connection failed",
				Error:   errors.New("nbdkit-vddk-plugin: Unknown error"),
			},
		}

		detail := v2.NewVirtualMachineDetailFromModel(vm)

		Expect(detail.InspectionStatus).NotTo(BeNil())
		Expect(detail.InspectionStatus.State).To(Equal(v2.InspectionStatusStateError))
		Expect(detail.InspectionStatus.Details).NotTo(BeNil())
		Expect(*detail.InspectionStatus.Details).To(Equal("VDDK connection failed"))
		Expect(detail.InspectionStatus.Error).NotTo(BeNil())
		Expect(*detail.InspectionStatus.Error).To(Equal("nbdkit-vddk-plugin: Unknown error"))
	})

	It("should omit inspectionStatus when state is not_started", func() {
		vm := models.VM{
			ID:              "vm-2",
			Name:            "clean-vm",
			PowerState:      "poweredOn",
			ConnectionState: "connected",
			CpuCount:        2,
			CoresPerSocket:  1,
			MemoryMB:        4096,
			InspectionStatus: models.InspectionStatus{
				State: models.InspectionStateNotStarted,
			},
		}

		detail := v2.NewVirtualMachineDetailFromModel(vm)

		Expect(detail.InspectionStatus).To(BeNil())
	})
})

var _ = Describe("Inventory schema", func() {
	It("exposes host storage adapters and LUNs on inventory and group responses", func() {
		spec, err := v2.GetSwagger()
		Expect(err).NotTo(HaveOccurred())
		hosts := spec.Paths.Value("/inventory").Get.Responses.Value("200").Value.Content["application/json"].Schema.Value.Properties["inventory"].Value.Properties["inventory"].Value.Properties["vcenter"].Value.Properties["infra"].Value.Properties["hosts"].Value.Items.Value
		Expect(hosts.Properties).To(HaveKey("storageAdapters"))
		Expect(hosts.Properties).To(HaveKey("storageLuns"))
		Expect(hosts.Properties["storageAdapters"].Value.Items.Value.Properties).To(HaveKey("nodeWwn"))
		Expect(hosts.Properties["storageLuns"].Value.Items.Value.Properties).To(HaveKey("adapterDevices"))
		groupHosts := spec.Components.Schemas["GroupResponse"].Value.Properties["inventory"].Value.Properties["vcenter"].Value.Properties["infra"].Value.Properties["hosts"].Value.Items.Value
		Expect(groupHosts.Properties).To(HaveKey("storageAdapters"))
		Expect(groupHosts.Properties).To(HaveKey("storageLuns"))
	})
})

var _ = Describe("NewApplianceList", func() {
	It("should map appliance fields and count its VMs", func() {
		// Arrange
		appliances := []models.Appliance{{
			Name:       "DataSync",
			Vendor:     "AWS",
			Version:    "2.0",
			Confidence: 100,
			VirtualMachines: models.VirtualMachinesIdentites{
				"vm-1": "web-server",
				"vm-2": "db-server",
			},
		}}

		// Act
		result := v2.NewApplianceList(appliances)

		// Assert
		Expect(result).To(HaveLen(1))
		Expect(result[0].Name).To(Equal("DataSync"))
		Expect(result[0].Vendor).To(Equal("AWS"))
		Expect(result[0].Version).To(Equal("2.0"))
		Expect(result[0].Confidence).To(Equal(100))
		Expect(result[0].VmCount).To(Equal(2))
		Expect(result[0].Vms).To(ConsistOf(
			v2.VirtualMachineIdentity{Id: "vm-1", Name: "web-server"},
			v2.VirtualMachineIdentity{Id: "vm-2", Name: "db-server"},
		))
	})

	It("should return a non-nil empty list", func() {
		// Act
		result := v2.NewApplianceList(nil)

		// Assert
		Expect(result).ToNot(BeNil())
		Expect(result).To(BeEmpty())

		raw, err := json.Marshal(v2.ApplianceListResponse{Appliances: result})
		Expect(err).NotTo(HaveOccurred())
		Expect(string(raw)).To(Equal(`{"appliances":[]}`))
	})
})
