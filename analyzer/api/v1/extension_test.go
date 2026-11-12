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
	It("returns an empty metadata object and omits unavailable notes", func() {
		detail := v1.NewVirtualMachineDetailFromModel(models.VM{})

		raw, err := json.Marshal(detail)
		Expect(err).NotTo(HaveOccurred())
		var fields map[string]json.RawMessage
		Expect(json.Unmarshal(raw, &fields)).To(Succeed())
		Expect(string(fields["metadata"])).To(MatchJSON(`{}`))
		Expect(fields).NotTo(HaveKey("annotation"))
	})

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
		"rdmBacking":{"lunUuid":"rdm-lun-uuid","compatibilityMode":"unknown"},
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
	Entry("reports unknown mode for a domain backing without identifiers", models.Disk{
		RDM: true, RDMBacking: &models.RDMBacking{},
	}, `{
		"file":"","capacity":0,"shared":false,"rdm":true,"bus":"","mode":"",
		"rdmBacking":{"compatibilityMode":"unknown"}
	}`),
	Entry("exposes a device-only v2 backing with its mode", models.Disk{
		RDM: true,
		RDMBacking: &models.RDMBacking{
			DeviceName: "/vmfs/devices/disks/naa.v2", CompatibilityMode: "physical",
		},
	}, `{
		"file":"","capacity":0,"shared":false,"rdm":true,"bus":"","mode":"",
		"rdmBacking":{"deviceName":"/vmfs/devices/disks/naa.v2","compatibilityMode":"physical"}
	}`),
	Entry("exposes every identifier with the matched host LUN", models.Disk{
		RDM: true,
		RDMBacking: &models.RDMBacking{
			LunUUID: "lun-uuid-A", DeviceName: "/vmfs/devices/disks/naa.001",
			CompatibilityMode: "virtual", BackingUUID: "backing-uuid-A",
			HostLun: &models.RDMHostLun{
				HostID: "host-1", Key: "lun-1", CanonicalName: "naa.001", LunUUID: "lun-uuid-A",
			},
		},
	}, `{
		"file":"","capacity":0,"shared":false,"rdm":true,"bus":"","mode":"",
		"rdmBacking":{
			"lunUuid":"lun-uuid-A","deviceName":"/vmfs/devices/disks/naa.001",
			"compatibilityMode":"virtual","backingUuid":"backing-uuid-A",
			"hostLun":{"hostId":"host-1","key":"lun-1","canonicalName":"naa.001","lunUuid":"lun-uuid-A"}
		}
	}`),
	Entry("omits optional host LUN fields without reported values", models.Disk{
		RDM: true,
		RDMBacking: &models.RDMBacking{
			LunUUID: "lun-uuid-B", CompatibilityMode: "unknown",
			HostLun: &models.RDMHostLun{HostID: "host-2"},
		},
	}, `{
		"file":"","capacity":0,"shared":false,"rdm":true,"bus":"","mode":"",
		"rdmBacking":{"lunUuid":"lun-uuid-B","compatibilityMode":"unknown","hostLun":{"hostId":"host-2"}}
	}`),
	Entry("preserves known false flags and omits unknown ones", models.Disk{
		RDM:             false,
		ThinProvisioned: &[]bool{false}[0],
	}, `{
		"file":"","capacity":0,"shared":false,"rdm":false,"bus":"","mode":"",
		"thinProvisioned":false
	}`),
	Entry("preserves known true flags on an RDM disk", models.Disk{
		RDM:             true,
		ThinProvisioned: &[]bool{true}[0], EagerlyScrubbed: &[]bool{true}[0],
	}, `{
		"file":"","capacity":0,"shared":false,"rdm":true,"bus":"","mode":"",
		"thinProvisioned":true,"eagerlyScrubbed":true
	}`),
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
