package filter

import (
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// sqlTestMapper maps variable names to simple quoted column names.
var sqlTestMapper MapFunc = func(name string) (string, error) {
	return fmt.Sprintf(`"%s"`, name), nil
}

// toSqlString is a test helper that converts a Sqlizer to a string with args interpolated.
func toSqlString(expr Expression, mf MapFunc) (string, error) {
	sqlizer, err := toSql(expr, mf)
	if err != nil {
		return "", err
	}
	sql, args, err := sqlizer.ToSql()
	if err != nil {
		return "", err
	}
	for _, arg := range args {
		var replacement string
		switch v := arg.(type) {
		case float64:
			replacement = fmt.Sprintf("%.2f", v)
		default:
			replacement = fmt.Sprintf("'%v'", arg)
		}
		sql = strings.Replace(sql, "?", replacement, 1)
	}
	return sql, nil
}

var _ = Describe("SQL Generation", func() {
	Context("Simple equality operators", func() {
		type testCase struct {
			input  string
			output string
		}

		tests := []testCase{
			// ===== EQUAL OPERATOR =====
			{input: "name = 'test'", output: `("name" = 'test')`},
			{input: "name = 'hello'", output: `("name" = 'hello')`},
			{input: `name = "test"`, output: `("name" = 'test')`},

			// ===== NOT EQUAL OPERATOR =====
			{input: "name != 'test'", output: `("name" != 'test')`},
			{input: "status != 'active'", output: `("status" != 'active')`},
		}

		for _, test := range tests {
			test := test
			It("should generate SQL for: "+test.input, func() {
				expr, err := parse([]byte(test.input))
				Expect(err).ToNot(HaveOccurred())
				sql, err := toSqlString(expr, sqlTestMapper)
				Expect(err).ToNot(HaveOccurred())
				Expect(sql).To(Equal(test.output))
			})
		}
	})

	Context("Comparison operators", func() {
		type testCase struct {
			input  string
			output string
		}

		tests := []testCase{
			// ===== GREATER THAN =====
			{input: "count > '10'", output: `("count" > '10')`},
			{input: "age > '25'", output: `("age" > '25')`},

			// ===== GREATER THAN OR EQUAL =====
			{input: "count >= '10'", output: `("count" >= '10')`},
			{input: "priority >= '5'", output: `("priority" >= '5')`},

			// ===== LESS THAN =====
			{input: "count < '10'", output: `("count" < '10')`},
			{input: "level < '3'", output: `("level" < '3')`},

			// ===== LESS THAN OR EQUAL =====
			{input: "count <= '10'", output: `("count" <= '10')`},
			{input: "rank <= '100'", output: `("rank" <= '100')`},
		}

		for _, test := range tests {
			test := test
			It("should generate SQL for: "+test.input, func() {
				expr, err := parse([]byte(test.input))
				Expect(err).ToNot(HaveOccurred())
				sql, err := toSqlString(expr, sqlTestMapper)
				Expect(err).ToNot(HaveOccurred())
				Expect(sql).To(Equal(test.output))
			})
		}
	})

	Context("Regex operators with regexp_matches", func() {
		type testCase struct {
			input  string
			output string
		}

		tests := []testCase{
			// ===== LIKE (regex match) =====
			{input: "name ~ /pattern/", output: `regexp_matches("name", 'pattern')`},
			{input: "name ~ /^prod-.*/", output: `regexp_matches("name", '^prod-.*')`},
			{input: "name ~ /test/", output: `regexp_matches("name", 'test')`},
			{input: "name ~ /[a-z]+/", output: `regexp_matches("name", '[a-z]+')`},
			{input: "name ~ /foo|bar/", output: `regexp_matches("name", 'foo|bar')`},
			{input: "name ~ /^start/", output: `regexp_matches("name", '^start')`},
			{input: "name ~ /end$/", output: `regexp_matches("name", 'end$')`},
			{input: "name ~ /.*middle.*/", output: `regexp_matches("name", '.*middle.*')`},

			// ===== NOT LIKE (regex not match) =====
			{input: "name !~ /pattern/", output: `NOT regexp_matches("name", 'pattern')`},
			{input: "name !~ /^test-.*/", output: `NOT regexp_matches("name", '^test-.*')`},
			{input: "name !~ /excluded/", output: `NOT regexp_matches("name", 'excluded')`},
			{input: "name !~ /[0-9]+/", output: `NOT regexp_matches("name", '[0-9]+')`},

			// ===== REGEX WITH ESCAPED SLASHES =====
			{input: "path ~ /a\\/b/", output: `regexp_matches("path", 'a/b')`},
			{input: "url ~ /https:\\/\\//", output: `regexp_matches("url", 'https://')`},

			// ===== REGEX WITH DOTTED IDENTIFIERS =====
			{input: "vm.name ~ /prod/", output: `regexp_matches("vm.name", 'prod')`},
			{input: "vm.host.name ~ /^dc1-.*/", output: `regexp_matches("vm.host.name", '^dc1-.*')`},
		}

		for _, test := range tests {
			test := test
			It("should generate SQL for: "+test.input, func() {
				expr, err := parse([]byte(test.input))
				Expect(err).ToNot(HaveOccurred())
				sql, err := toSqlString(expr, sqlTestMapper)
				Expect(err).ToNot(HaveOccurred())
				Expect(sql).To(Equal(test.output))
			})
		}
	})

	Context("Regex patterns with single quotes (escaping)", func() {
		type testCase struct {
			input  string
			output string
		}

		tests := []testCase{
			// With parameterized queries, the database driver handles escaping
			{input: "name ~ /it's/", output: `regexp_matches("name", 'it's')`},
			{input: "name ~ /test'pattern/", output: `regexp_matches("name", 'test'pattern')`},
			{input: "name ~ /'quoted'/", output: `regexp_matches("name", ''quoted'')`},
			{input: "name ~ /a'b'c/", output: `regexp_matches("name", 'a'b'c')`},
			{input: "name !~ /don't/", output: `NOT regexp_matches("name", 'don't')`},
		}

		for _, test := range tests {
			test := test
			It("should escape quotes in: "+test.input, func() {
				expr, err := parse([]byte(test.input))
				Expect(err).ToNot(HaveOccurred())
				sql, err := toSqlString(expr, sqlTestMapper)
				Expect(err).ToNot(HaveOccurred())
				Expect(sql).To(Equal(test.output))
			})
		}
	})

	Context("Boolean values", func() {
		type testCase struct {
			input  string
			output string
		}

		tests := []testCase{
			// ===== TRUE VALUES =====
			{input: "enabled = true", output: `("enabled" = TRUE)`},
			{input: "enabled = TRUE", output: `("enabled" = TRUE)`},
			{input: "enabled = True", output: `("enabled" = TRUE)`},
			{input: "active = true", output: `("active" = TRUE)`},

			// ===== FALSE VALUES =====
			{input: "enabled = false", output: `("enabled" = FALSE)`},
			{input: "enabled = FALSE", output: `("enabled" = FALSE)`},
			{input: "enabled = False", output: `("enabled" = FALSE)`},
			{input: "disabled = false", output: `("disabled" = FALSE)`},

			// ===== BOOLEAN WITH NOT EQUAL =====
			{input: "enabled != true", output: `("enabled" != TRUE)`},
			{input: "enabled != false", output: `("enabled" != FALSE)`},
			{input: "active != true", output: `("active" != TRUE)`},

			// ===== BOOLEAN WITH DOTTED IDENTIFIERS =====
			{input: "vm.enabled = true", output: `("vm.enabled" = TRUE)`},
			{input: "vm.config.active = false", output: `("vm.config.active" = FALSE)`},
		}

		for _, test := range tests {
			test := test
			It("should generate SQL for: "+test.input, func() {
				expr, err := parse([]byte(test.input))
				Expect(err).ToNot(HaveOccurred())
				sql, err := toSqlString(expr, sqlTestMapper)
				Expect(err).ToNot(HaveOccurred())
				Expect(sql).To(Equal(test.output))
			})
		}
	})

	Context("Quantity values with unit conversion to MB", func() {
		type testCase struct {
			input  string
			output string
		}

		tests := []testCase{
			// ===== KILOBYTES (divide by 1024) =====
			{input: "memory > 1024KB", output: `("memory" > 1.00)`},
			{input: "memory > 2048KB", output: `("memory" > 2.00)`},
			{input: "memory > 512KB", output: `("memory" > 0.50)`},
			{input: "memory >= 10240KB", output: `("memory" >= 10.00)`},
			{input: "disk < 5120KB", output: `("disk" < 5.00)`},

			// ===== MEGABYTES (baseline, no conversion) =====
			{input: "memory > 8MB", output: `("memory" > 8.00)`},
			{input: "memory >= 16MB", output: `("memory" >= 16.00)`},
			{input: "memory < 4MB", output: `("memory" < 4.00)`},
			{input: "memory <= 2MB", output: `("memory" <= 2.00)`},
			{input: "disk = 100MB", output: `("disk" = 100.00)`},
			{input: "memory > 1.5MB", output: `("memory" > 1.50)`},

			// ===== GIGABYTES (multiply by 1024) =====
			{input: "memory > 1GB", output: `("memory" > 1024.00)`},
			{input: "memory > 8GB", output: `("memory" > 8192.00)`},
			{input: "memory >= 16GB", output: `("memory" >= 16384.00)`},
			{input: "memory < 4GB", output: `("memory" < 4096.00)`},
			{input: "memory <= 2GB", output: `("memory" <= 2048.00)`},
			{input: "disk = 100GB", output: `("disk" = 102400.00)`},
			{input: "memory > 1.5GB", output: `("memory" > 1536.00)`},
			{input: "memory > 0.5GB", output: `("memory" > 512.00)`},

			// ===== TERABYTES (multiply by 1024 * 1024) =====
			{input: "disk > 1TB", output: `("disk" > 1048576.00)`},
			{input: "disk >= 2TB", output: `("disk" >= 2097152.00)`},
			{input: "disk < 4TB", output: `("disk" < 4194304.00)`},
			{input: "disk = 10TB", output: `("disk" = 10485760.00)`},
			{input: "storage > 0.5TB", output: `("storage" > 524288.00)`},

			// ===== PLAIN NUMBERS (no unit, no conversion) =====
			{input: "count > 100", output: `("count" > 100.00)`},
			{input: "count >= 50", output: `("count" >= 50.00)`},
			{input: "count < 10", output: `("count" < 10.00)`},
			{input: "count <= 5", output: `("count" <= 5.00)`},
			{input: "count = 0", output: `("count" = 0.00)`},
			{input: "price > 3.14", output: `("price" > 3.14)`},
			{input: "ratio = 0.5", output: `("ratio" = 0.50)`},
			{input: "value = 999", output: `("value" = 999.00)`},

			// ===== CASE INSENSITIVE UNITS =====
			{input: "memory > 8gb", output: `("memory" > 8192.00)`},
			{input: "memory > 8Gb", output: `("memory" > 8192.00)`},
			{input: "memory > 8gB", output: `("memory" > 8192.00)`},
			{input: "disk > 1tb", output: `("disk" > 1048576.00)`},
			{input: "disk > 1Tb", output: `("disk" > 1048576.00)`},
			{input: "memory > 512mb", output: `("memory" > 512.00)`},
			{input: "memory > 1024kb", output: `("memory" > 1.00)`},

			// ===== QUANTITIES WITH DOTTED IDENTIFIERS =====
			{input: "vm.memory > 8GB", output: `("vm.memory" > 8192.00)`},
			{input: "vm.config.disk >= 100GB", output: `("vm.config.disk" >= 102400.00)`},
		}

		for _, test := range tests {
			test := test
			It("should convert units in: "+test.input, func() {
				expr, err := parse([]byte(test.input))
				Expect(err).ToNot(HaveOccurred())
				sql, err := toSqlString(expr, sqlTestMapper)
				Expect(err).ToNot(HaveOccurred())
				Expect(sql).To(Equal(test.output))
			})
		}
	})

	Context("String values with escaping", func() {
		type testCase struct {
			input  string
			output string
		}

		tests := []testCase{
			// ===== SIMPLE STRINGS =====
			{input: "name = 'test'", output: `("name" = 'test')`},
			{input: "name = 'hello world'", output: `("name" = 'hello world')`},

			// ===== STRINGS WITH SPECIAL CHARACTERS =====
			{input: "name = 'test=value'", output: `("name" = 'test=value')`},
			{input: "name = 'test>value'", output: `("name" = 'test>value')`},
			{input: "name = 'test<value'", output: `("name" = 'test<value')`},
			{input: "name = 'hello\tworld'", output: "(\"name\" = 'hello\tworld')"},
		}

		for _, test := range tests {
			test := test
			It("should handle: "+test.input, func() {
				expr, err := parse([]byte(test.input))
				Expect(err).ToNot(HaveOccurred())
				sql, err := toSqlString(expr, sqlTestMapper)
				Expect(err).ToNot(HaveOccurred())
				Expect(sql).To(Equal(test.output))
			})
		}
	})

	Context("Dotted identifiers (variables)", func() {
		type testCase struct {
			input  string
			output string
		}

		tests := []testCase{
			{input: "vm.name = 'test'", output: `("vm.name" = 'test')`},
			{input: "vm.host.datacenter = 'DC1'", output: `("vm.host.datacenter" = 'DC1')`},
			{input: "a.b.c.d.e = 'value'", output: `("a.b.c.d.e" = 'value')`},
			{input: "config.nested.value > 100", output: `("config.nested.value" > 100.00)`},
		}

		for _, test := range tests {
			test := test
			It("should generate SQL for: "+test.input, func() {
				expr, err := parse([]byte(test.input))
				Expect(err).ToNot(HaveOccurred())
				sql, err := toSqlString(expr, sqlTestMapper)
				Expect(err).ToNot(HaveOccurred())
				Expect(sql).To(Equal(test.output))
			})
		}
	})

	Context("AND expressions", func() {
		type testCase struct {
			input  string
			output string
		}

		tests := []testCase{
			// ===== SIMPLE AND =====
			{input: "a = '1' and b = '2'", output: `(("a" = '1') AND ("b" = '2'))`},
			{input: "a = '1' AND b = '2'", output: `(("a" = '1') AND ("b" = '2'))`},
			{input: "a = '1' And b = '2'", output: `(("a" = '1') AND ("b" = '2'))`},

			// ===== CHAINED AND =====
			{input: "a = '1' and b = '2' and c = '3'", output: `((("a" = '1') AND ("b" = '2')) AND ("c" = '3'))`},
			{input: "a = '1' and b = '2' and c = '3' and d = '4'", output: `(((("a" = '1') AND ("b" = '2')) AND ("c" = '3')) AND ("d" = '4'))`},

			// ===== AND WITH DIFFERENT VALUE TYPES =====
			{input: "name = 'test' and enabled = true", output: `(("name" = 'test') AND ("enabled" = TRUE))`},
			{input: "memory > 8GB and active = true", output: `(("memory" > 8192.00) AND ("active" = TRUE))`},
			{input: "name ~ /prod/ and memory > 8GB", output: `(regexp_matches("name", 'prod') AND ("memory" > 8192.00))`},
		}

		for _, test := range tests {
			test := test
			It("should generate SQL for: "+test.input, func() {
				expr, err := parse([]byte(test.input))
				Expect(err).ToNot(HaveOccurred())
				sql, err := toSqlString(expr, sqlTestMapper)
				Expect(err).ToNot(HaveOccurred())
				Expect(sql).To(Equal(test.output))
			})
		}
	})

	Context("OR expressions", func() {
		type testCase struct {
			input  string
			output string
		}

		tests := []testCase{
			// ===== SIMPLE OR =====
			{input: "a = '1' or b = '2'", output: `(("a" = '1') OR ("b" = '2'))`},
			{input: "a = '1' OR b = '2'", output: `(("a" = '1') OR ("b" = '2'))`},
			{input: "a = '1' Or b = '2'", output: `(("a" = '1') OR ("b" = '2'))`},

			// ===== CHAINED OR =====
			{input: "a = '1' or b = '2' or c = '3'", output: `((("a" = '1') OR ("b" = '2')) OR ("c" = '3'))`},
			{input: "a = '1' or b = '2' or c = '3' or d = '4'", output: `(((("a" = '1') OR ("b" = '2')) OR ("c" = '3')) OR ("d" = '4'))`},

			// ===== OR WITH DIFFERENT VALUE TYPES =====
			{input: "enabled = true or disabled = false", output: `(("enabled" = TRUE) OR ("disabled" = FALSE))`},
			{input: "memory > 8GB or disk > 100GB", output: `(("memory" > 8192.00) OR ("disk" > 102400.00))`},
			{input: "name ~ /prod/ or name ~ /staging/", output: `(regexp_matches("name", 'prod') OR regexp_matches("name", 'staging'))`},
		}

		for _, test := range tests {
			test := test
			It("should generate SQL for: "+test.input, func() {
				expr, err := parse([]byte(test.input))
				Expect(err).ToNot(HaveOccurred())
				sql, err := toSqlString(expr, sqlTestMapper)
				Expect(err).ToNot(HaveOccurred())
				Expect(sql).To(Equal(test.output))
			})
		}
	})

	Context("Mixed AND/OR (AND has higher precedence)", func() {
		type testCase struct {
			input  string
			output string
		}

		tests := []testCase{
			// AND binds tighter than OR
			{input: "a = '1' or b = '2' and c = '3'", output: `(("a" = '1') OR (("b" = '2') AND ("c" = '3')))`},
			{input: "a = '1' and b = '2' or c = '3'", output: `((("a" = '1') AND ("b" = '2')) OR ("c" = '3'))`},
			{input: "a = '1' or b = '2' and c = '3' or d = '4'", output: `((("a" = '1') OR (("b" = '2') AND ("c" = '3'))) OR ("d" = '4'))`},
			{input: "a = '1' and b = '2' or c = '3' and d = '4'", output: `((("a" = '1') AND ("b" = '2')) OR (("c" = '3') AND ("d" = '4')))`},

			// Complex mixed expressions
			{
				input:  "name = 'test' and enabled = true or memory > 8GB and active = false",
				output: `((("name" = 'test') AND ("enabled" = TRUE)) OR (("memory" > 8192.00) AND ("active" = FALSE)))`,
			},
		}

		for _, test := range tests {
			test := test
			It("should generate SQL for: "+test.input, func() {
				expr, err := parse([]byte(test.input))
				Expect(err).ToNot(HaveOccurred())
				sql, err := toSqlString(expr, sqlTestMapper)
				Expect(err).ToNot(HaveOccurred())
				Expect(sql).To(Equal(test.output))
			})
		}
	})

	Context("Parentheses (grouping)", func() {
		type testCase struct {
			input  string
			output string
		}

		tests := []testCase{
			// ===== SIMPLE GROUPING =====
			{input: "(a = '1')", output: `("a" = '1')`},
			{input: "((a = '1'))", output: `("a" = '1')`},
			{input: "(a = '1' and b = '2')", output: `(("a" = '1') AND ("b" = '2'))`},
			{input: "(a = '1' or b = '2')", output: `(("a" = '1') OR ("b" = '2'))`},

			// ===== PARENTHESES CHANGING PRECEDENCE =====
			{input: "(a = '1' or b = '2') and c = '3'", output: `((("a" = '1') OR ("b" = '2')) AND ("c" = '3'))`},
			{input: "a = '1' and (b = '2' or c = '3')", output: `(("a" = '1') AND (("b" = '2') OR ("c" = '3')))`},
			{input: "(a = '1' or b = '2') and (c = '3' or d = '4')", output: `((("a" = '1') OR ("b" = '2')) AND (("c" = '3') OR ("d" = '4')))`},

			// ===== DEEPLY NESTED PARENTHESES =====
			{input: "((a = '1' or b = '2') and c = '3')", output: `((("a" = '1') OR ("b" = '2')) AND ("c" = '3'))`},
			{input: "(a = '1' and (b = '2' or (c = '3' and d = '4')))", output: `(("a" = '1') AND (("b" = '2') OR (("c" = '3') AND ("d" = '4'))))`},

			// ===== MULTIPLE NESTED LEVELS =====
			{
				input:  "((a = '1' or b = '2') and (c = '3' or d = '4')) or e = '5'",
				output: `(((("a" = '1') OR ("b" = '2')) AND (("c" = '3') OR ("d" = '4'))) OR ("e" = '5'))`,
			},
		}

		for _, test := range tests {
			test := test
			It("should generate SQL for: "+test.input, func() {
				expr, err := parse([]byte(test.input))
				Expect(err).ToNot(HaveOccurred())
				sql, err := toSqlString(expr, sqlTestMapper)
				Expect(err).ToNot(HaveOccurred())
				Expect(sql).To(Equal(test.output))
			})
		}
	})

	Context("Complex real-world expressions", func() {
		type testCase struct {
			input  string
			output string
		}

		tests := []testCase{
			// ===== VM FILTERING =====
			{
				input:  "vm.name = 'production-db' and vm.host.datacenter = 'DC1'",
				output: `(("vm.name" = 'production-db') AND ("vm.host.datacenter" = 'DC1'))`,
			},
			{
				input:  "vm.name ~ /^prod-.*/ and vm.status = 'running'",
				output: `(regexp_matches("vm.name", '^prod-.*') AND ("vm.status" = 'running'))`,
			},
			{
				input:  "memory >= 8GB and cpu.cores > '4' or vm.priority = 'high'",
				output: `((("memory" >= 8192.00) AND ("cpu.cores" > '4')) OR ("vm.priority" = 'high'))`,
			},
			{
				input:  "(memory >= 8GB or cpu.cores > '4') and vm.status = 'ready'",
				output: `((("memory" >= 8192.00) OR ("cpu.cores" > '4')) AND ("vm.status" = 'ready'))`,
			},

			// ===== OS/SYSTEM FILTERING =====
			{
				input:  "os.name = 'linux' and os.version != 'ubuntu' and kernel.version >= '5.0'",
				output: `((("os.name" = 'linux') AND ("os.version" != 'ubuntu')) AND ("kernel.version" >= '5.0'))`,
			},

			// ===== ROLE-BASED FILTERING =====
			{
				input:  "active = true and (role = 'admin' or role = 'superuser')",
				output: `(("active" = TRUE) AND (("role" = 'admin') OR ("role" = 'superuser')))`,
			},

			// ===== RESOURCE FILTERING =====
			{
				input:  "vm.memory >= 16GB and vm.disk >= 500GB and vm.cpu.cores >= '8'",
				output: `((("vm.memory" >= 16384.00) AND ("vm.disk" >= 512000.00)) AND ("vm.cpu.cores" >= '8'))`,
			},

			// ===== REGEX WITH EXCLUSION =====
			{
				input:  "vm.name ~ /^prod-/ and vm.name !~ /test/",
				output: `(regexp_matches("vm.name", '^prod-') AND NOT regexp_matches("vm.name", 'test'))`,
			},

			// ===== COMPLEX BOOLEAN LOGIC =====
			{
				input:  "(enabled = true and status = 'active') or (priority = 'critical' and memory >= 32GB)",
				output: `((("enabled" = TRUE) AND ("status" = 'active')) OR (("priority" = 'critical') AND ("memory" >= 32768.00)))`,
			},

			// ===== MIGRATION READINESS CHECK =====
			{
				input:  "vm.status = 'running' and vm.disk < 1TB and (vm.os ~ /linux/ or vm.os ~ /windows/)",
				output: `((("vm.status" = 'running') AND ("vm.disk" < 1048576.00)) AND (regexp_matches("vm.os", 'linux') OR regexp_matches("vm.os", 'windows')))`,
			},

			// ===== DATACENTER SELECTION =====
			{
				input:  "(datacenter = 'DC1' or datacenter = 'DC2') and tier = 'production' and enabled = true",
				output: `(((("datacenter" = 'DC1') OR ("datacenter" = 'DC2')) AND ("tier" = 'production')) AND ("enabled" = TRUE))`,
			},

			// ===== HIGHLY NESTED EXPRESSION =====
			{
				input:  "((a = '1' and b = '2') or (c = '3' and d = '4')) and ((e = '5' or f = '6') and g = '7')",
				output: `(((("a" = '1') AND ("b" = '2')) OR (("c" = '3') AND ("d" = '4'))) AND ((("e" = '5') OR ("f" = '6')) AND ("g" = '7')))`,
			},
		}

		for _, test := range tests {
			test := test
			It("should generate SQL for: "+test.input, func() {
				expr, err := parse([]byte(test.input))
				Expect(err).ToNot(HaveOccurred())
				sql, err := toSqlString(expr, sqlTestMapper)
				Expect(err).ToNot(HaveOccurred())
				Expect(sql).To(Equal(test.output))
			})
		}
	})

	Context("Edge cases and boundary conditions", func() {
		type testCase struct {
			input  string
			output string
		}

		tests := []testCase{
			// ===== ZERO VALUES =====
			{input: "count = 0", output: `("count" = 0.00)`},
			{input: "memory > 0GB", output: `("memory" > 0.00)`},
			{input: "memory > 0MB", output: `("memory" > 0.00)`},
			{input: "memory > 0KB", output: `("memory" > 0.00)`},

			// ===== VERY SMALL VALUES =====
			{input: "ratio = 0.001", output: `("ratio" = 0.00)`},
			{input: "memory > 1KB", output: `("memory" > 0.00)`},

			// ===== VERY LARGE VALUES =====
			{input: "disk > 100TB", output: `("disk" > 104857600.00)`},
			{input: "count > 999999999", output: `("count" > 999999999.00)`},

			// ===== SINGLE CHARACTER STRING =====
			{input: "name = 'x'", output: `("name" = 'x')`},
			{input: "name != 'y'", output: `("name" != 'y')`},

			// ===== LONG IDENTIFIERS =====
			{input: "very.long.nested.identifier.path.to.value = 'test'", output: `("very.long.nested.identifier.path.to.value" = 'test')`},

			// ===== REGEX WITH SPECIAL REGEX CHARS =====
			{input: "name ~ /\\d+/", output: `regexp_matches("name", '\d+')`},
			{input: "name ~ /\\w+/", output: `regexp_matches("name", '\w+')`},
			{input: "name ~ /\\s+/", output: `regexp_matches("name", '\s+')`},
			{input: "name ~ /a\\.b/", output: `regexp_matches("name", 'a\.b')`},
			{input: "name ~ /a\\*b/", output: `regexp_matches("name", 'a\*b')`},

			// ===== MULTIPLE OPERATORS SAME TYPE =====
			{input: "a > 1 and b > 2 and c > 3 and d > 4 and e > 5", output: `((((("a" > 1.00) AND ("b" > 2.00)) AND ("c" > 3.00)) AND ("d" > 4.00)) AND ("e" > 5.00))`},

			// ===== ALL OPERATORS IN ONE EXPRESSION =====
			{
				input:  "a = '1' and b != '2' and c > 3 and d >= 4 and e < 5 and f <= 6 and g ~ /pattern/ and h !~ /excluded/",
				output: `(((((((("a" = '1') AND ("b" != '2')) AND ("c" > 3.00)) AND ("d" >= 4.00)) AND ("e" < 5.00)) AND ("f" <= 6.00)) AND regexp_matches("g", 'pattern')) AND NOT regexp_matches("h", 'excluded'))`,
			},
		}

		for _, test := range tests {
			test := test
			It("should generate SQL for: "+test.input, func() {
				expr, err := parse([]byte(test.input))
				Expect(err).ToNot(HaveOccurred())
				sql, err := toSqlString(expr, sqlTestMapper)
				Expect(err).ToNot(HaveOccurred())
				Expect(sql).To(Equal(test.output))
			})
		}
	})

	Context("LIKE operator (like2)", func() {
		type testCase struct {
			input  string
			output string
		}

		tests := []testCase{
			{input: "name like 'test'", output: `("name" LIKE '%test%')`},
			{input: "name like 'prod-db'", output: `("name" LIKE '%prod-db%')`},
			{input: "name like 'web'", output: `("name" LIKE '%web%')`},
		}

		for _, test := range tests {
			test := test
			It("should generate SQL for: "+test.input, func() {
				expr, err := parse([]byte(test.input))
				Expect(err).ToNot(HaveOccurred())
				sql, err := toSqlString(expr, sqlTestMapper)
				Expect(err).ToNot(HaveOccurred())
				Expect(sql).To(Equal(test.output))
			})
		}

		It("should properly parameterize the like2 value", func() {
			expr, err := parse([]byte("name like 'test'"))
			Expect(err).ToNot(HaveOccurred())
			sqlizer, err := toSql(expr, sqlTestMapper)
			Expect(err).ToNot(HaveOccurred())
			sql, args, err := sqlizer.ToSql()
			Expect(err).ToNot(HaveOccurred())
			Expect(sql).To(Equal(`("name" LIKE ?)`))
			Expect(args).To(Equal([]interface{}{"%test%"}))
		})

		It("should combine like2 with AND", func() {
			expr, err := parse([]byte("name like 'prod' and active = true"))
			Expect(err).ToNot(HaveOccurred())
			sql, err := toSqlString(expr, sqlTestMapper)
			Expect(err).ToNot(HaveOccurred())
			Expect(sql).To(Equal(`(("name" LIKE '%prod%') AND ("active" = TRUE))`))
		})

		It("should combine like2 with OR", func() {
			expr, err := parse([]byte("name like 'web' or name like 'db'"))
			Expect(err).ToNot(HaveOccurred())
			sql, err := toSqlString(expr, sqlTestMapper)
			Expect(err).ToNot(HaveOccurred())
			Expect(sql).To(Equal(`(("name" LIKE '%web%') OR ("name" LIKE '%db%'))`))
		})
	})

	Context("IN operator", func() {
		It("should generate SQL for single value IN", func() {
			expr, err := parse([]byte("status in ['active']"))
			Expect(err).ToNot(HaveOccurred())
			sqlizer, err := toSql(expr, sqlTestMapper)
			Expect(err).ToNot(HaveOccurred())
			sql, args, err := sqlizer.ToSql()
			Expect(err).ToNot(HaveOccurred())
			Expect(sql).To(Equal(`"status" IN (?)`))
			Expect(args).To(Equal([]interface{}{"active"}))
		})

		It("should generate SQL for multiple values IN", func() {
			expr, err := parse([]byte("status in ['active', 'pending', 'running']"))
			Expect(err).ToNot(HaveOccurred())
			sqlizer, err := toSql(expr, sqlTestMapper)
			Expect(err).ToNot(HaveOccurred())
			sql, args, err := sqlizer.ToSql()
			Expect(err).ToNot(HaveOccurred())
			Expect(sql).To(Equal(`"status" IN (?,?,?)`))
			Expect(args).To(Equal([]interface{}{"active", "pending", "running"}))
		})

		It("should generate SQL for IN with AND", func() {
			expr, err := parse([]byte("status in ['active', 'pending'] and memory > 8GB"))
			Expect(err).ToNot(HaveOccurred())
			sqlizer, err := toSql(expr, sqlTestMapper)
			Expect(err).ToNot(HaveOccurred())
			sql, args, err := sqlizer.ToSql()
			Expect(err).ToNot(HaveOccurred())
			Expect(sql).To(Equal(`("status" IN (?,?) AND ("memory" > ?))`))
			Expect(args).To(Equal([]interface{}{"active", "pending", float64(8192)}))
		})

		It("should generate SQL for IN with OR", func() {
			expr, err := parse([]byte("status in ['active'] or name = 'test'"))
			Expect(err).ToNot(HaveOccurred())
			sqlizer, err := toSql(expr, sqlTestMapper)
			Expect(err).ToNot(HaveOccurred())
			sql, args, err := sqlizer.ToSql()
			Expect(err).ToNot(HaveOccurred())
			Expect(sql).To(Equal(`("status" IN (?) OR ("name" = ?))`))
			Expect(args).To(Equal([]interface{}{"active", "test"}))
		})

		It("should handle empty list", func() {
			expr, err := parse([]byte("status in []"))
			Expect(err).ToNot(HaveOccurred())
			sqlizer, err := toSql(expr, sqlTestMapper)
			Expect(err).ToNot(HaveOccurred())
			sql, _, err := sqlizer.ToSql()
			Expect(err).ToNot(HaveOccurred())
			// Empty IN produces (1=0) which is always false
			Expect(sql).To(Equal("(1=0)"))
		})
	})

	Context("NOT IN operator", func() {
		It("should generate SQL for single value NOT IN", func() {
			expr, err := parse([]byte("status not in ['inactive']"))
			Expect(err).ToNot(HaveOccurred())
			sqlizer, err := toSql(expr, sqlTestMapper)
			Expect(err).ToNot(HaveOccurred())
			sql, args, err := sqlizer.ToSql()
			Expect(err).ToNot(HaveOccurred())
			Expect(sql).To(Equal(`"status" NOT IN (?)`))
			Expect(args).To(Equal([]interface{}{"inactive"}))
		})

		It("should generate SQL for multiple values NOT IN", func() {
			expr, err := parse([]byte("status not in ['inactive', 'deleted', 'archived']"))
			Expect(err).ToNot(HaveOccurred())
			sqlizer, err := toSql(expr, sqlTestMapper)
			Expect(err).ToNot(HaveOccurred())
			sql, args, err := sqlizer.ToSql()
			Expect(err).ToNot(HaveOccurred())
			Expect(sql).To(Equal(`"status" NOT IN (?,?,?)`))
			Expect(args).To(Equal([]interface{}{"inactive", "deleted", "archived"}))
		})

		It("should generate SQL for NOT IN with AND", func() {
			expr, err := parse([]byte("status not in ['deleted'] and memory > 4GB"))
			Expect(err).ToNot(HaveOccurred())
			sqlizer, err := toSql(expr, sqlTestMapper)
			Expect(err).ToNot(HaveOccurred())
			sql, args, err := sqlizer.ToSql()
			Expect(err).ToNot(HaveOccurred())
			Expect(sql).To(Equal(`("status" NOT IN (?) AND ("memory" > ?))`))
			Expect(args).To(Equal([]interface{}{"deleted", float64(4096)}))
		})

		It("should handle empty NOT IN list", func() {
			expr, err := parse([]byte("status not in []"))
			Expect(err).ToNot(HaveOccurred())
			sqlizer, err := toSql(expr, sqlTestMapper)
			Expect(err).ToNot(HaveOccurred())
			sql, _, err := sqlizer.ToSql()
			Expect(err).ToNot(HaveOccurred())
			// Empty NOT IN produces (1=1) which is always true
			Expect(sql).To(Equal("(1=1)"))
		})
	})

	Context("defaultMapFn field mappings", func() {
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
				col, err := defaultMapFn(tc.field)
				Expect(err).ToNot(HaveOccurred())
				Expect(col).To(Equal(tc.expect))
			})
		}

		It("should be case-insensitive", func() {
			col, err := defaultMapFn("MEMORY")
			Expect(err).ToNot(HaveOccurred())
			Expect(col).To(Equal(`v."Memory"`))
		})

		It("should return error for unknown field", func() {
			_, err := defaultMapFn("nonexistent")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unknown filter field"))
		})
	})

	Context("groupMapFn field mappings", func() {
		It("should map name", func() {
			col, err := groupMapFn("name")
			Expect(err).ToNot(HaveOccurred())
			Expect(col).To(Equal("name"))
		})

		It("should map description", func() {
			col, err := groupMapFn("description")
			Expect(err).ToNot(HaveOccurred())
			Expect(col).To(Equal("description"))
		})

		It("should map filter", func() {
			col, err := groupMapFn("filter")
			Expect(err).ToNot(HaveOccurred())
			Expect(col).To(Equal("filter"))
		})

		It("should return error for unknown field", func() {
			_, err := groupMapFn("bogus")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unknown group filter field"))
		})
	})

	Context("toSql error paths", func() {
		It("should return error for unknown expression type", func() {
			_, err := toSql(nil, sqlTestMapper)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unknown expression type"))
		})

		It("should propagate map error from inExpression", func() {
			failMapper := func(name string) (string, error) {
				return "", fmt.Errorf("bad field: %s", name)
			}
			expr := &inExpression{
				Left:   &varExpression{Name: "unknown"},
				Values: []string{"a"},
			}
			_, err := toSql(expr, failMapper)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("bad field"))
		})

		It("should propagate map error from left side of binary expression", func() {
			failMapper := func(name string) (string, error) {
				return "", fmt.Errorf("bad field: %s", name)
			}
			expr := &binaryExpression{
				Left:  &varExpression{Name: "unknown"},
				Op:    equal,
				Right: &stringExpression{Value: "x"},
			}
			_, err := toSql(expr, failMapper)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("bad field"))
		})

		It("should propagate map error from right side of binary expression", func() {
			failMapper := func(name string) (string, error) {
				if name == "ok" {
					return `"ok"`, nil
				}
				return "", fmt.Errorf("bad field: %s", name)
			}
			expr := &binaryExpression{
				Left:  &varExpression{Name: "ok"},
				Op:    equal,
				Right: &varExpression{Name: "bad"},
			}
			_, err := toSql(expr, failMapper)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("bad field"))
		})
	})

	Context("Token SQL mapping", func() {
		It("should map AND token to SQL AND", func() {
			Expect(and.Sql()).To(Equal("AND"))
		})

		It("should map OR token to SQL OR", func() {
			Expect(or.Sql()).To(Equal("OR"))
		})

		It("should map equal token to SQL =", func() {
			Expect(equal.Sql()).To(Equal("="))
		})

		It("should map notEqual token to SQL !=", func() {
			Expect(notEqual.Sql()).To(Equal("!="))
		})

		It("should map greater token to SQL >", func() {
			Expect(greater.Sql()).To(Equal(">"))
		})

		It("should map gte token to SQL >=", func() {
			Expect(gte.Sql()).To(Equal(">="))
		})

		It("should map less token to SQL <", func() {
			Expect(less.Sql()).To(Equal("<"))
		})

		It("should map lte token to SQL <=", func() {
			Expect(lte.Sql()).To(Equal("<="))
		})

		It("should return empty string for like token (handled specially)", func() {
			Expect(like.Sql()).To(Equal(""))
		})

		It("should return NOT for notLike token (handled specially)", func() {
			Expect(notLike.Sql()).To(Equal("NOT"))
		})

		It("should map like2 token to SQL LIKE", func() {
			Expect(like2.Sql()).To(Equal("LIKE"))
		})

		It("should return empty string for illegal token", func() {
			Expect(illegal.Sql()).To(Equal(""))
		})

		It("should return empty string for eol token", func() {
			Expect(eol.Sql()).To(Equal(""))
		})
	})
})
