package v1_test

import (
	"encoding/json"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1 "github.com/kubev2v/assisted-migration-agent/analyzer/api/v1"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
)

func TestExtension(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Analyzer API V1 Extension Suite")
}

var _ = Describe("NewVirtualMachineDetailFromModel", func() {
	It("includes a zero NIC count in JSON", func() {
		detail := v1.NewVirtualMachineDetailFromModel(models.VM{})
		raw, err := json.Marshal(detail)
		Expect(err).NotTo(HaveOccurred())
		var fields map[string]json.RawMessage
		Expect(json.Unmarshal(raw, &fields)).To(Succeed())
		Expect(string(fields["nicCount"])).To(Equal("0"))
	})

	It("preserves the source NIC count without adapter details", func() {
		detail := v1.NewVirtualMachineDetailFromModel(models.VM{NICCount: 3})
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

		detail := v1.NewVirtualMachineDetailFromModel(vm)

		Expect(detail.NicCount).To(Equal(vm.NICCount))
		Expect(detail.Nics).To(HaveLen(2))
		for i, nic := range vm.NICs {
			Expect(detail.Nics[i].Adapter).To(HaveValue(Equal(nic.Adapter)))
			Expect(detail.Nics[i].Network).To(HaveValue(Equal(nic.Network)))
			Expect(detail.Nics[i].Mac).To(HaveValue(Equal(nic.MAC)))
			Expect(detail.Nics[i].Index).To(HaveValue(Equal(nic.Index)))
		}
	})
})

var _ = DescribeTable("VM disk storage metadata", func(disk models.Disk, want string) {
	detail := v1.NewVirtualMachineDetailFromModel(models.VM{Disks: []models.Disk{disk}})
	Expect(detail.Disks).To(HaveLen(1))
	data, err := json.Marshal(detail.Disks[0])
	Expect(err).NotTo(HaveOccurred())
	Expect(string(data)).To(MatchJSON(want))
},
	Entry("preserves reported RDM and datastore metadata", models.Disk{
		Key: 2000, File: "[shared] vm/rdm.vmdk", Capacity: 1,
		Shared: true, RDM: true, Bus: "scsi", Mode: "independent_persistent",
		RDMBacking: &models.RDMBacking{LunUUID: "rdm-lun-uuid"},
		Datastore: &models.DiskDatastore{
			ID: "datastore-1", Name: "shared", Type: "VMFS",
			FilesystemType: "VMFS", FilesystemMajorVersion: 6,
			ProtocolType: "iSCSI", StorageProtocol: "Block",
			BackingDeviceIDs: []string{"naa.extent-1", "naa.extent-2"},
			Vendor:           "NETAPP", Model: "LUN C-Mode",
		},
	}, `{
		"key":2000,"file":"[shared] vm/rdm.vmdk","capacity":1048576,
		"shared":true,"rdm":true,"bus":"scsi","mode":"independent_persistent",
		"rdmBacking":{"lunUuid":"rdm-lun-uuid"},
		"datastore":{
			"id":"datastore-1","name":"shared","type":"VMFS",
			"filesystemType":"VMFS","filesystemMajorVersion":6,
			"protocolType":"iSCSI","storageProtocol":"Block",
			"backingDeviceIds":["naa.extent-1","naa.extent-2"],
			"vendor":"NETAPP","model":"LUN C-Mode"
		}
	}`),
	Entry("omits RDM backing for a normal disk and unknown datastore values", models.Disk{
		RDMBacking: &models.RDMBacking{LunUUID: "stale-uuid"},
		Datastore: &models.DiskDatastore{
			Name: "partial", FilesystemMajorVersion: -1, BackingDeviceIDs: []string{},
		},
	}, `{
		"file":"","capacity":0,"shared":false,"rdm":false,"bus":"","mode":"",
		"datastore":{"name":"partial"}
	}`),
	Entry("omits unavailable storage details from a legacy RDM disk", models.Disk{
		RDM: true,
	}, `{"file":"","capacity":0,"shared":false,"rdm":true,"bus":"","mode":""}`),
	Entry("omits an empty RDM UUID", models.Disk{
		RDM: true, RDMBacking: &models.RDMBacking{},
	}, `{"file":"","capacity":0,"shared":false,"rdm":true,"bus":"","mode":""}`),
)

var _ = Describe("Inventory schema", func() {
	It("exposes host storage in aggregate and group inventories", func() {
		spec, err := v1.GetSwagger()
		Expect(err).NotTo(HaveOccurred())
		for _, ref := range []*openapi3.SchemaRef{
			spec.Components.Schemas["InventoryResponse"].Value.Properties["data"],
			spec.Components.Schemas["GroupResponse"].Value.Properties["inventory"],
		} {
			Expect(ref.Value.Properties).To(HaveKey("vcenter"))
			host := ref.Value.Properties["vcenter"].Value.Properties["infra"].Value.Properties["hosts"].Value.Items.Value
			Expect(host.Properties).To(HaveKey("storageAdapters"))
			Expect(host.Properties).To(HaveKey("storageLuns"))
		}
	})
})
