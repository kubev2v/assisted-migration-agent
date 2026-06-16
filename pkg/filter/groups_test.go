package filter

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Groups Filter", func() {
	Context("Parser - contains operator", func() {
		type testCase struct {
			input  string
			output string
		}

		tests := []testCase{
			// ===== CONTAINS OPERATOR =====
			{input: "groups contains 'production-vms'", output: `(groups CONTAINS "production-vms")`},
			{input: "groups contains 'test'", output: `(groups CONTAINS "test")`},
			{input: `groups contains "staging"`, output: `(groups CONTAINS "staging")`},

			// ===== NOT CONTAINS OPERATOR =====
			{input: "groups not contains 'production-vms'", output: `(groups NOT CONTAINS "production-vms")`},
			{input: "groups not contains 'test'", output: `(groups NOT CONTAINS "test")`},

			// ===== COMBINED WITH AND =====
			{input: "name = 'vm1' and groups contains 'production-vms'", output: `((name equal "vm1") and (groups CONTAINS "production-vms"))`},
			{input: "groups contains 'critical' and cluster = 'prod'", output: `((groups CONTAINS "critical") and (cluster equal "prod"))`},

			// ===== COMBINED WITH OR =====
			{input: "groups contains 'production-vms' or groups contains 'staging'", output: `((groups CONTAINS "production-vms") or (groups CONTAINS "staging"))`},
			{input: "name = 'vm1' or groups contains 'test'", output: `((name equal "vm1") or (groups CONTAINS "test"))`},

			// ===== MIXED CONTAINS AND NOT CONTAINS =====
			{input: "groups contains 'production-vms' and groups not contains 'test'", output: `((groups CONTAINS "production-vms") and (groups NOT CONTAINS "test"))`},

			// ===== WITH PARENTHESES =====
			{input: "(groups contains 'production-vms' or groups contains 'staging') and cluster = 'prod'", output: `(((groups CONTAINS "production-vms") or (groups CONTAINS "staging")) and (cluster equal "prod"))`},

			// ===== COMBINED WITH LABELS =====
			{input: "groups contains 'critical' and labels contains 'production'", output: `((groups CONTAINS "critical") and (labels CONTAINS "production"))`},
		}

		for _, test := range tests {
			test := test
			It("should parse: "+test.input, func() {
				expr, err := parse([]byte(test.input))
				Expect(err).ToNot(HaveOccurred())
				Expect(expr.String()).To(Equal(test.output))
			})
		}
	})

	Context("SQL Generation - contains operator", func() {
		type testCase struct {
			input  string
			output string
		}

		tests := []testCase{
			// ===== CONTAINS OPERATOR =====
			{input: "groups contains 'production-vms'", output: `list_contains(CAST("groups" AS VARCHAR[]), 'production-vms')`},
			{input: "groups contains 'test'", output: `list_contains(CAST("groups" AS VARCHAR[]), 'test')`},
			{input: "groups contains 'staging'", output: `list_contains(CAST("groups" AS VARCHAR[]), 'staging')`},

			// ===== NOT CONTAINS OPERATOR =====
			{input: "groups not contains 'production-vms'", output: `("groups" IS NULL OR NOT list_contains(CAST("groups" AS VARCHAR[]), 'production-vms'))`},
			{input: "groups not contains 'test'", output: `("groups" IS NULL OR NOT list_contains(CAST("groups" AS VARCHAR[]), 'test'))`},

			// ===== COMBINED WITH AND =====
			{input: "name = 'vm1' and groups contains 'production-vms'", output: `(("name" = 'vm1') AND list_contains(CAST("groups" AS VARCHAR[]), 'production-vms'))`},
			{input: "groups contains 'critical' and cluster = 'prod'", output: `(list_contains(CAST("groups" AS VARCHAR[]), 'critical') AND ("cluster" = 'prod'))`},

			// ===== COMBINED WITH OR =====
			{input: "groups contains 'production-vms' or groups contains 'staging'", output: `(list_contains(CAST("groups" AS VARCHAR[]), 'production-vms') OR list_contains(CAST("groups" AS VARCHAR[]), 'staging'))`},

			// ===== MIXED CONTAINS AND NOT CONTAINS =====
			{input: "groups contains 'production-vms' and groups not contains 'test'", output: `(list_contains(CAST("groups" AS VARCHAR[]), 'production-vms') AND ("groups" IS NULL OR NOT list_contains(CAST("groups" AS VARCHAR[]), 'test')))`},
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

	Context("SQL Generation with defaultMapFn", func() {
		It("should map groups to g.groups column with CAST", func() {
			expr, err := parse([]byte("groups contains 'production-vms'"))
			Expect(err).ToNot(HaveOccurred())
			sqlizer, err := toSql(expr, defaultMapFn)
			Expect(err).ToNot(HaveOccurred())
			sql, args, err := sqlizer.ToSql()
			Expect(err).ToNot(HaveOccurred())
			Expect(sql).To(Equal(`list_contains(CAST(g.groups AS VARCHAR[]), ?)`))
			Expect(args).To(Equal([]interface{}{"production-vms"}))
		})

		It("should handle groups not contains with defaultMapFn", func() {
			expr, err := parse([]byte("groups not contains 'test'"))
			Expect(err).ToNot(HaveOccurred())
			sqlizer, err := toSql(expr, defaultMapFn)
			Expect(err).ToNot(HaveOccurred())
			sql, args, err := sqlizer.ToSql()
			Expect(err).ToNot(HaveOccurred())
			Expect(sql).To(Equal(`(g.groups IS NULL OR NOT list_contains(CAST(g.groups AS VARCHAR[]), ?))`))
			Expect(args).To(Equal([]interface{}{"test"}))
		})
	})
})
