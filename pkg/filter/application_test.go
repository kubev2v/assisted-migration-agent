package filter

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Application Filter", func() {
	Context("application field mapping", func() {
		It("should map 'application' to va.app_name as StringField", func() {
			column, fieldType, err := defaultMapFn("application")
			Expect(err).NotTo(HaveOccurred())
			Expect(column).To(Equal(`va.app_name`))
			Expect(fieldType).To(Equal(StringField))
		})

		It("should map 'application.name' to va.app_name as StringField", func() {
			column, fieldType, err := defaultMapFn("application.name")
			Expect(err).NotTo(HaveOccurred())
			Expect(column).To(Equal(`va.app_name`))
			Expect(fieldType).To(Equal(StringField))
		})

		It("should map 'application.description' to va.app_desc as StringField", func() {
			column, fieldType, err := defaultMapFn("application.description")
			Expect(err).NotTo(HaveOccurred())
			Expect(column).To(Equal(`va.app_desc`))
			Expect(fieldType).To(Equal(StringField))
		})
	})

	Context("application filter expressions", func() {
		type testCase struct {
			input       string
			expectedSQL string
			description string
		}

		tests := []testCase{
			{
				input:       "application = 'PostgreSQL'",
				expectedSQL: `(va.app_name = 'PostgreSQL')`,
				description: "exact match by application name",
			},
			{
				input:       "application != 'MySQL'",
				expectedSQL: `(va.app_name != 'MySQL')`,
				description: "not equal by application name",
			},
			{
				input:       "application ~ /Oracle.*/",
				expectedSQL: `regexp_matches(va.app_name, 'Oracle.*')`,
				description: "regex match by application name",
			},
			{
				input:       "application.description ~ /.*database.*/",
				expectedSQL: `regexp_matches(va.app_desc, '.*database.*')`,
				description: "regex match by application description",
			},
			{
				input:       "application = 'Apache' and cluster = 'prod'",
				expectedSQL: `((va.app_name = 'Apache') AND (v."Cluster" = 'prod'))`,
				description: "combine application with cluster filter",
			},
		}

		for _, test := range tests {
			test := test
			It("should generate correct SQL for: "+test.description, func() {
				expr, err := parse([]byte(test.input))
				Expect(err).NotTo(HaveOccurred())

				sql, err := toSqlString(expr, defaultMapFn)

				Expect(err).NotTo(HaveOccurred())
				Expect(sql).To(Equal(test.expectedSQL))
			})
		}
	})

	Context("application filter with in operator", func() {
		It("should parse application in list", func() {
			sqlizer, err := ParseWithDefaultMap([]byte("application in ['PostgreSQL', 'MySQL']"))

			Expect(err).NotTo(HaveOccurred())
			Expect(sqlizer).NotTo(BeNil())

			sql, args, err := sqlizer.ToSql()
			Expect(err).NotTo(HaveOccurred())
			Expect(sql).To(ContainSubstring("va.app_name"))
			Expect(args).To(ConsistOf("PostgreSQL", "MySQL"))
		})
	})

	Context("error handling", func() {
		It("should reject non-string values for application field", func() {
			sqlizer, err := ParseWithDefaultMap([]byte("application = true"))

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("string"))
			Expect(sqlizer).To(BeNil())
		})
	})
})
