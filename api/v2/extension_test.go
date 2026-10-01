package v2_test

import (
	"encoding/json"
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
	It("serializes empty source metadata as an array", func() {
		detail := v2.NewVirtualMachineDetailFromModel(models.VM{})
		raw, err := json.Marshal(detail)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(raw)).To(ContainSubstring(`"sourceMetadata":[]`))
	})

	It("preserves source facts and their origin", func() {
		detail := v2.NewVirtualMachineDetailFromModel(models.VM{SourceMetadata: []models.SourceMetadataEntry{
			{Key: "Environment", Value: "Production", Kind: "tag"},
			{Key: "Owner", Value: "Finance", Kind: "customAttribute"},
			{Key: "Team", Value: "Payments; QA", Kind: "unknown"},
		}})
		Expect(detail.SourceMetadata).NotTo(BeNil())
		entries := *detail.SourceMetadata
		Expect(entries).To(HaveLen(3))
		raw, err := json.Marshal(entries)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(raw)).To(MatchJSON(`[
			{"key":"Environment","value":"Production","kind":"tag"},
			{"key":"Owner","value":"Finance","kind":"customAttribute"},
			{"key":"Team","value":"Payments; QA","kind":"unknown"}
		]`))
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
