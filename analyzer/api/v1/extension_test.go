package v1_test

import (
	"encoding/json"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1 "github.com/kubev2v/assisted-migration-agent/analyzer/api/v1"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
)

func TestExtension(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Analyzer API Extension Suite")
}

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
