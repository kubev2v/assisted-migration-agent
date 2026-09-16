package scripting

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.starlark.net/starlark"
)

var _ = Describe("math module", func() {
	var g starlark.StringDict

	BeforeEach(func() {
		g = starlark.StringDict{"math": mathModule()}
	})

	Describe("ceil", func() {
		It("rounds up floats", func() {
			r := execScript(g, `out = math.ceil(3.2)`)
			Expect(intVal(r, "out")).To(Equal(int64(4)))
		})

		It("preserves whole floats", func() {
			r := execScript(g, `out = math.ceil(3.0)`)
			Expect(intVal(r, "out")).To(Equal(int64(3)))
		})

		It("handles negative floats", func() {
			r := execScript(g, `out = math.ceil(-2.7)`)
			Expect(intVal(r, "out")).To(Equal(int64(-2)))
		})

		It("accepts ints", func() {
			r := execScript(g, `out = math.ceil(5)`)
			Expect(intVal(r, "out")).To(Equal(int64(5)))
		})
	})

	Describe("floor", func() {
		It("rounds down floats", func() {
			r := execScript(g, `out = math.floor(3.9)`)
			Expect(intVal(r, "out")).To(Equal(int64(3)))
		})

		It("handles negatives", func() {
			r := execScript(g, `out = math.floor(-2.1)`)
			Expect(intVal(r, "out")).To(Equal(int64(-3)))
		})
	})

	Describe("round", func() {
		It("rounds to nearest", func() {
			r := execScript(g, `a = math.round(3.5); b = math.round(3.4)`)
			Expect(intVal(r, "a")).To(Equal(int64(4)))
			Expect(intVal(r, "b")).To(Equal(int64(3)))
		})
	})

	Describe("abs", func() {
		It("returns absolute value of int", func() {
			r := execScript(g, `out = math.abs(-5)`)
			Expect(intVal(r, "out")).To(Equal(int64(5)))
		})

		It("returns absolute value of float", func() {
			r := execScript(g, `out = math.abs(-3.14)`)
			Expect(floatVal(r, "out")).To(Equal(3.14))
		})

		It("preserves positive values", func() {
			r := execScript(g, `out = math.abs(7)`)
			Expect(intVal(r, "out")).To(Equal(int64(7)))
		})
	})

	Describe("min/max", func() {
		It("returns min of two ints", func() {
			r := execScript(g, `out = math.min(3, 7)`)
			Expect(intVal(r, "out")).To(Equal(int64(3)))
		})

		It("returns max of two ints", func() {
			r := execScript(g, `out = math.max(3, 7)`)
			Expect(intVal(r, "out")).To(Equal(int64(7)))
		})

		It("returns float when mixed types", func() {
			r := execScript(g, `out = math.min(3, 2.5)`)
			Expect(floatVal(r, "out")).To(Equal(2.5))
		})
	})

	Describe("pow", func() {
		It("computes integer powers", func() {
			r := execScript(g, `out = math.pow(2, 10)`)
			Expect(floatVal(r, "out")).To(Equal(1024.0))
		})

		It("handles zero exponent", func() {
			r := execScript(g, `out = math.pow(10, 0)`)
			Expect(floatVal(r, "out")).To(Equal(1.0))
		})
	})

	Describe("sqrt", func() {
		It("computes square root", func() {
			r := execScript(g, `out = math.sqrt(144)`)
			Expect(floatVal(r, "out")).To(Equal(12.0))
		})
	})

	Describe("sum", func() {
		It("sums a list of numbers", func() {
			r := execScript(g, `out = math.sum([1, 2, 3, 4])`)
			Expect(floatVal(r, "out")).To(Equal(10.0))
		})

		It("returns 0 for empty list", func() {
			r := execScript(g, `out = math.sum([])`)
			Expect(floatVal(r, "out")).To(Equal(0.0))
		})
	})

	Describe("mean", func() {
		It("computes average", func() {
			r := execScript(g, `out = math.mean([10, 20, 30])`)
			Expect(floatVal(r, "out")).To(Equal(20.0))
		})
	})

	Describe("median", func() {
		It("returns middle value for odd count", func() {
			r := execScript(g, `out = math.median([3, 1, 2])`)
			Expect(floatVal(r, "out")).To(Equal(2.0))
		})

		It("returns average of middle two for even count", func() {
			r := execScript(g, `out = math.median([1, 2, 3, 4])`)
			Expect(floatVal(r, "out")).To(Equal(2.5))
		})
	})

	Describe("stddev", func() {
		It("computes population standard deviation", func() {
			r := execScript(g, `out = math.stddev([2, 4, 4, 4, 5, 5, 7, 9])`)
			Expect(floatVal(r, "out")).To(Equal(2.0))
		})
	})

	Describe("percentile", func() {
		It("computes 50th percentile (median)", func() {
			r := execScript(g, `out = math.percentile([1, 2, 3, 4, 5, 6, 7, 8, 9, 10], 50)`)
			Expect(floatVal(r, "out")).To(Equal(5.5))
		})

		It("computes 0th percentile (min)", func() {
			r := execScript(g, `out = math.percentile([10, 20, 30], 0)`)
			Expect(floatVal(r, "out")).To(Equal(10.0))
		})

		It("computes 100th percentile (max)", func() {
			r := execScript(g, `out = math.percentile([10, 20, 30], 100)`)
			Expect(floatVal(r, "out")).To(Equal(30.0))
		})

		It("interpolates between values", func() {
			r := execScript(g, `out = math.percentile([0, 10], 25)`)
			Expect(floatVal(r, "out")).To(Equal(2.5))
		})
	})
})
