package scripting

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.starlark.net/starlark"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
)

var _ = Describe("VirtualMachineList", func() {
	var vms *VirtualMachineList

	BeforeEach(func() {
		vms = NewVirtualMachineList([]*VirtualMachine{
			makeVM("vm-a", "vm-a", "prod", 4, 8192, true, false),
			makeVM("vm-b", "vm-b", "dev", 2, 4096, false, false),
			makeVM("vm-c", "vm-c", "prod", 8, 16384, true, false),
			makeVM("vm-d", "vm-d", "dev", 1, 2048, true, false),
		})
	})

	Describe("filter", func() {
		It("filters by predicate", func() {
			g := starlark.StringDict{"vms": vms}
			r := execScript(g, `count = len(vms.filter(lambda vm: vm.is_migratable))`)
			Expect(intVal(r, "count")).To(Equal(int64(3)))
		})

		It("filters by cluster", func() {
			g := starlark.StringDict{"vms": vms}
			r := execScript(g, `count = len(vms.filter(lambda vm: vm.cluster == "prod"))`)
			Expect(intVal(r, "count")).To(Equal(int64(2)))
		})

		It("returns VirtualMachineList that supports chaining", func() {
			g := starlark.StringDict{"vms": vms}
			r := execScript(g, `
filtered = vms.filter(lambda vm: vm.is_migratable)
count = len(filtered.filter(lambda vm: vm.cluster == "prod"))
`)
			Expect(intVal(r, "count")).To(Equal(int64(2)))
		})

		It("returns empty VirtualMachineList when nothing matches", func() {
			g := starlark.StringDict{"vms": vms}
			r := execScript(g, `count = len(vms.filter(lambda vm: vm.cluster == "nonexistent"))`)
			Expect(intVal(r, "count")).To(Equal(int64(0)))
		})
	})

	Describe("map", func() {
		It("extracts field values", func() {
			g := starlark.StringDict{"vms": vms}
			r := execScript(g, `
names = vms.map(lambda vm: vm.name)
count = len(names)
first = names[0]
`)
			Expect(intVal(r, "count")).To(Equal(int64(4)))
			Expect(strVal(r, "first")).To(Equal("vm-a"))
		})
	})

	Describe("sort", func() {
		It("sorts ascending by key", func() {
			g := starlark.StringDict{"vms": vms}
			r := execScript(g, `
sorted_vms = vms.sort(key=lambda vm: vm.memory_mb)
first = sorted_vms[0].name
last = sorted_vms[3].name
`)
			Expect(strVal(r, "first")).To(Equal("vm-d"))
			Expect(strVal(r, "last")).To(Equal("vm-c"))
		})

		It("sorts descending with reverse", func() {
			g := starlark.StringDict{"vms": vms}
			r := execScript(g, `first = vms.sort(key=lambda vm: vm.memory_mb, reverse=True)[0].name`)
			Expect(strVal(r, "first")).To(Equal("vm-c"))
		})
	})

	Describe("first", func() {
		It("finds first matching VM", func() {
			g := starlark.StringDict{"vms": vms}
			r := execScript(g, `name = vms.first(lambda vm: vm.cpu_count > 4).name`)
			Expect(strVal(r, "name")).To(Equal("vm-c"))
		})

		It("returns None when nothing matches", func() {
			g := starlark.StringDict{"vms": vms}
			r := execScript(g, `is_none = vms.first(lambda vm: vm.cpu_count > 100) == None`)
			Expect(bool(r["is_none"].(starlark.Bool))).To(BeTrue())
		})
	})

	Describe("group_by", func() {
		It("groups by cluster", func() {
			g := starlark.StringDict{"vms": vms}
			r := execScript(g, `
groups = vms.group_by(lambda vm: vm.cluster)
prod_count = len(groups["prod"])
dev_count = len(groups["dev"])
num_groups = len(groups)
`)
			Expect(intVal(r, "prod_count")).To(Equal(int64(2)))
			Expect(intVal(r, "dev_count")).To(Equal(int64(2)))
			Expect(intVal(r, "num_groups")).To(Equal(int64(2)))
		})

		It("returns VirtualMachineLists that support chaining", func() {
			g := starlark.StringDict{"vms": vms}
			r := execScript(g, `count = len(vms.group_by(lambda vm: vm.cluster)["prod"].map(lambda vm: vm.name))`)
			Expect(intVal(r, "count")).To(Equal(int64(2)))
		})
	})

	Describe("count_by", func() {
		It("counts VMs per group", func() {
			g := starlark.StringDict{"vms": vms}
			r := execScript(g, `
counts = vms.count_by(lambda vm: vm.cluster)
prod = counts["prod"]
dev = counts["dev"]
`)
			Expect(intVal(r, "prod")).To(Equal(int64(2)))
			Expect(intVal(r, "dev")).To(Equal(int64(2)))
		})

		It("returns empty dict for empty list", func() {
			empty := NewVirtualMachineList(nil)
			g := starlark.StringDict{"vms": empty}
			r := execScript(g, `count = len(vms.count_by(lambda vm: vm.cluster))`)
			Expect(intVal(r, "count")).To(Equal(int64(0)))
		})
	})

	Describe("sum_by", func() {
		It("sums values per group", func() {
			g := starlark.StringDict{"vms": vms}
			r := execScript(g, `
totals = vms.sum_by(lambda vm: vm.cluster, lambda vm: vm.cpu_count)
prod = totals["prod"]
dev = totals["dev"]
`)
			Expect(floatVal(r, "prod")).To(Equal(12.0))
			Expect(floatVal(r, "dev")).To(Equal(3.0))
		})

		It("returns empty dict for empty list", func() {
			empty := NewVirtualMachineList(nil)
			g := starlark.StringDict{"vms": empty}
			r := execScript(g, `count = len(vms.sum_by(lambda vm: vm.cluster, lambda vm: vm.cpu_count))`)
			Expect(intVal(r, "count")).To(Equal(int64(0)))
		})
	})

	Describe("group_by_cluster", func() {
		It("groups by cluster field", func() {
			g := starlark.StringDict{"vms": vms}
			r := execScript(g, `
groups = vms.group_by_cluster()
prod_count = len(groups["prod"])
dev_count = len(groups["dev"])
`)
			Expect(intVal(r, "prod_count")).To(Equal(int64(2)))
			Expect(intVal(r, "dev_count")).To(Equal(int64(2)))
		})

		It("returns chainable VirtualMachineLists", func() {
			g := starlark.StringDict{"vms": vms}
			r := execScript(g, `count = len(vms.group_by_cluster()["prod"].map(lambda vm: vm.name))`)
			Expect(intVal(r, "count")).To(Equal(int64(2)))
		})
	})

	Describe("group_by_datacenter", func() {
		It("groups by datacenter field", func() {
			g := starlark.StringDict{"vms": vms}
			r := execScript(g, `
groups = vms.group_by_datacenter()
count = len(groups["dc1"])
`)
			Expect(intVal(r, "count")).To(Equal(int64(4)))
		})
	})

	Describe("group_by_power_state", func() {
		It("groups by power_state field", func() {
			g := starlark.StringDict{"vms": vms}
			r := execScript(g, `
groups = vms.group_by_power_state()
on_count = len(groups["poweredOn"])
`)
			Expect(intVal(r, "on_count")).To(Equal(int64(4)))
		})
	})

	Describe("merge", func() {
		It("merges two lists", func() {
			a := NewVirtualMachineList([]*VirtualMachine{
				makeVM("vm-a", "vm-a", "prod", 4, 8192, true, false),
			})
			b := NewVirtualMachineList([]*VirtualMachine{
				makeVM("vm-b", "vm-b", "dev", 2, 4096, true, false),
			})
			g := starlark.StringDict{"a": a, "b": b}
			r := execScript(g, `count = len(a.merge(b))`)
			Expect(intVal(r, "count")).To(Equal(int64(2)))
		})

		It("merges multiple lists", func() {
			a := NewVirtualMachineList([]*VirtualMachine{
				makeVM("vm-a", "vm-a", "prod", 4, 8192, true, false),
			})
			b := NewVirtualMachineList([]*VirtualMachine{
				makeVM("vm-b", "vm-b", "dev", 2, 4096, true, false),
			})
			c := NewVirtualMachineList([]*VirtualMachine{
				makeVM("vm-c", "vm-c", "staging", 1, 2048, true, false),
			})
			g := starlark.StringDict{"a": a, "b": b, "c": c}
			r := execScript(g, `count = len(a.merge(b, c))`)
			Expect(intVal(r, "count")).To(Equal(int64(3)))
		})
	})

	Describe("+ operator", func() {
		It("concatenates two VirtualMachineLists", func() {
			a := NewVirtualMachineList([]*VirtualMachine{
				makeVM("vm-a", "vm-a", "prod", 4, 8192, true, false),
			})
			b := NewVirtualMachineList([]*VirtualMachine{
				makeVM("vm-b", "vm-b", "dev", 2, 4096, true, false),
			})
			g := starlark.StringDict{"a": a, "b": b}
			r := execScript(g, `count = len(a + b)`)
			Expect(intVal(r, "count")).To(Equal(int64(2)))
		})

		It("result is chainable", func() {
			a := NewVirtualMachineList([]*VirtualMachine{
				makeVM("vm-a", "vm-a", "prod", 4, 8192, true, false),
			})
			b := NewVirtualMachineList([]*VirtualMachine{
				makeVM("vm-b", "vm-b", "dev", 2, 4096, true, false),
			})
			g := starlark.StringDict{"a": a, "b": b}
			r := execScript(g, `name = (a + b).sort(key=lambda vm: vm.name)[0].name`)
			Expect(strVal(r, "name")).To(Equal("vm-a"))
		})
	})

	Describe("VirtualMachine detail attributes", func() {
		It("exposes disks, nics, and issues as direct attributes", func() {
			vm := makeVM("vm-1", "web", "prod", 4, 8192, true, false)
			vm.disks = starlark.NewList([]starlark.Value{
				NewDisk(models.Disk{File: "[ds1] web/web.vmdk", Capacity: 1073741824}),
			})
			vm.nics = starlark.NewList([]starlark.Value{
				NewNIC(models.NIC{MAC: "00:50:56:a1:b2:c3", Network: "VM Network"}),
			})
			vm.issues = starlark.NewList([]starlark.Value{
				NewIssue(models.Issue{Label: "No tools", Category: "Warning"}),
			})

			g := starlark.StringDict{"vm": vm}
			r := execScript(g, `
disk_count = len(vm.disks)
nic_count = len(vm.nics)
issue_count_list = len(vm.issues)
`)
			Expect(intVal(r, "disk_count")).To(Equal(int64(1)))
			Expect(intVal(r, "nic_count")).To(Equal(int64(1)))
			Expect(intVal(r, "issue_count_list")).To(Equal(int64(1)))
		})

		It("returns empty lists when no detail data is present", func() {
			vm := makeVM("vm-1", "web", "prod", 4, 8192, true, false)
			g := starlark.StringDict{"vm": vm}
			r := execScript(g, `count = len(vm.disks)`)
			Expect(intVal(r, "count")).To(Equal(int64(0)))
		})
	})

	Describe("totals", func() {
		It("sums specified fields across all VMs", func() {
			g := starlark.StringDict{"vms": vms}
			r := execScript(g, `t = vms.totals(["cpu_count", "memory_mb"])`)
			t := r["t"].(*starlark.Dict)
			cpu, _, _ := t.Get(starlark.String("cpu_count"))
			mem, _, _ := t.Get(starlark.String("memory_mb"))
			cpuVal, _ := cpu.(starlark.Int).Int64()
			memVal, _ := mem.(starlark.Int).Int64()
			Expect(cpuVal).To(Equal(int64(4 + 2 + 8 + 1)))
			Expect(memVal).To(Equal(int64(8192 + 4096 + 16384 + 2048)))
		})

		It("returns int sums for int fields", func() {
			g := starlark.StringDict{"vms": vms}
			r := execScript(g, `
t = vms.totals(["cpu_count"])
is_int = type(t["cpu_count"]) == "int"
`)
			Expect(r["is_int"]).To(Equal(starlark.True))
		})

		It("returns float sums for float fields", func() {
			vm := makeVM("vm-1", "web", "prod", 4, 8192, true, false)
			vm.utilizationCpuP95 = starlark.Float(25.5)
			vml := NewVirtualMachineList([]*VirtualMachine{vm})
			g := starlark.StringDict{"vms": vml}
			r := execScript(g, `
t = vms.totals(["utilization_cpu_p95"])
is_float = type(t["utilization_cpu_p95"]) == "float"
`)
			Expect(r["is_float"]).To(Equal(starlark.True))
		})

		It("skips None values", func() {
			vm1 := makeVM("vm-1", "web", "prod", 4, 8192, true, false)
			vm1.utilizationCpuP95 = starlark.Float(30.0)
			vm2 := makeVM("vm-2", "db", "prod", 8, 16384, true, false)
			// vm2 utilization stays None (default)
			vml := NewVirtualMachineList([]*VirtualMachine{vm1, vm2})
			g := starlark.StringDict{"vms": vml}
			r := execScript(g, `val = vms.totals(["utilization_cpu_p95"])["utilization_cpu_p95"]`)
			Expect(r["val"]).To(Equal(starlark.Float(30.0)))
		})
	})
})
