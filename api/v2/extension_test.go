package v2_test

import (
	"errors"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v2 "github.com/kubev2v/assisted-migration-agent/api/v2"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
)

func TestExtension(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "API V2 Extension Suite")
}

var _ = Describe("NewVirtualMachineDetailFromModel", func() {
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
