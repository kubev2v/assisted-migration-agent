package scripting

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.starlark.net/starlark"
)

var _ = Describe("debug module", func() {
	var g starlark.StringDict

	BeforeEach(func() {
		g = starlark.StringDict{
			"debug": debugModule(),
		}
	})

	Describe("dump", func() {
		It("dumps a VirtualMachine with all fields", func() {
			vm := makeVM("vm-1", "web-1", "prod", 4, 8192, true, false)
			g["vm"] = vm
			r := execScript(g, `out = debug.dump(vm)`)
			out := strVal(r, "out")
			Expect(out).To(ContainSubstring("VirtualMachine:"))
			Expect(out).To(ContainSubstring("name:"))
			Expect(out).To(ContainSubstring(`"web-1"`))
			Expect(out).To(ContainSubstring("cluster:"))
			Expect(out).To(ContainSubstring(`"prod"`))
			Expect(out).To(ContainSubstring("memory_mb:"))
			Expect(out).To(ContainSubstring("8192"))
		})

		It("dumps a VirtualMachineList with summary", func() {
			vms := NewVirtualMachineList([]*VirtualMachine{
				makeVM("vm-1", "web", "prod", 4, 8192, true, false),
				makeVM("vm-2", "db", "prod", 8, 16384, true, false),
				makeVM("vm-3", "cache", "dev", 2, 4096, true, false),
			})
			g["vms"] = vms
			r := execScript(g, `out = debug.dump(vms)`)
			out := strVal(r, "out")
			Expect(out).To(ContainSubstring("VirtualMachineList (3 items)"))
			Expect(out).To(ContainSubstring("[0]"))
			Expect(out).To(ContainSubstring("web"))
			Expect(out).To(ContainSubstring("cache"))
		})

		It("dumps a dict", func() {
			r := execScript(g, `out = debug.dump({"total": 400, "migratable": 101})`)
			out := strVal(r, "out")
			Expect(out).To(ContainSubstring("dict:"))
			Expect(out).To(ContainSubstring("total"))
			Expect(out).To(ContainSubstring("400"))
		})

		It("dumps a list", func() {
			r := execScript(g, `out = debug.dump(["a", "b", "c"])`)
			out := strVal(r, "out")
			Expect(out).To(ContainSubstring("list (3 items)"))
			Expect(out).To(ContainSubstring(`"a"`))
		})

		It("dumps primitives", func() {
			r := execScript(g, `out = debug.dump(42)`)
			Expect(strVal(r, "out")).To(Equal("42"))
		})
	})

	Describe("describe", func() {
		It("shows type and attributes for VirtualMachine", func() {
			g["vm"] = makeVM("vm-1", "web", "prod", 4, 8192, true, false)
			r := execScript(g, `out = debug.describe(vm)`)
			out := strVal(r, "out")
			Expect(out).To(ContainSubstring("VirtualMachine"))
			Expect(out).To(ContainSubstring("attributes:"))
			Expect(out).To(ContainSubstring("name"))
			Expect(out).To(ContainSubstring("cluster"))
			Expect(out).To(ContainSubstring("memory_mb"))
		})

		It("shows type for primitives without attributes", func() {
			r := execScript(g, `out = debug.describe(42)`)
			Expect(strVal(r, "out")).To(Equal("int"))
		})
	})

	Describe("peek", func() {
		It("shows tabular output with selected fields", func() {
			vms := NewVirtualMachineList([]*VirtualMachine{
				makeVM("vm-1", "web", "prod", 4, 8192, true, false),
				makeVM("vm-2", "db", "staging", 8, 16384, false, false),
			})
			g["vms"] = vms
			r := execScript(g, `out = debug.peek(vms, ["name", "cluster", "memory_mb"])`)
			out := strVal(r, "out")
			Expect(out).To(ContainSubstring("name"))
			Expect(out).To(ContainSubstring("cluster"))
			Expect(out).To(ContainSubstring("memory_mb"))
			Expect(out).To(ContainSubstring("web"))
			Expect(out).To(ContainSubstring("staging"))
			Expect(out).To(ContainSubstring("8192"))
			Expect(out).To(ContainSubstring("16384"))
		})

		It("respects limit", func() {
			vms := NewVirtualMachineList([]*VirtualMachine{
				makeVM("vm-1", "a", "prod", 1, 1024, true, false),
				makeVM("vm-2", "b", "prod", 2, 2048, true, false),
				makeVM("vm-3", "c", "prod", 3, 3072, true, false),
			})
			g["vms"] = vms
			r := execScript(g, `out = debug.peek(vms, ["name"], limit=2)`)
			out := strVal(r, "out")
			Expect(out).To(ContainSubstring("a"))
			Expect(out).To(ContainSubstring("b"))
			Expect(out).To(ContainSubstring("1 more"))
			Expect(out).NotTo(ContainSubstring(`"c"`))
		})
	})
})
