package scripting

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.starlark.net/starlark"
)

var _ = Describe("json", func() {
	It("serializes dicts to JSON", func() {
		g := starlark.StringDict{"json": starlark.NewBuiltin("json", jsonReport)}
		r := execScript(g, `out = json({"key": "value", "num": 42})`)
		got := strVal(r, "out")
		Expect(got).To(ContainSubstring(`"key"`))
		Expect(got).To(ContainSubstring(`"value"`))
	})
})

var _ = Describe("csv", func() {
	It("generates CSV with headers", func() {
		g := starlark.StringDict{"csv": starlark.NewBuiltin("csv", csvReport)}
		r := execScript(g, `out = csv(["Name", "Value"], [["vm1", 100]])`)
		Expect(strVal(r, "out")).To(ContainSubstring("Name,Value"))
	})

	It("sanitizes formula injection", func() {
		g := starlark.StringDict{"csv": starlark.NewBuiltin("csv", csvReport)}
		r := execScript(g, `out = csv(["Name", "Value"], [["=cmd", 200]])`)
		Expect(strVal(r, "out")).To(ContainSubstring("'=cmd"))
	})
})

var _ = Describe("template module", func() {
	It("substitutes template variables", func() {
		g := starlark.StringDict{"template": templateModule()}
		r := execScript(g, `out = template.render("Hello {{.name}}, you have {{.count}} VMs", {"name": "admin", "count": 42})`)
		Expect(strVal(r, "out")).To(Equal("Hello admin, you have 42 VMs"))
	})

	It("handles range loops", func() {
		g := starlark.StringDict{"template": templateModule()}
		r := execScript(g, `out = template.render("{{range .items}}- {{.}}\n{{end}}", {"items": ["a", "b", "c"]})`)
		Expect(strVal(r, "out")).To(Equal("- a\n- b\n- c\n"))
	})
})
