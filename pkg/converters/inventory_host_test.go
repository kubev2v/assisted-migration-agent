package converters_test

import (
	"encoding/json"

	api "github.com/kubev2v/migration-planner/api/v1alpha1"
	"github.com/kubev2v/migration-planner/pkg/inventory"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

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
