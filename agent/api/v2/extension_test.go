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

var _ = DescribeTable("VM disk storage metadata", func(disk models.Disk, want string) {
	detail := v2.NewVirtualMachineDetailFromModel(models.VM{Disks: []models.Disk{disk}})
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

// ptr returns a pointer to value, for the optional API fields.
func ptr[T any](value T) *T {
	return &value
}

var _ = Describe("NewApplianceRuleFromModel", func() {
	// Given a rule whose only condition is an expression
	// When it is converted for the API
	// Then the rule fields and the expression condition are carried over
	It("should map a rule with an expression condition", func() {
		// Arrange
		rule := models.Rule{
			ID:         7,
			Name:       "oracle-oda",
			Vendor:     "Oracle",
			RuleKind:   models.CustomRule,
			Category:   "Database",
			OsvVerdict: models.OsvConditional,
			Confidence: models.ConfidenceMediumLevel,
			MinMatches: 2,
			Conditions: []any{
				models.ExpressionCondition{Name: "annotation", Tier: 2, Expression: `Name ~ /^ORA-/`},
			},
		}

		// Act
		apiRule, err := v2.NewApplianceRuleFromModel(rule)

		// Assert
		Expect(err).NotTo(HaveOccurred())
		Expect(apiRule.Id).To(HaveValue(Equal("7")))
		Expect(apiRule.Name).To(Equal("oracle-oda"))
		Expect(apiRule.Vendor).To(Equal("Oracle"))
		Expect(apiRule.Category).To(Equal("Database"))
		Expect(apiRule.Kind).To(Equal(v2.Custom))
		Expect(apiRule.OsvVerdict).To(Equal(v2.ApplianceRuleOsvVerdictConditional))
		Expect(apiRule.MaxConfidence).To(Equal(v2.ApplianceRuleMaxConfidenceMedium))
		Expect(apiRule.MinMatched).To(Equal(json.Number("2")))
		Expect(apiRule.Conditions).To(Equal([]v2.ApplianceRuleCondition{
			{Name: "annotation", Expression: ptr(`Name ~ /^ORA-/`)},
		}))
	})

	// Given a rule whose only condition is a disk geometry predicate
	// When it is converted for the API
	// Then the wire shape carries the predicate name, the disks, the mode and the tolerance
	It("should map a disk geometry condition with its mode and tolerance", func() {
		// Arrange
		rule := models.Rule{
			ID:         8,
			Name:       "disk-rule",
			Confidence: models.ConfidenceHighLevel,
			MinMatches: 1,
			Conditions: []any{
				models.FuncCondition[models.DiskGeometry]{
					Name:     "disk_geometry",
					Tier:     5,
					FuncName: "disk_geometry_superset",
					Data:     models.DiskGeometry{Disks: []int64{2048, 4096}, Tolerance: 512, Mode: models.DiskGeometrySuperset},
				},
			},
		}

		// Act
		apiRule, err := v2.NewApplianceRuleFromModel(rule)

		// Assert
		Expect(err).NotTo(HaveOccurred())
		Expect(apiRule.Conditions).To(HaveLen(1))
		Expect(apiRule.Conditions[0].Name).To(Equal("disk_geometry"))
		Expect(apiRule.Conditions[0].Function).To(Equal(&v2.ApplianceRuleFunction{
			Name: "disk_geometry",
			DiskGeometry: &v2.DiskGeometryCondition{
				Disks:        []json.Number{"2048", "4096"},
				Mode:         v2.Superset,
				ToleranceMib: json.Number("512"),
			},
		}))
	})

	// Given a rule that holds a condition the API has no shape for
	// When it is converted for the API
	// Then the conversion fails instead of dropping the condition
	It("should fail on a condition the API cannot express", func() {
		// Arrange
		rule := models.Rule{
			Name:       "unexpressible",
			Conditions: []any{models.ExpressionCondition{Name: "name", Tier: 6, Expression: `Name = "web-01"`}, "not a condition"},
		}

		// Act
		_, err := v2.NewApplianceRuleFromModel(rule)

		// Assert
		Expect(err).To(MatchError(ContainSubstring("condition[1]")))
		Expect(err).To(MatchError(ContainSubstring("cannot express")))
	})
})

var _ = DescribeTable("NewApplianceRuleFromModel OSV verdict",
	func(verdict models.OsvVerdict, want v2.ApplianceRuleOsvVerdict) {
		// Arrange
		rule := models.Rule{
			Name:       "rule",
			OsvVerdict: verdict,
			Conditions: []any{models.ExpressionCondition{Name: "name", Tier: 6, Expression: `Name = "web-01"`}},
		}

		// Act
		apiRule, err := v2.NewApplianceRuleFromModel(rule)

		// Assert
		Expect(err).NotTo(HaveOccurred())
		Expect(apiRule.OsvVerdict).To(Equal(want))
	},
	Entry("blocked", models.OsvBlocked, v2.ApplianceRuleOsvVerdictBlocked),
	Entry("conditional", models.OsvConditional, v2.ApplianceRuleOsvVerdictConditional),
	Entry("supported", models.OsvSupported, v2.ApplianceRuleOsvVerdictSupported),
	Entry("unknown", models.OsvUnknown, v2.ApplianceRuleOsvVerdictUnknown),
)

var _ = DescribeTable("NewApplianceRuleFromModel confidence",
	func(confidence models.ConfidenceLevel, want v2.ApplianceRuleMaxConfidence) {
		// Arrange
		rule := models.Rule{
			Name:       "rule",
			Confidence: confidence,
			Conditions: []any{models.ExpressionCondition{Name: "name", Tier: 6, Expression: `Name = "web-01"`}},
		}

		// Act
		apiRule, err := v2.NewApplianceRuleFromModel(rule)

		// Assert
		Expect(err).NotTo(HaveOccurred())
		Expect(apiRule.MaxConfidence).To(Equal(want))
	},
	Entry("high", models.ConfidenceHighLevel, v2.ApplianceRuleMaxConfidenceHigh),
	Entry("medium", models.ConfidenceMediumLevel, v2.ApplianceRuleMaxConfidenceMedium),
	Entry("low", models.ConfidenceLowerLevel, v2.ApplianceRuleMaxConfidenceLow),
)

var _ = DescribeTable("NewApplianceRuleFromModel kind",
	func(kind models.RuleKind, want v2.ApplianceRuleKind) {
		// Arrange
		rule := models.Rule{
			Name:       "rule",
			RuleKind:   kind,
			Conditions: []any{models.ExpressionCondition{Name: "name", Tier: 6, Expression: `Name = "web-01"`}},
		}

		// Act
		apiRule, err := v2.NewApplianceRuleFromModel(rule)

		// Assert
		Expect(err).NotTo(HaveOccurred())
		Expect(apiRule.Kind).To(Equal(want))
	},
	Entry("default", models.DefaultRule, v2.Default),
	Entry("custom", models.CustomRule, v2.Custom),
)

var _ = Describe("NewRuleFromRequest", func() {
	// Given a request carrying an expression condition and a disk geometry condition
	// When it is converted into a domain rule
	// Then each condition becomes input the service can resolve, tolerance included
	It("should map both condition shapes", func() {
		// Arrange
		req := v2.ApplianceRuleRequest{
			Name:          "ora",
			Vendor:        "Oracle",
			Category:      "Database",
			OsvVerdict:    v2.ApplianceRuleRequestOsvVerdictSupported,
			MaxConfidence: v2.ApplianceRuleRequestMaxConfidenceHigh,
			MinMatched:    json.Number("1"),
			Conditions: []v2.ApplianceRuleCondition{
				{Name: "annotation", Expression: ptr(`Name ~ /^ORA-/`)},
				{
					Name: "disk_geometry",
					Function: &v2.ApplianceRuleFunction{
						Name: "disk_geometry",
						DiskGeometry: &v2.DiskGeometryCondition{
							Disks:        []json.Number{"2048", "4096"},
							Mode:         v2.Superset,
							ToleranceMib: json.Number("512"),
						},
					},
				},
			},
		}

		// Act
		rule, err := v2.NewRuleFromRequest(req)

		// Assert
		Expect(err).NotTo(HaveOccurred())
		Expect(rule.Name).To(Equal("ora"))
		Expect(rule.Vendor).To(Equal("Oracle"))
		Expect(rule.Category).To(Equal("Database"))
		Expect(rule.OsvVerdict).To(Equal(models.OsvSupported))
		Expect(rule.Confidence).To(Equal(models.ConfidenceHighLevel))
		Expect(rule.MinMatches).To(Equal(1))
		Expect(rule.Conditions).To(Equal([]any{
			models.ConditionInput{Name: "annotation", Expression: `Name ~ /^ORA-/`},
			models.ConditionInput{
				Name: "disk_geometry",
				Function: &models.FunctionInput{
					Name:         "disk_geometry",
					DiskGeometry: &models.DiskGeometry{Disks: []int64{2048, 4096}, Tolerance: 512, Mode: models.DiskGeometrySuperset},
				},
			},
		}))
	})

	// Given a request whose disk geometry carries a size that is not a number
	// When it is converted into a domain rule
	// Then the conversion fails
	It("should fail on a disk size that is not a number", func() {
		// Arrange
		req := v2.ApplianceRuleRequest{
			Name:       "ora",
			MinMatched: json.Number("1"),
			Conditions: []v2.ApplianceRuleCondition{{
				Name: "disk_geometry",
				Function: &v2.ApplianceRuleFunction{
					Name: "disk_geometry",
					DiskGeometry: &v2.DiskGeometryCondition{
						Disks:        []json.Number{"big"},
						Mode:         v2.Exact,
						ToleranceMib: json.Number("0"),
					},
				},
			}},
		}

		// Act
		_, err := v2.NewRuleFromRequest(req)

		// Assert
		Expect(err).To(MatchError(ContainSubstring("disk size must be an integer")))
	})

	// Given a request whose disk geometry carries a tolerance that is not a number
	// When it is converted into a domain rule
	// Then the conversion fails
	It("should fail on a tolerance that is not a number", func() {
		// Arrange
		req := v2.ApplianceRuleRequest{
			Name:       "ora",
			MinMatched: json.Number("1"),
			Conditions: []v2.ApplianceRuleCondition{{
				Name: "disk_geometry",
				Function: &v2.ApplianceRuleFunction{
					Name: "disk_geometry",
					DiskGeometry: &v2.DiskGeometryCondition{
						Disks:        []json.Number{"2048"},
						Mode:         v2.Exact,
						ToleranceMib: json.Number("lots"),
					},
				},
			}},
		}

		// Act
		_, err := v2.NewRuleFromRequest(req)

		// Assert
		Expect(err).To(MatchError(ContainSubstring("tolerance_mib must be an integer")))
	})
})
