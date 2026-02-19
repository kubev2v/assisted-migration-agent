package filter

import (
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Parser", func() {
	Context("Valid expressions", func() {
		type testCase struct {
			input  string
			output string
		}

		tests := []testCase{
			// ===== SIMPLE EQUALITY =====
			{input: "name = 'test'", output: `(name equal "test")`},
			{input: "name != 'test'", output: `(name notEqual "test")`},
			{input: `name = "test"`, output: `(name equal "test")`},
			{input: `name != "test"`, output: `(name notEqual "test")`},

			// ===== COMPARISON OPERATORS =====
			{input: "count > '10'", output: `(count greater "10")`},
			{input: "count >= '10'", output: `(count gte "10")`},
			{input: "count < '10'", output: `(count less "10")`},
			{input: "count <= '10'", output: `(count lte "10")`},

			// ===== REGEX OPERATORS =====
			{input: "name ~ /pattern/", output: "(name like /pattern/)"},
			{input: "name !~ /pattern/", output: "(name notLike /pattern/)"},
			{input: "name ~ /^prod-.*/", output: "(name like /^prod-.*/)"},
			{input: "name !~ /test/", output: "(name notLike /test/)"},
			{input: "name ~ /a\\/b/", output: "(name like /a/b/)"},

			// ===== BOOLEAN VALUES =====
			{input: "enabled = true", output: "(enabled equal true)"},
			{input: "enabled = false", output: "(enabled equal false)"},
			{input: "active != true", output: "(active notEqual true)"},
			{input: "active != false", output: "(active notEqual false)"},
			{input: "enabled = TRUE", output: "(enabled equal true)"},
			{input: "enabled = FALSE", output: "(enabled equal false)"},
			{input: "enabled = True", output: "(enabled equal true)"},
			{input: "enabled = False", output: "(enabled equal false)"},

			// ===== QUANTITY VALUES =====
			// With units
			{input: "memory > 8GB", output: "(memory greater 8.00Gb)"},
			{input: "memory >= 16GB", output: "(memory gte 16.00Gb)"},
			{input: "memory < 4GB", output: "(memory less 4.00Gb)"},
			{input: "memory <= 2GB", output: "(memory lte 2.00Gb)"},
			{input: "disk = 100MB", output: "(disk equal 100.00Mb)"},
			{input: "disk = 500KB", output: "(disk equal 500.00Kb)"},
			{input: "disk = 1TB", output: "(disk equal 1.00Tb)"},
			{input: "memory > 1.5GB", output: "(memory greater 1.50Gb)"},
			{input: "memory > 100.25MB", output: "(memory greater 100.25Mb)"},

			// Without units (plain numbers)
			{input: "count > 100", output: "(count greater 100.00)"},
			{input: "count >= 50", output: "(count gte 50.00)"},
			{input: "count < 10", output: "(count less 10.00)"},
			{input: "count <= 5", output: "(count lte 5.00)"},
			{input: "count = 0", output: "(count equal 0.00)"},
			{input: "price > 3.14", output: "(price greater 3.14)"},
			{input: "ratio = 0.5", output: "(ratio equal 0.50)"},

			// ===== DOTTED IDENTIFIERS =====
			{input: "vm.name = 'test'", output: `(vm.name equal "test")`},
			{input: "vm.host.datacenter = 'DC1'", output: `(vm.host.datacenter equal "DC1")`},
			{input: "a.b.c.d.e = 'value'", output: `(a.b.c.d.e equal "value")`},

			// ===== AND EXPRESSIONS =====
			{input: "a = '1' and b = '2'", output: `((a equal "1") and (b equal "2"))`},
			{input: "a = '1' AND b = '2'", output: `((a equal "1") and (b equal "2"))`},
			{input: "a = '1' And b = '2'", output: `((a equal "1") and (b equal "2"))`},
			{input: "a = '1' and b = '2' and c = '3'", output: `(((a equal "1") and (b equal "2")) and (c equal "3"))`},

			// ===== OR EXPRESSIONS =====
			{input: "a = '1' or b = '2'", output: `((a equal "1") or (b equal "2"))`},
			{input: "a = '1' OR b = '2'", output: `((a equal "1") or (b equal "2"))`},
			{input: "a = '1' Or b = '2'", output: `((a equal "1") or (b equal "2"))`},
			{input: "a = '1' or b = '2' or c = '3'", output: `(((a equal "1") or (b equal "2")) or (c equal "3"))`},

			// ===== MIXED AND/OR (AND has higher precedence) =====
			{input: "a = '1' or b = '2' and c = '3'", output: `((a equal "1") or ((b equal "2") and (c equal "3")))`},
			{input: "a = '1' and b = '2' or c = '3'", output: `(((a equal "1") and (b equal "2")) or (c equal "3"))`},
			{input: "a = '1' or b = '2' and c = '3' or d = '4'", output: `(((a equal "1") or ((b equal "2") and (c equal "3"))) or (d equal "4"))`},
			{input: "a = '1' and b = '2' or c = '3' and d = '4'", output: `(((a equal "1") and (b equal "2")) or ((c equal "3") and (d equal "4")))`},

			// ===== PARENTHESES (grouping) =====
			{input: "(a = '1')", output: `(a equal "1")`},
			{input: "((a = '1'))", output: `(a equal "1")`},
			{input: "(a = '1' and b = '2')", output: `((a equal "1") and (b equal "2"))`},
			{input: "(a = '1' or b = '2')", output: `((a equal "1") or (b equal "2"))`},

			// ===== PARENTHESES CHANGING PRECEDENCE =====
			{input: "(a = '1' or b = '2') and c = '3'", output: `(((a equal "1") or (b equal "2")) and (c equal "3"))`},
			{input: "a = '1' and (b = '2' or c = '3')", output: `((a equal "1") and ((b equal "2") or (c equal "3")))`},
			{input: "(a = '1' or b = '2') and (c = '3' or d = '4')", output: `(((a equal "1") or (b equal "2")) and ((c equal "3") or (d equal "4")))`},

			// ===== DEEPLY NESTED PARENTHESES =====
			{input: "((a = '1' or b = '2') and c = '3')", output: `(((a equal "1") or (b equal "2")) and (c equal "3"))`},
			{input: "(a = '1' and (b = '2' or (c = '3' and d = '4')))", output: `((a equal "1") and ((b equal "2") or ((c equal "3") and (d equal "4"))))`},

			// ===== STRINGS WITH SPECIAL CHARACTERS =====
			{input: "name = 'hello world'", output: `(name equal "hello world")`},
			{input: "name = 'test=value'", output: `(name equal "test=value")`},
			{input: "name = 'test>value'", output: `(name equal "test>value")`},
			{input: "name = 'test<value'", output: `(name equal "test<value")`},

			// ===== MIXED TYPES IN EXPRESSIONS =====
			{input: "name = 'test' and enabled = true", output: `((name equal "test") and (enabled equal true))`},
			{input: "name ~ /prod/ and memory > 8GB", output: "((name like /prod/) and (memory greater 8.00Gb))"},
			{input: "enabled = true or memory < 4GB", output: "((enabled equal true) or (memory less 4.00Gb))"},
			{input: "name ~ /test/ and enabled = false and memory >= 16GB", output: "(((name like /test/) and (enabled equal false)) and (memory gte 16.00Gb))"},

			// ===== REAL-WORLD EXAMPLES =====
			{
				input:  "vm.name = 'production-db' and vm.host.datacenter = 'DC1'",
				output: `((vm.name equal "production-db") and (vm.host.datacenter equal "DC1"))`,
			},
			{
				input:  "vm.name ~ /^prod-.*/ and vm.status = 'running'",
				output: `((vm.name like /^prod-.*/) and (vm.status equal "running"))`,
			},
			{
				input:  "memory >= 8GB and cpu.cores > '4' or vm.priority = 'high'",
				output: `(((memory gte 8.00Gb) and (cpu.cores greater "4")) or (vm.priority equal "high"))`,
			},
			{
				input:  "(memory >= 8GB or cpu.cores > '4') and vm.status = 'ready'",
				output: `(((memory gte 8.00Gb) or (cpu.cores greater "4")) and (vm.status equal "ready"))`,
			},
			{
				input:  "os.name = 'linux' and os.version != 'ubuntu' and kernel.version >= '5.0'",
				output: `(((os.name equal "linux") and (os.version notEqual "ubuntu")) and (kernel.version gte "5.0"))`,
			},
			{
				input:  "active = true and (role = 'admin' or role = 'superuser')",
				output: `((active equal true) and ((role equal "admin") or (role equal "superuser")))`,
			},

			// ===== OPERATORS WITHOUT SPACES =====
			{input: "name='test'", output: `(name equal "test")`},
			{input: "count>='10'", output: `(count gte "10")`},
			{input: "count<='10'", output: `(count lte "10")`},
			{input: "name~/pattern/", output: "(name like /pattern/)"},

			// ===== WHITESPACE VARIATIONS =====
			{input: "  name = 'test'  ", output: `(name equal "test")`},
			{input: "\tname = 'test'\t", output: `(name equal "test")`},
			{input: "name   =   'test'", output: `(name equal "test")`},
			{input: "a = '1'   and   b = '2'", output: `((a equal "1") and (b equal "2"))`},
		}

		for _, test := range tests {
			test := test // capture range variable
			It("should parse: "+test.input, func() {
				expr, err := Parse([]byte(test.input))
				Expect(err).ToNot(HaveOccurred())
				Expect(expr.String()).To(Equal(test.output))
			})
		}
	})

	Context("Invalid expressions", func() {
		inputs := []string{
			"name 'test'",
			"name =",
			"(name = 'test'",
			"= = =",
			"",
			"   ",
			"name = = 'test'",
			"= 'test'",
		}

		for _, input := range inputs {
			input := input
			It("should return ParseError for: "+input, func() {
				_, err := Parse([]byte(input))
				Expect(err).To(HaveOccurred())
				var pe ParseError
				Expect(errors.As(err, &pe)).To(BeTrue())
			})
		}
	})

})
