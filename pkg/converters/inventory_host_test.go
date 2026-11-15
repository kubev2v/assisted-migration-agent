package converters_test

import (
	"encoding/json"

	"github.com/getkin/kin-openapi/openapi3"
	api "github.com/kubev2v/migration-planner/api/v1alpha1"
	"github.com/kubev2v/migration-planner/pkg/inventory"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	agentapi "github.com/kubev2v/assisted-migration-agent/agent/api/v2"
	analyzerapi "github.com/kubev2v/assisted-migration-agent/analyzer/api/v1"
	"github.com/kubev2v/assisted-migration-agent/pkg/converters"
)

var _ = Describe("host inventory conversion", func() {
	DescribeTable("preserves optional host source facts", func(host inventory.Host, want map[string]string) {
		data := inventory.InventoryData{Infra: inventory.InfraData{Hosts: []inventory.Host{host}, TotalHosts: 1}}
		parsed := &inventory.Inventory{
			VCenter:  &data,
			Clusters: map[string]inventory.InventoryData{"cluster-id": data},
		}

		result := converters.ToAPI(parsed)
		Expect(result.Vcenter).NotTo(BeNil())
		Expect(result.Vcenter.Infra.Hosts).NotTo(BeNil())
		Expect(result.Clusters["cluster-id"].Infra.Hosts).NotTo(BeNil())

		for _, hosts := range [][]api.Host{*result.Vcenter.Infra.Hosts, *result.Clusters["cluster-id"].Infra.Hosts} {
			Expect(hosts).To(HaveLen(1))
			got := hosts[0]
			fields := map[string]*string{
				"datacenter": got.Datacenter, "cluster": got.Cluster,
				"cpuModel": got.CpuModel, "serialNumber": got.SerialNumber,
			}
			for field, expected := range want {
				Expect(fields[field]).NotTo(BeNil(), field)
				Expect(*fields[field]).To(Equal(expected), field)
			}
			if len(want) == 0 {
				for _, value := range fields {
					Expect(value).To(BeNil())
				}
			}

			encoded, err := json.Marshal(got)
			Expect(err).NotTo(HaveOccurred())
			var wire map[string]any
			Expect(json.Unmarshal(encoded, &wire)).To(Succeed())
			Expect(wire).NotTo(HaveKey("name"))
			if len(want) == 0 {
				Expect(wire).NotTo(HaveKey("serialNumber"))
			} else {
				Expect(wire["serialNumber"]).To(Equal(want["serialNumber"]))
			}
		}
	},
		Entry("known host facts", inventory.Host{
			Datacenter: "dc-1", Cluster: "cluster-1",
			CpuModel: "Xeon Gold 6338", SerialNumber: "SN-1",
		}, map[string]string{
			"datacenter": "dc-1", "cluster": "cluster-1",
			"cpuModel": "Xeon Gold 6338", "serialNumber": "SN-1",
		}),
		Entry("unknown source facts", inventory.Host{}, map[string]string{}),
	)
})

var _ = Describe("datastore storage array conversion", func() {
	DescribeTable("preserves source array identity separately from device inquiry facts", func(known bool) {
		datastore := inventory.Datastore{DiskId: "vvol", Vendor: "LUN vendor", Model: "LUN model", BackingDeviceIDs: []string{"naa.device"}}
		if known {
			datastore.StorageArrays = []inventory.StorageArray{{UUID: "array-1", Name: "Main", VendorID: "vendor", ModelID: "model"}, {Name: "partial"}}
			datastore.StorageContainerID = "container-1"
		}
		data := inventory.InventoryData{Infra: inventory.InfraData{Datastores: []inventory.Datastore{datastore}}}
		result := converters.ToAPI(&inventory.Inventory{VCenter: &data, Clusters: map[string]inventory.InventoryData{"cluster": data}})
		encoded, err := json.Marshal(result)
		Expect(err).NotTo(HaveOccurred())
		var wire struct {
			VCenter struct {
				Infra struct {
					Datastores []map[string]any `json:"datastores"`
				} `json:"infra"`
			} `json:"vcenter"`
			Clusters map[string]struct {
				Infra struct {
					Datastores []map[string]any `json:"datastores"`
				} `json:"infra"`
			} `json:"clusters"`
		}
		Expect(json.Unmarshal(encoded, &wire)).To(Succeed())
		for _, datastores := range [][]map[string]any{wire.VCenter.Infra.Datastores, wire.Clusters["cluster"].Infra.Datastores} {
			Expect(datastores).To(HaveLen(1))
			got := datastores[0]
			Expect(got["vendor"]).To(Equal("LUN vendor"))
			Expect(got["model"]).To(Equal("LUN model"))
			Expect(got["backingDeviceIds"]).To(Equal([]any{"naa.device"}))
			if known {
				Expect(got["storageArrays"]).To(Equal([]any{
					map[string]any{"uuid": "array-1", "name": "Main", "vendorId": "vendor", "modelId": "model"},
					map[string]any{"name": "partial"},
				}))
				Expect(got["storageContainerId"]).To(Equal("container-1"))
			} else {
				Expect(got).NotTo(HaveKey("storageArrays"))
				Expect(got).NotTo(HaveKey("storageContainerId"))
			}
		}
	}, Entry("known and partial records", true), Entry("unavailable records", false))

	It("resolves both generated inventory schemas to optional array records", func() {
		agentSpec, err := agentapi.GetSwagger()
		Expect(err).NotTo(HaveOccurred())
		analyzerSpec, err := analyzerapi.GetSwagger()
		Expect(err).NotTo(HaveOccurred())
		for name, spec := range map[string]*openapi3.T{"agent": agentSpec, "analyzer": analyzerSpec} {
			queue := make([]*openapi3.Schema, 0, len(spec.Components.Schemas))
			for _, ref := range spec.Components.Schemas {
				queue = append(queue, ref.Value)
			}
			seen := make(map[*openapi3.Schema]bool)
			found := false
			for len(queue) > 0 {
				schema := queue[0]
				queue = queue[1:]
				if schema == nil || seen[schema] {
					continue
				}
				seen[schema] = true
				if arrays, ok := schema.Properties["storageArrays"]; ok {
					found = true
					Expect(schema.Required).NotTo(ContainElement("storageArrays"), name)
					Expect(schema.Properties).To(HaveKey("storageContainerId"), name)
					Expect(arrays.Value.Items.Value.Properties).To(HaveKey("uuid"), name)
					Expect(arrays.Value.Items.Value.Properties).To(HaveKey("vendorId"), name)
					Expect(arrays.Value.Items.Value.Required).To(BeEmpty(), name)
				}
				for _, ref := range schema.Properties {
					queue = append(queue, ref.Value)
				}
				if schema.Items != nil {
					queue = append(queue, schema.Items.Value)
				}
			}
			Expect(found).To(BeTrue(), name)
		}
	})
})
