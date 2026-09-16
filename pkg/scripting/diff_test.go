package scripting

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.starlark.net/starlark"
)

var _ = Describe("diff module", func() {
	diffGlobals := func() starlark.StringDict {
		return starlark.StringDict{"diff": DiffModule()}
	}

	Describe("diff.vm", func() {
		It("detects field changes between two VMs", func() {
			g := diffGlobals()
			g["vm_a"] = makeVM("vm-1", "web-1", "prod", 4, 8192, true, false)
			g["vm_b"] = makeVM("vm-1", "web-1", "staging", 8, 8192, true, false)

			r := execScript(g, `changes = diff.vm(vm_a, vm_b)`)
			changes := r["changes"].(*starlark.List)
			Expect(changes.Len()).To(Equal(2)) // cluster, cpu_count
			Expect(string(changes.Index(0).(*FieldChange).field)).To(Equal("cluster"))
		})

		It("returns empty list when VMs are identical", func() {
			g := diffGlobals()
			vm := makeVM("vm-1", "web-1", "prod", 4, 8192, true, false)
			g["vm_a"] = vm
			g["vm_b"] = vm

			r := execScript(g, `changes = diff.vm(vm_a, vm_b)`)
			Expect(r["changes"].(*starlark.List).Len()).To(Equal(0))
		})
	})

	Describe("diff.vms", func() {
		It("categorizes added, removed, and changed VMs", func() {
			g := diffGlobals()
			g["vms_a"] = NewVirtualMachineList([]*VirtualMachine{
				makeVM("vm-1", "web-1", "prod", 4, 8192, true, false),
				makeVM("vm-2", "db-1", "prod", 8, 16384, true, false),
			})
			g["vms_b"] = NewVirtualMachineList([]*VirtualMachine{
				makeVM("vm-1", "web-1", "staging", 4, 8192, true, false), // changed
				makeVM("vm-3", "new-vm", "prod", 2, 4096, true, false),  // added
			})

			r := execScript(g, `d = diff.vms(vms_a, vms_b)`)
			d := r["d"].(*CollectionDiff)
			Expect(d.added.Len()).To(Equal(1))
			Expect(d.removed.Len()).To(Equal(1))
			Expect(d.changed.Len()).To(Equal(1))
		})
	})

	Describe("diff.sets", func() {
		It("computes set differences", func() {
			g := diffGlobals()
			r := execScript(g, `d = diff.sets(["a", "b", "c"], ["b", "c", "d"])`)
			d := r["d"].(*starlark.Dict)
			onlyA, _, _ := d.Get(starlark.String("only_in_a"))
			Expect(onlyA.(*starlark.List).Len()).To(Equal(1))
		})
	})

	Describe("diff.summary", func() {
		It("returns aggregate stats", func() {
			g := diffGlobals()
			g["vms_a"] = NewVirtualMachineList([]*VirtualMachine{makeVM("vm-1", "web", "prod", 4, 8192, true, false)})
			g["vms_b"] = NewVirtualMachineList([]*VirtualMachine{makeVM("vm-1", "web", "staging", 4, 8192, true, false)})

			r := execScript(g, `s = diff.summary(vms_a, vms_b)`)
			s := r["s"].(*starlark.Dict)
			changed, _, _ := s.Get(starlark.String("changed"))
			i, _ := changed.(starlark.Int).Int64()
			Expect(i).To(Equal(int64(1)))
		})
	})

	Describe("diff.fields", func() {
		It("compares only specified fields", func() {
			g := diffGlobals()
			g["vm_a"] = makeVM("vm-1", "web", "prod", 4, 8192, true, false)
			g["vm_b"] = makeVM("vm-1", "web", "staging", 8, 16384, true, false)

			r := execScript(g, `changes = diff.fields(vm_a, vm_b, ["cpu_count", "memory_mb"])`)
			Expect(r["changes"].(*starlark.List).Len()).To(Equal(2))
		})
	})

	Describe("diff.rank_changes", func() {
		It("ranks VMs by magnitude of change", func() {
			g := diffGlobals()
			g["vms_a"] = NewVirtualMachineList([]*VirtualMachine{
				makeVM("vm-1", "small", "prod", 4, 4096, true, false),
				makeVM("vm-2", "big", "prod", 4, 8192, true, false),
			})
			g["vms_b"] = NewVirtualMachineList([]*VirtualMachine{
				makeVM("vm-1", "small", "prod", 4, 8192, true, false),  // +4096
				makeVM("vm-2", "big", "prod", 4, 32768, true, false),   // +24576
			})

			r := execScript(g, `ranked = diff.rank_changes(vms_a, vms_b, "memory_mb")`)
			ranked := r["ranked"].(*starlark.List)
			Expect(ranked.Len()).To(Equal(2))
			Expect(string(ranked.Index(0).(*VirtualMachineDiff).vmID)).To(Equal("vm-2"))
		})
	})

	Describe("diff.where", func() {
		It("finds VMs where predicate result changed", func() {
			g := diffGlobals()
			g["vms_a"] = NewVirtualMachineList([]*VirtualMachine{
				makeVM("vm-1", "web", "prod", 4, 8192, true, false),
				makeVM("vm-2", "db", "prod", 8, 16384, true, false),
				makeVM("vm-3", "cache", "prod", 2, 4096, true, false),
			})
			g["vms_b"] = NewVirtualMachineList([]*VirtualMachine{
				makeVM("vm-1", "web", "prod", 4, 8192, true, false),      // unchanged
				makeVM("vm-2", "db", "staging", 8, 16384, true, false),   // cluster changed
				makeVM("vm-3", "cache", "dev", 2, 4096, true, false),     // cluster changed
			})

			r := execScript(g, `count = len(diff.where(vms_a, vms_b, lambda vm: vm.cluster))`)
			Expect(intVal(r, "count")).To(Equal(int64(2)))
		})

		It("exposes old and new values", func() {
			g := diffGlobals()
			g["vms_a"] = NewVirtualMachineList([]*VirtualMachine{makeVM("vm-1", "web", "prod", 4, 8192, true, false)})
			g["vms_b"] = NewVirtualMachineList([]*VirtualMachine{makeVM("vm-1", "web", "prod", 8, 16384, true, false)})

			r := execScript(g, `
changes = diff.where(vms_a, vms_b, lambda vm: vm.memory_mb)
old_val = changes[0]["old"]
new_val = changes[0]["new"]
`)
			Expect(intVal(r, "old_val")).To(Equal(int64(8192)))
			Expect(intVal(r, "new_val")).To(Equal(int64(16384)))
		})

		It("returns empty list when nothing changed", func() {
			g := diffGlobals()
			g["vms_a"] = NewVirtualMachineList([]*VirtualMachine{makeVM("vm-1", "web", "prod", 4, 8192, true, false)})
			g["vms_b"] = NewVirtualMachineList([]*VirtualMachine{makeVM("vm-1", "web", "prod", 4, 8192, true, false)})

			r := execScript(g, `count = len(diff.where(vms_a, vms_b, lambda vm: vm.cluster))`)
			Expect(intVal(r, "count")).To(Equal(int64(0)))
		})
	})

	Describe("diff.distribution", func() {
		It("merges and ranks two count dicts", func() {
			g := diffGlobals()
			r := execScript(g, `
d = diff.distribution(
    {"RHEL 8": 10, "Windows": 5, "Ubuntu": 3},
    {"RHEL 8": 12, "Windows": 4, "CentOS": 2},
    2,
)
labels = d["labels"]
vals_a = d["values_a"]
vals_b = d["values_b"]
`)
			labels := r["labels"].(*starlark.List)
			Expect(labels.Len()).To(Equal(3))
			Expect(string(labels.Index(0).(starlark.String))).To(Equal("RHEL 8"))
			Expect(string(labels.Index(2).(starlark.String))).To(Equal("Other"))

			valsA := r["vals_a"].(*starlark.List)
			rhel8A, _ := valsA.Index(0).(starlark.Int).Int64()
			Expect(rhel8A).To(Equal(int64(10)))

			otherA, _ := valsA.Index(2).(starlark.Int).Int64()
			Expect(otherA).To(Equal(int64(3)))

			valsB := r["vals_b"].(*starlark.List)
			otherB, _ := valsB.Index(2).(starlark.Int).Int64()
			Expect(otherB).To(Equal(int64(2)))
		})

		It("skips Other when all fit within top_n", func() {
			g := diffGlobals()
			r := execScript(g, `count = len(diff.distribution({"a": 1}, {"b": 2}, 10)["labels"])`)
			Expect(intVal(r, "count")).To(Equal(int64(2)))
		})

		It("handles missing keys across dicts", func() {
			g := diffGlobals()
			r := execScript(g, `
d = diff.distribution({"a": 5}, {"b": 3}, 10)
vals_a = d["values_a"]
vals_b = d["values_b"]
`)
			valsA := r["vals_a"].(*starlark.List)
			valsB := r["vals_b"].(*starlark.List)
			// "a" exists in A but not B
			aInA, _ := valsA.Index(0).(starlark.Int).Int64()
			Expect(aInA).To(Equal(int64(5)))
			aInB, _ := valsB.Index(0).(starlark.Int).Int64()
			Expect(aInB).To(Equal(int64(0)))
		})
	})

	Describe("diff.resource_delta", func() {
		It("computes resource deltas between two VM lists", func() {
			g := diffGlobals()
			g["vms_a"] = NewVirtualMachineList([]*VirtualMachine{
				makeVM("vm-1", "web", "prod", 4, 8192, true, false),
			})
			g["vms_b"] = NewVirtualMachineList([]*VirtualMachine{
				makeVM("vm-1", "web", "prod", 8, 16384, true, false),
			})
			r := execScript(g, `d = diff.resource_delta(vms_a, vms_b)`)
			d := r["d"].(*starlark.Dict)
			cpu, _, _ := d.Get(starlark.String("cpu"))
			cpuVal, _ := cpu.(starlark.Int).Int64()
			Expect(cpuVal).To(Equal(int64(4)))
			mem, _, _ := d.Get(starlark.String("memory_mb"))
			memVal, _ := mem.(starlark.Int).Int64()
			Expect(memVal).To(Equal(int64(8192)))
		})

		It("returns negative deltas when B is smaller", func() {
			g := diffGlobals()
			g["vms_a"] = NewVirtualMachineList([]*VirtualMachine{
				makeVM("vm-1", "web", "prod", 8, 16384, true, false),
				makeVM("vm-2", "db", "prod", 4, 8192, true, false),
			})
			g["vms_b"] = NewVirtualMachineList([]*VirtualMachine{
				makeVM("vm-1", "web", "prod", 4, 8192, true, false),
			})
			r := execScript(g, `d = diff.resource_delta(vms_a, vms_b)`)
			d := r["d"].(*starlark.Dict)
			cpu, _, _ := d.Get(starlark.String("cpu"))
			cpuVal, _ := cpu.(starlark.Int).Int64()
			Expect(cpuVal).To(Equal(int64(-8)))
		})
	})
})
