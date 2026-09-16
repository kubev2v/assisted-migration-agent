package scripting

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.starlark.net/starlark"
)

func testCollections() *CollectionList {
	mkCol := func(id, ts string) *Collection {
		col := &Collection{
			id:        starlark.String(id),
			timestamp: starlark.String(ts),
		}
		col.VirtualMachinesFn = func(_ string) (*VirtualMachineList, error) {
			return NewVirtualMachineList([]*VirtualMachine{
				makeVM("vm-1", "web", "prod", 4, 8192, true, false),
				makeVM("vm-2", "template", "prod", 2, 4096, false, true),
			}), nil
		}
		return col
	}
	return NewCollectionList([]*Collection{
		mkCol("col-1", "2026-07-01T00:00:00Z"),
		mkCol("col-2", "2026-08-15T00:00:00Z"),
		mkCol("col-3", "2026-09-01T00:00:00Z"),
	})
}

var _ = Describe("CollectionList", func() {
	var cols *CollectionList

	BeforeEach(func() {
		cols = testCollections()
	})

	Describe("latest", func() {
		It("returns the most recent collection", func() {
			g := starlark.StringDict{"cols": cols}
			r := execScript(g, `id = cols.latest().id`)
			Expect(strVal(r, "id")).To(Equal("col-3"))
		})
	})

	Describe("get", func() {
		It("finds by ID", func() {
			g := starlark.StringDict{"cols": cols}
			r := execScript(g, `id = cols.get("col-2").id`)
			Expect(strVal(r, "id")).To(Equal("col-2"))
		})

		It("returns None for unknown ID", func() {
			g := starlark.StringDict{"cols": cols}
			r := execScript(g, `is_none = cols.get("nonexistent") == None`)
			Expect(bool(r["is_none"].(starlark.Bool))).To(BeTrue())
		})

		It("filters by predicate", func() {
			g := starlark.StringDict{"cols": cols}
			r := execScript(g, `count = len(cols.get(lambda c: c.timestamp > "2026-08-01"))`)
			Expect(intVal(r, "count")).To(Equal(int64(2)))
		})
	})

	It("supports iteration", func() {
		g := starlark.StringDict{"cols": cols}
		r := execScript(g, `count = len([c for c in cols])`)
		Expect(intVal(r, "count")).To(Equal(int64(3)))
	})

	It("supports indexing", func() {
		g := starlark.StringDict{"cols": cols}
		r := execScript(g, `id = cols[0].id`)
		Expect(strVal(r, "id")).To(Equal("col-1"))
	})
})

var _ = Describe("Collection.summary", func() {
	It("returns VM stats", func() {
		cols := testCollections()
		g := starlark.StringDict{"cols": cols}
		r := execScript(g, `
s = cols[0].summary()
total = s["total"]
migratable = s["migratable"]
templates = s["templates"]
`)
		Expect(intVal(r, "total")).To(Equal(int64(2)))
		Expect(intVal(r, "migratable")).To(Equal(int64(1)))
		Expect(intVal(r, "templates")).To(Equal(int64(1)))
	})
})
