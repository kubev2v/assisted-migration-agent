package filter

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type evaluatorNested struct {
	Datacenter string
}

type evaluatorDisk struct {
	Size int64
}

type evaluatorModel struct {
	ID          string
	Name        string
	PowerState  string
	CpuCount    int32
	MemoryMB    int32
	DiskSize    int64
	IsTemplate  bool
	Labels      []string
	Sizes       []int
	Flags       []bool
	Disks       []evaluatorDisk
	ClusterInfo evaluatorNested
}

var _ = Describe("Evaluator", func() {
	newModel := func() evaluatorModel {
		return evaluatorModel{
			ID:         "vm-1",
			Name:       "web-01",
			PowerState: "poweredOn",
			CpuCount:   4,
			MemoryMB:   8192,
			DiskSize:   6144,
			IsTemplate: false,
			Labels:     []string{"production", "critical"},
			Sizes:      []int{2048, 4096},
			Flags:      []bool{false, true},
			Disks:      []evaluatorDisk{{Size: 2048}, {Size: 4096}},
			ClusterInfo: evaluatorNested{
				Datacenter: "DC1",
			},
		}
	}

	// evaluate parses and evaluates the expression against the model.
	evaluate := func(expression string, model evaluatorModel) (bool, error) {
		expr, err := ParseExpression([]byte(expression))
		Expect(err).NotTo(HaveOccurred())

		return Eval(expr, model)
	}

	Context("Given an expression and a model", func() {
		When("the expression compares values", func() {
			DescribeTable("evaluates the comparison",
				func(expression string, expected bool) {
					// Arrange
					model := newModel()

					// Act
					matched, err := evaluate(expression, model)

					// Assert
					Expect(err).NotTo(HaveOccurred())
					Expect(matched).To(Equal(expected))
				},
				Entry("equal strings", `Name = "web-01"`, true),
				Entry("equal strings with a different value", `Name = "db-01"`, false),
				Entry("not equal strings", `Name != "db-01"`, true),
				Entry("not equal strings with the same value", `Name != "web-01"`, false),
				Entry("greater numbers", `CpuCount > 2`, true),
				Entry("greater numbers with an equal value", `CpuCount > 4`, false),
				Entry("greater or equal numbers", `CpuCount >= 4`, true),
				Entry("less numbers", `CpuCount < 8`, true),
				Entry("less or equal numbers with a smaller value", `CpuCount <= 2`, false),
				Entry("equal booleans", `IsTemplate = false`, true),
				Entry("equal booleans with a different value", `IsTemplate = true`, false),
				Entry("not equal booleans", `IsTemplate != true`, true),
				Entry("greater int64 numbers", `DiskSize > 4096`, true),
			)
		})

		When("the expression uses quantities", func() {
			DescribeTable("evaluates the quantity in MB",
				func(expression string, expected bool) {
					// Arrange
					model := newModel()

					// Act
					matched, err := evaluate(expression, model)

					// Assert
					Expect(err).NotTo(HaveOccurred())
					Expect(matched).To(Equal(expected))
				},
				Entry("equal quantity", `MemoryMB = 8192`, true),
				Entry("greater or equal quantity", `MemoryMB >= 8GB`, true),
				Entry("greater quantity with an equal value", `MemoryMB > 8GB`, false),
				Entry("less quantity", `MemoryMB < 1TB`, true),
				Entry("kilobytes below the field", `MemoryMB > 1024KB`, true),
				Entry("megabytes above the field", `MemoryMB > 1024MB`, true),
			)
		})

		When("the expression combines conditions", func() {
			DescribeTable("evaluates the logical operators",
				func(expression string, expected bool) {
					// Arrange
					model := newModel()

					// Act
					matched, err := evaluate(expression, model)

					// Assert
					Expect(err).NotTo(HaveOccurred())
					Expect(matched).To(Equal(expected))
				},
				Entry("and with both matching", `Name = "web-01" and CpuCount = 4`, true),
				Entry("and with one mismatch", `Name = "web-01" and CpuCount = 8`, false),
				Entry("or with one matching", `Name = "db-01" or CpuCount = 4`, true),
				Entry("or without a match", `Name = "db-01" or CpuCount = 8`, false),
				Entry("and binds tighter than or", `Name = "db-01" or Name = "web-01" and CpuCount = 4`, true),
				Entry("parentheses override precedence", `(Name = "db-01" or Name = "web-01") and CpuCount = 8`, false),
			)
		})

		When("the expression matches a regex", func() {
			DescribeTable("evaluates the regex",
				func(expression string, expected bool) {
					// Arrange
					model := newModel()

					// Act
					matched, err := evaluate(expression, model)

					// Assert
					Expect(err).NotTo(HaveOccurred())
					Expect(matched).To(Equal(expected))
				},
				Entry("matching regex", `Name ~ /^web-/`, true),
				Entry("non-matching regex", `Name ~ /^db-/`, false),
				Entry("negated regex", `Name !~ /^db-/`, true),
				Entry("negated regex that matches", `Name !~ /^web-/`, false),
				Entry("regex with an alternation", `PowerState ~ /poweredOn|poweredOff/`, true),
			)
		})

		When("the expression tests membership", func() {
			DescribeTable("evaluates the string list",
				func(expression string, expected bool) {
					// Arrange
					model := newModel()

					// Act
					matched, err := evaluate(expression, model)

					// Assert
					Expect(err).NotTo(HaveOccurred())
					Expect(matched).To(Equal(expected))
				},
				Entry("in with a match", `Name in ["db-01", "web-01"]`, true),
				Entry("in without a match", `Name in ["db-01", "api-01"]`, false),
				Entry("in with an empty list", `Name in []`, false),
				Entry("not in without a match", `Name not in ["db-01"]`, true),
				Entry("not in with a match", `Name not in ["db-01", "web-01"]`, false),
			)
		})

		When("the expression tests list containment", func() {
			DescribeTable("evaluates the list element",
				func(expression string, expected bool) {
					// Arrange
					model := newModel()

					// Act
					matched, err := evaluate(expression, model)

					// Assert
					Expect(err).NotTo(HaveOccurred())
					Expect(matched).To(Equal(expected))
				},
				Entry("strings contains a matching element", `Labels contains "production"`, true),
				Entry("strings contains a missing element", `Labels contains "test"`, false),
				Entry("strings not contains a missing element", `Labels not contains "test"`, true),
				Entry("strings not contains a matching element", `Labels not contains "critical"`, false),
				Entry("numbers contains a matching element", `Sizes contains "4096"`, true),
				Entry("numbers contains a missing element", `Sizes contains "1024"`, false),
				Entry("booleans contains a matching element", `Flags contains "true"`, true),
				Entry("booleans contains a false element", `Flags contains "false"`, true),
			)
		})

		When("the expression resolves model fields", func() {
			DescribeTable("looks the field up",
				func(expression string, expected bool) {
					// Arrange
					model := newModel()

					// Act
					matched, err := evaluate(expression, model)

					// Assert
					Expect(err).NotTo(HaveOccurred())
					Expect(matched).To(Equal(expected))
				},
				Entry("matches the field name case-insensitively", `name = "web-01"`, true),
				Entry("matches the field name in upper case", `NAME = "web-01"`, true),
				Entry("resolves a dotted path", `ClusterInfo.Datacenter = "DC1"`, true),
				Entry("resolves a dotted path case-insensitively", `clusterinfo.datacenter = "DC1"`, true),
			)
		})
	})

	Context("Given an expression that cannot be evaluated", func() {
		DescribeTable("returns an error",
			func(expression, expectedError string) {
				// Arrange
				model := newModel()

				// Act
				_, err := evaluate(expression, model)

				// Assert
				Expect(err).To(MatchError(ContainSubstring(expectedError)))
			},
			Entry("unknown field", `Unknown = "web-01"`, `unknown field "Unknown"`),
			Entry("type mismatch on equality", `CpuCount = "web-01"`, "cannot compare int32 and string"),
			Entry("type mismatch on ordering", `Name > 4`, "cannot compare string and float64"),
			Entry("type mismatch on boolean", `CpuCount = true`, "cannot compare int32 and bool"),
			Entry("regex on a non-string field", `CpuCount ~ /^4/`, "is int32, not a string"),
			Entry("membership on a non-string field", `CpuCount in ["4"]`, "is int32, not a string"),
			Entry("contains on a non-list field", `Name contains "web"`, "expected a list"),
			Entry("contains with an unparsable element", `Sizes contains "two"`, "cannot parse"),
			Entry("unsupported LIKE operator", `Name like "web%"`, "unsupported operator like2"),
		)
	})

	Context("Given an invalid expression", func() {
		DescribeTable("returns a parse error",
			func(expression string) {
				// Arrange

				// Act
				_, err := ParseExpression([]byte(expression))

				// Assert
				Expect(err).To(HaveOccurred())
			},
			Entry("missing operator", `Name`),
			Entry("missing right value", `Name =`),
			Entry("invalid regex", `Name ~ /(/`),
			Entry("invalid operator", `Name ? "web-01"`),
			Entry("unclosed list", `Name in ["web-01"`),
		)
	})
})
