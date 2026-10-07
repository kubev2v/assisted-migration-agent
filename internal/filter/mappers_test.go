package filter

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("VM Mapper Tests", func() {
	Context("DefaultMapper field mappings", func() {
		type fieldCase struct {
			field  string
			expect string
		}

		fields := []fieldCase{
			{"id", `v."VM ID"`},
			{"name", `v."VM"`},
			{"folder_id", `v."Folder ID"`},
			{"folder", `v."Folder"`},
			{"host", `v."Host"`},
			{"smbios_uuid", `v."SMBIOS UUID"`},
			{"vm_uuid", `v."VM UUID"`},
			{"firmware", `v."Firmware"`},
			{"powerstate", `v."Powerstate"`},
			{"status", `v."Powerstate"`},
			{"connection_state", `v."Connection state"`},
			{"ft_state", `v."FT State"`},
			{"cpus", `v."CPUs"`},
			{"memory", `v."Memory"`},
			{"os_config", `v."OS according to the configuration file"`},
			{"os_tools", `v."OS according to the VMware Tools"`},
			{"dns_name", `v."DNS Name"`},
			{"ip_address", `v."Primary IP Address"`},
			{"storage_used", `v."In Use MiB"`},
			{"template", `v."Template"`},
			{"cbt", `v."CBT"`},
			{"enable_uuid", `v."EnableUUID"`},
			{"datacenter", `v."Datacenter"`},
			{"cluster", `v."Cluster"`},
			{"cluster_id", `vc."Object ID"`},
			{"metadata.key", `metadata.key`},
			{"metadata.value", `metadata.value`},
			{"hw_version", `v."HW version"`},
			{"total_disk_capacity", `d.total_disk`},
			{"provisioned", `v."Provisioned MiB"`},
			{"resource_pool", `v."Resource pool"`},
			{"issues_count", `cc."issues_count"`},
			{"migratable", `(COALESCE(crit.critical_count, 0) = 0)`},
			{"disk.key", `dk."Disk Key"`},
			{"disk.path", `dk."Disk Path"`},
			{"disk.capacity", `dk."Capacity MiB"`},
			{"disk.sharing", `dk."Sharing mode"`},
			{"disk.raw", `dk."Raw"`},
			{"disk.shared_bus", `dk."Shared Bus"`},
			{"disk.mode", `dk."Disk Mode"`},
			{"disk.thin", `dk."Thin"`},
			{"disk.controller", `dk."Controller"`},
			{"disk.label", `dk."Label"`},
			{"concern.label", `c."Label"`},
			{"concern.category", `c."Category"`},
			{"concern.assessment", `c."Assessment"`},
			{"inspection.status", `i.status`},
			{"inspection.error", `i.error`},
			{"inspection_concern.label", `ic.label`},
			{"inspection_concern.category", `ic.category`},
			{"inspection_concern.msg", `ic.msg`},
			{"cpu.hot_add", `cpu."Hot Add"`},
			{"cpu.hot_remove", `cpu."Hot Remove"`},
			{"cpu.sockets", `cpu."Sockets"`},
			{"cpu.cores_per_socket", `cpu."Cores p/s"`},
			{"mem.hot_add", `mem."Hot Add"`},
			{"mem.ballooned", `mem."Ballooned"`},
			{"net.network", `net."Network"`},
			{"net.mac", `net."Mac Address"`},
			{"net.nic_label", `net."NIC label"`},
			{"net.adapter", `net."Adapter"`},
			{"net.switch", `net."Switch"`},
			{"net.connected", `net."Connected"`},
			{"net.starts_connected", `net."Starts Connected"`},
			{"net.type", `net."Type"`},
			{"net.ipv4", `net."IPv4 Address"`},
			{"net.ipv6", `net."IPv6 Address"`},
			{"net.cluster", `net."Cluster"`},
			{"datastore.name", `ds."Name"`},
			{"datastore.hosts", `ds."Hosts"`},
			{"datastore.address", `ds."Address"`},
			{"datastore.object_id", `ds."Object ID"`},
			{"datastore.free", `ds."Free MiB"`},
			{"datastore.mha", `ds."MHA"`},
			{"datastore.capacity", `ds."Capacity MiB"`},
			{"datastore.type", `ds."Type"`},
		}

		for _, tc := range fields {
			tc := tc
			It("should map "+tc.field, func() {
				col, _, err := DefaultMapper(tc.field)
				Expect(err).ToNot(HaveOccurred())
				Expect(col).To(Equal(tc.expect))
			})
		}

		It("should be case-insensitive", func() {
			col, _, err := DefaultMapper("MEMORY")
			Expect(err).ToNot(HaveOccurred())
			Expect(col).To(Equal(`v."Memory"`))
		})

		It("should return error for unknown field", func() {
			_, _, err := DefaultMapper("nonexistent")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unknown filter field"))
		})
	})

	Context("GroupMapper field mappings", func() {
		It("should map name", func() {
			col, _, err := GroupMapper("name")
			Expect(err).ToNot(HaveOccurred())
			Expect(col).To(Equal("name"))
		})

		It("should map description", func() {
			col, _, err := GroupMapper("description")
			Expect(err).ToNot(HaveOccurred())
			Expect(col).To(Equal("description"))
		})

		It("should map filter", func() {
			col, _, err := GroupMapper("filter")
			Expect(err).ToNot(HaveOccurred())
			Expect(col).To(Equal("filter"))
		})

		It("should return error for unknown field", func() {
			_, _, err := GroupMapper("bogus")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unknown group filter field"))
		})
	})

	Context("ParseWithDefaultMap type validation", func() {
		It("should reject boolean value for string field", func() {
			_, err := ParseWithDefaultMap([]byte("name = true"))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("is string, but got boolean"))
		})

		It("should reject string value for numeric field", func() {
			_, err := ParseWithDefaultMap([]byte("memory = 'big'"))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("is numeric, but got string"))
		})

		It("should reject numeric value for boolean field", func() {
			_, err := ParseWithDefaultMap([]byte("template > 5"))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("is boolean, but got numeric"))
		})

		It("should accept valid typed expressions", func() {
			_, err := ParseWithDefaultMap([]byte("memory > 8GB and template = true and name = 'test'"))
			Expect(err).ToNot(HaveOccurred())
		})
	})
})
