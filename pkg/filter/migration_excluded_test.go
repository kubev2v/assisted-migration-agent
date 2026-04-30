package filter

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Migration Excluded Filter", func() {
	Context("migration_excluded field mapping", func() {
		// Given the defaultMapFn
		// When migration_excluded is queried
		// Then it should map to the correct SQL expression with BooleanField type
		It("should map migration_excluded to COALESCE expression", func() {
			// Act
			column, fieldType, err := defaultMapFn("migration_excluded")

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(column).To(Equal(`COALESCE(vui.migration_excluded, FALSE)`))
			Expect(fieldType).To(Equal(BooleanField))
		})
	})

	Context("migration_excluded filter expressions", func() {
		type testCase struct {
			input       string
			expectedSQL string
			description string
		}

		tests := []testCase{
			{
				input:       "migration_excluded = true",
				expectedSQL: `(COALESCE(vui.migration_excluded, FALSE) = TRUE)`,
				description: "filter for excluded VMs",
			},
			{
				input:       "migration_excluded = false",
				expectedSQL: `(COALESCE(vui.migration_excluded, FALSE) = FALSE)`,
				description: "filter for included VMs",
			},
			{
				input:       "migration_excluded != true",
				expectedSQL: `(COALESCE(vui.migration_excluded, FALSE) != TRUE)`,
				description: "filter for not excluded VMs using !=",
			},
			{
				input:       "migration_excluded = true and cluster = 'prod'",
				expectedSQL: `((COALESCE(vui.migration_excluded, FALSE) = TRUE) AND (v."Cluster" = 'prod'))`,
				description: "combine migration_excluded with cluster filter",
			},
			{
				input:       "migration_excluded = false and migratable = true",
				expectedSQL: `((COALESCE(vui.migration_excluded, FALSE) = FALSE) AND ((COALESCE(crit.critical_count, 0) = 0) = TRUE))`,
				description: "combine migration_excluded with migratable filter",
			},
			{
				input:       "cluster = 'staging' and migration_excluded = true",
				expectedSQL: `((v."Cluster" = 'staging') AND (COALESCE(vui.migration_excluded, FALSE) = TRUE))`,
				description: "cluster filter before migration_excluded",
			},
			{
				input:       "(migration_excluded = true or cluster = 'test') and powerstate = 'poweredOn'",
				expectedSQL: `(((COALESCE(vui.migration_excluded, FALSE) = TRUE) OR (v."Cluster" = 'test')) AND (v."Powerstate" = 'poweredOn'))`,
				description: "complex expression with OR and AND",
			},
		}

		for _, test := range tests {
			test := test
			It("should generate correct SQL for: "+test.description, func() {
				// Act
				expr, err := parse([]byte(test.input))
				Expect(err).NotTo(HaveOccurred())

				sql, err := toSqlString(expr, defaultMapFn)

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(sql).To(Equal(test.expectedSQL))
			})
		}
	})

	Context("ParseWithDefaultMap integration", func() {
		// Given a migration_excluded filter expression
		// When ParseWithDefaultMap is called
		// Then it should return a valid Sqlizer
		It("should parse migration_excluded = true", func() {
			// Act
			sqlizer, err := ParseWithDefaultMap([]byte("migration_excluded = true"))

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(sqlizer).NotTo(BeNil())

			sql, args, err := sqlizer.ToSql()
			Expect(err).NotTo(HaveOccurred())
			Expect(sql).To(ContainSubstring("COALESCE(vui.migration_excluded, FALSE)"))
			Expect(sql).To(ContainSubstring("= TRUE"))
			Expect(args).To(HaveLen(0)) // Boolean values are embedded directly, not placeholders
		})

		// Given a migration_excluded filter expression
		// When ParseWithDefaultMap is called
		// Then it should return a valid Sqlizer
		It("should parse migration_excluded = false", func() {
			// Act
			sqlizer, err := ParseWithDefaultMap([]byte("migration_excluded = false"))

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(sqlizer).NotTo(BeNil())

			sql, args, err := sqlizer.ToSql()
			Expect(err).NotTo(HaveOccurred())
			Expect(sql).To(ContainSubstring("COALESCE(vui.migration_excluded, FALSE)"))
			Expect(sql).To(ContainSubstring("= FALSE"))
			Expect(args).To(HaveLen(0)) // Boolean values are embedded directly, not placeholders
		})

		// Given a complex filter with migration_excluded
		// When ParseWithDefaultMap is called
		// Then it should parse successfully
		It("should parse complex expression with migration_excluded", func() {
			// Act
			expression := `migration_excluded = false and cluster = "production" and memory >= 8192`
			sqlizer, err := ParseWithDefaultMap([]byte(expression))

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(sqlizer).NotTo(BeNil())

			sql, _, err := sqlizer.ToSql()
			Expect(err).NotTo(HaveOccurred())
			Expect(sql).To(ContainSubstring("COALESCE(vui.migration_excluded, FALSE)"))
			Expect(sql).To(ContainSubstring(`v."Cluster"`))
			Expect(sql).To(ContainSubstring(`v."Memory"`))
		})
	})

	Context("Error handling", func() {
		// Given an invalid boolean value
		// When parsing the expression
		// Then it should return an error
		It("should reject non-boolean values for boolean fields", func() {
			// Act
			sqlizer, err := ParseWithDefaultMap([]byte("migration_excluded = 'invalid'"))

			// Assert - should fail because migration_excluded is a boolean field
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("boolean"))
			Expect(sqlizer).To(BeNil())
		})
	})
})
