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
