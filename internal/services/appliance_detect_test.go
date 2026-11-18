package services_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/services"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

var _ = Describe("ApplianceService Detect", Ordered, func() {
	const (
		ruleName      = "test-rule"
		conditionName = "condition"
		vmID          = "vm-1"

		exactGeometryFunc    = "disk_geometry_exact"
		supersetGeometryFunc = "disk_geometry_superset"
	)

	var (
		ctx    context.Context
		pool   *store.Pool
		sqlDB  *sql.DB
		srv    *services.ApplianceService
		tmpDir string
	)

	BeforeAll(func() {
		ctx = context.Background()

		var err error
		tmpDir, err = os.MkdirTemp("", "appliance-detect-test-*")
		Expect(err).NotTo(HaveOccurred())

		pool = store.NewPool(5 * time.Minute)

		db, err := pool.NewDatabase("collection", filepath.Join(tmpDir, "collection.duckdb"), time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
		Expect(err).NotTo(HaveOccurred())

		Expect(db.Migrate(ctx, func(mCtx context.Context, d *sql.DB) error {
			sqlDB = d
			s, sErr := db.Store()
			if sErr != nil {
				return sErr
			}
			if pErr := duckdb_parser.New(s.Querier(), nil).Init(); pErr != nil {
				return pErr
			}
			return migrations.RunCollection(mCtx, d, "collection")
		})).To(Succeed())

		st, err := db.Store()
		Expect(err).NotTo(HaveOccurred())

		srv, err = services.NewApplianceService(st)
		Expect(err).NotTo(HaveOccurred())
	})

	AfterAll(func() {
		pool.Close()
		if tmpDir != "" {
			_ = os.RemoveAll(tmpDir)
		}
	})

	insertRule := func(rule models.Rule) int {
		var id int
		err := sqlDB.QueryRowContext(ctx, `
			INSERT INTO rules (name, vendor, category, confidence, min_matched, osv_verdict)
			VALUES (?, 'Test', 'Test', ?, ?, ?)
			RETURNING id
		`, rule.Name, int(rule.Confidence), rule.MinMatches, int(rule.OsvVerdict)).Scan(&id)
		Expect(err).NotTo(HaveOccurred())

		return id
	}

	insertExpression := func(ruleID int, name string, tier int, expression string) {
		_, err := sqlDB.ExecContext(ctx, `
			INSERT INTO rule_conditions (rule_id, name, tier, expression)
			VALUES (?, ?, ?, ?)
		`, ruleID, name, tier, expression)
		Expect(err).NotTo(HaveOccurred())
	}

	// seedRule writes a rule the way ListRules reads it back: one row per
	// expression condition.
	seedRule := func(rule models.Rule) {
		ruleID := insertRule(rule)

		for _, condition := range rule.Conditions {
			c, ok := condition.(models.ExpressionCondition)
			if !ok {
				Fail(fmt.Sprintf("unsupported condition type %T", condition))
			}

			insertExpression(ruleID, c.Name, c.Tier, c.Expression)
		}
	}

	// seedFuncRule writes a function rule the way ListRules reads it back: the
	// registered function name in func_name and its arguments in int_value.
	seedFuncRule := func(tier int, funcName string, sizes ...int) {
		ruleID := insertRule(models.Rule{
			Name:       ruleName,
			Vendor:     "Test",
			Category:   "Test",
			OsvVerdict: models.OsvUnknown,
			Confidence: models.ConfidenceHighLevel,
			MinMatches: 1,
		})

		for _, size := range sizes {
			_, err := sqlDB.ExecContext(ctx, `
				INSERT INTO rule_conditions (rule_id, name, tier, func_name, int_value)
				VALUES (?, ?, ?, ?, ?)
			`, ruleID, conditionName, tier, funcName, size)
			Expect(err).NotTo(HaveOccurred())
		}
	}

	expectDetectionError := func() *srvErrors.DetectionError {
		_, _, err := srv.Detect(ctx, "")
		Expect(err).To(HaveOccurred())
		Expect(srvErrors.IsDetectionError(err)).To(BeTrue())

		return err.(*srvErrors.DetectionError)
	}

	newExpressionRule := func(tier int, expression string) models.Rule {
		return models.Rule{
			Name:       ruleName,
			Vendor:     "Test",
			Category:   "Test",
			OsvVerdict: models.OsvUnknown,
			Confidence: models.ConfidenceHighLevel,
			MinMatches: 1,
			Conditions: []any{
				models.ExpressionCondition{Name: conditionName, Tier: tier, Expression: expression},
			},
		}
	}

	Context("Given a collection seeded with a single VM", func() {
		BeforeEach(func() {
			_, err := sqlDB.ExecContext(ctx, `
				INSERT INTO vinfo ("VM ID", "VM", "Powerstate", "Cluster", "CPUs", "Memory", "Template")
				VALUES (?, 'web-01', 'poweredOn', 'cluster-a', 4, 8192, true)
			`, vmID)
			Expect(err).NotTo(HaveOccurred())

			for _, disk := range [][2]any{{"0", 2048}, {"1", 4096}} {
				_, err = sqlDB.ExecContext(ctx, `
					INSERT INTO vdisk ("VM ID", "Disk Key", "Capacity MiB") VALUES (?, ?, ?)
				`, vmID, disk[0], disk[1])
				Expect(err).NotTo(HaveOccurred())
			}
		})

		AfterEach(func() {
			for _, table := range []string{"rule_conditions", "rules", "vdisk", "vinfo"} {
				_, err := sqlDB.ExecContext(ctx, `DELETE FROM `+table)
				Expect(err).NotTo(HaveOccurred())
			}
		})

		Context("When the rule has an expression condition", func() {
			DescribeTable("returns the VM in detected when the expression matches",
				func(expression string) {
					// Arrange
					seedRule(newExpressionRule(1, expression))

					// Act
					detected, notDetected, err := srv.Detect(ctx, "")

					// Assert
					Expect(err).NotTo(HaveOccurred())
					Expect(notDetected).To(BeEmpty())
					Expect(detected).To(ConsistOf(models.DetectionResult{
						VirtualMachineID:  vmID,
						RuleName:          ruleName,
						MatchedConditions: []string{conditionName},
						Confidence:        models.ConfidenceHighLevel,
					}))
				},
				Entry("equal", `Name = "web-01"`),
				Entry("not equal", `Name != "db-01"`),
				Entry("greater or equal", `CpuCount >= 4`),
				Entry("less", `CpuCount < 8`),
				Entry("quantity in MB", `MemoryMB >= 8GB`),
				Entry("boolean", `IsTemplate = true`),
				Entry("regex", `Name ~ /^web-/`),
				Entry("negated regex", `Name !~ /^db-/`),
				Entry("in", `Name in ["db-01", "web-01"]`),
				Entry("not in", `Name not in ["db-01"]`),
				Entry("and", `Name = "web-01" and CpuCount = 4`),
				Entry("or", `Name = "db-01" or CpuCount = 4`),
				Entry("parentheses", `(Name = "db-01" or Name = "web-01") and CpuCount = 4`),
			)

			DescribeTable("returns the VM in notDetected when the expression does not match",
				func(expression string) {
					// Arrange
					seedRule(newExpressionRule(1, expression))

					// Act
					detected, notDetected, err := srv.Detect(ctx, "")

					// Assert
					Expect(err).NotTo(HaveOccurred())
					Expect(detected).To(BeEmpty())
					Expect(notDetected).To(ConsistOf(models.DetectionResult{
						VirtualMachineID:  vmID,
						RuleName:          ruleName,
						MatchedConditions: []string{},
						Confidence:        models.NoConfidence,
					}))
				},
				Entry("equal with a different value", `Name = "db-01"`),
				Entry("greater", `CpuCount > 4`),
				Entry("less or equal", `CpuCount <= 2`),
				Entry("quantity above the field", `MemoryMB > 8GB`),
				Entry("boolean negation", `IsTemplate = false`),
				Entry("regex that does not match", `Name ~ /^db-/`),
				Entry("in without a match", `Name in ["db-01", "api-01"]`),
				Entry("not in with a match", `Name not in ["db-01", "web-01"]`),
				Entry("and with one mismatch", `Name = "web-01" and CpuCount = 8`),
				Entry("or without a match", `Name = "db-01" or CpuCount = 8`),
			)

			It("returns the VM in detected when the expression uses a lower-case field name", func() {
				// Arrange
				seedRule(newExpressionRule(1, `name = "web-01"`))

				// Act
				detected, notDetected, err := srv.Detect(ctx, "")

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(notDetected).To(BeEmpty())
				Expect(detected).To(ConsistOf(models.DetectionResult{
					VirtualMachineID:  vmID,
					RuleName:          ruleName,
					MatchedConditions: []string{conditionName},
					Confidence:        models.ConfidenceHighLevel,
				}))
			})

			It("reports an error when the field does not exist", func() {
				// Arrange
				seedRule(newExpressionRule(1, `Unknown = "web-01"`))

				// Act
				detectionErr := expectDetectionError()

				// Assert
				Expect(detectionErr.ConditionType).To(Equal("expression"))
				Expect(detectionErr.ConditionName).To(Equal(conditionName))
			})

			It("reports an error when the field type does not match the value", func() {
				// Arrange
				seedRule(newExpressionRule(1, `CpuCount = "web-01"`))

				// Act
				detectionErr := expectDetectionError()

				// Assert
				Expect(detectionErr.ConditionType).To(Equal("expression"))
				Expect(detectionErr.ConditionName).To(Equal(conditionName))
			})

			It("reports an error when the regex is invalid", func() {
				// Arrange
				seedRule(newExpressionRule(1, `Name ~ /(/`))

				// Act
				detectionErr := expectDetectionError()

				// Assert
				Expect(detectionErr.ConditionType).To(Equal("expression"))
				Expect(detectionErr.ConditionName).To(Equal(conditionName))
			})
		})

		Context("When the rule has a function condition", func() {
			It("returns the VM in detected when the disk geometry matches exactly", func() {
				// Arrange
				seedFuncRule(1, exactGeometryFunc, 2048, 4096)

				// Act
				detected, notDetected, err := srv.Detect(ctx, "")

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(notDetected).To(BeEmpty())
				Expect(detected).To(ConsistOf(models.DetectionResult{
					VirtualMachineID:  vmID,
					RuleName:          ruleName,
					MatchedConditions: []string{conditionName},
					Confidence:        models.ConfidenceHighLevel,
				}))
			})

			It("returns the VM in detected when the disk values are in a different order", func() {
				// Arrange
				seedFuncRule(1, exactGeometryFunc, 4096, 2048)

				// Act
				detected, notDetected, err := srv.Detect(ctx, "")

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(notDetected).To(BeEmpty())
				Expect(detected).To(ConsistOf(models.DetectionResult{
					VirtualMachineID:  vmID,
					RuleName:          ruleName,
					MatchedConditions: []string{conditionName},
					Confidence:        models.ConfidenceHighLevel,
				}))
			})

			It("returns the VM in notDetected when the disk geometry differs from the VM disks", func() {
				// Arrange
				seedFuncRule(1, exactGeometryFunc, 2048, 8192)

				// Act
				detected, notDetected, err := srv.Detect(ctx, "")

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(detected).To(BeEmpty())
				Expect(notDetected).To(ConsistOf(models.DetectionResult{
					VirtualMachineID:  vmID,
					RuleName:          ruleName,
					MatchedConditions: []string{},
					Confidence:        models.NoConfidence,
				}))
			})

			It("returns the VM in detected when the disk geometry is a superset of the VM disks", func() {
				// Arrange
				seedFuncRule(2, supersetGeometryFunc, 2048, 4096)

				// Act
				detected, notDetected, err := srv.Detect(ctx, "")

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(notDetected).To(BeEmpty())
				Expect(detected).To(ConsistOf(models.DetectionResult{
					VirtualMachineID:  vmID,
					RuleName:          ruleName,
					MatchedConditions: []string{conditionName},
					Confidence:        models.ConfidenceHighLevel,
				}))
			})

			It("returns the VM in notDetected when a superset disk is missing on the VM", func() {
				// Arrange
				seedFuncRule(2, supersetGeometryFunc, 2048, 8192)

				// Act
				detected, notDetected, err := srv.Detect(ctx, "")

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(detected).To(BeEmpty())
				Expect(notDetected).To(ConsistOf(models.DetectionResult{
					VirtualMachineID:  vmID,
					RuleName:          ruleName,
					MatchedConditions: []string{},
					Confidence:        models.NoConfidence,
				}))
			})

			It("fails when the function is not registered", func() {
				// Arrange
				seedFuncRule(1, "unregistered-func", 2048)

				// Act
				_, _, err := srv.Detect(ctx, "")

				// Assert
				Expect(err).To(HaveOccurred())
				Expect(srvErrors.IsDetectionError(err)).To(BeFalse())
				Expect(err.Error()).To(ContainSubstring(`condition function "unregistered-func" is not registered`))
			})

			It("fails when the value count does not match the function arity", func() {
				// Arrange
				seedFuncRule(1, exactGeometryFunc, 2048)

				// Act
				_, _, err := srv.Detect(ctx, "")

				// Assert
				Expect(err).To(HaveOccurred())
				Expect(srvErrors.IsDetectionError(err)).To(BeFalse())
				Expect(err.Error()).To(ContainSubstring("expected func(int, models.ApplianceVirtualMachine) (bool, error)"))
			})
		})

		Context("When the rule confidence is computed", func() {
			DescribeTable("returns the VM in detected with the confidence mapped from the strongest tier",
				func(tier int, expected models.ConfidenceLevel) {
					// Arrange
					seedRule(newExpressionRule(tier, `Name = "web-01"`))

					// Act
					detected, notDetected, err := srv.Detect(ctx, "")

					// Assert
					Expect(err).NotTo(HaveOccurred())
					Expect(notDetected).To(BeEmpty())
					Expect(detected).To(ConsistOf(models.DetectionResult{
						VirtualMachineID:  vmID,
						RuleName:          ruleName,
						MatchedConditions: []string{conditionName},
						Confidence:        expected,
					}))
				},
				Entry("tier 1 is high", 1, models.ConfidenceHighLevel),
				Entry("tier 2 is high", 2, models.ConfidenceHighLevel),
				Entry("tier 3 is medium", 3, models.ConfidenceMediumLevel),
				Entry("tier 4 is medium", 4, models.ConfidenceMediumLevel),
				Entry("tier 5 is low", 5, models.ConfidenceLowerLevel),
			)

			DescribeTable("returns the VM in notDetected when no tier maps to a confidence level",
				func(tier int) {
					// Arrange
					seedRule(newExpressionRule(tier, `Name = "web-01"`))

					// Act
					detected, notDetected, err := srv.Detect(ctx, "")

					// Assert
					Expect(err).NotTo(HaveOccurred())
					Expect(detected).To(BeEmpty())
					Expect(notDetected).To(ConsistOf(models.DetectionResult{
						VirtualMachineID:  vmID,
						RuleName:          ruleName,
						MatchedConditions: []string{conditionName},
						Confidence:        models.NoConfidence,
					}))
				},
				Entry("tier 6 is no confidence", 6),
				Entry("tier 0 is no confidence", 0),
			)

			DescribeTable("caps the confidence of a detected VM with the rule confidence",
				func(ruleConfidence models.ConfidenceLevel, tier int, expected models.ConfidenceLevel) {
					// Arrange
					rule := newExpressionRule(tier, `Name = "web-01"`)
					rule.Confidence = ruleConfidence
					seedRule(rule)

					// Act
					detected, notDetected, err := srv.Detect(ctx, "")

					// Assert
					Expect(err).NotTo(HaveOccurred())
					Expect(notDetected).To(BeEmpty())
					Expect(detected).To(ConsistOf(models.DetectionResult{
						VirtualMachineID:  vmID,
						RuleName:          ruleName,
						MatchedConditions: []string{conditionName},
						Confidence:        expected,
					}))
				},
				Entry("rule high keeps detected high", models.ConfidenceHighLevel, 1, models.ConfidenceHighLevel),
				Entry("rule high does not raise detected medium", models.ConfidenceHighLevel, 3, models.ConfidenceMediumLevel),
				Entry("rule medium caps detected high", models.ConfidenceMediumLevel, 1, models.ConfidenceMediumLevel),
				Entry("rule low caps detected high", models.ConfidenceLowerLevel, 1, models.ConfidenceLowerLevel),
			)

			It("returns the VM in detected when the matched count is exactly the minimum", func() {
				// Arrange
				rule := newExpressionRule(1, `Name = "web-01"`)
				rule.MinMatches = 1
				seedRule(rule)

				// Act
				detected, notDetected, err := srv.Detect(ctx, "")

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(notDetected).To(BeEmpty())
				Expect(detected).To(ConsistOf(models.DetectionResult{
					VirtualMachineID:  vmID,
					RuleName:          ruleName,
					MatchedConditions: []string{conditionName},
					Confidence:        models.ConfidenceHighLevel,
				}))
			})

			It("returns the VM in notDetected when the matched count is below the minimum", func() {
				// Arrange
				rule := newExpressionRule(1, `Name = "web-01"`)
				rule.MinMatches = 2
				seedRule(rule)

				// Act
				detected, notDetected, err := srv.Detect(ctx, "")

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(detected).To(BeEmpty())
				Expect(notDetected).To(ConsistOf(models.DetectionResult{
					VirtualMachineID:  vmID,
					RuleName:          ruleName,
					MatchedConditions: []string{conditionName},
					Confidence:        models.NoConfidence,
				}))
			})

			It("returns the VM in detected using the strongest tier regardless of condition order", func() {
				// Arrange
				seedRule(models.Rule{
					Name:       ruleName,
					Vendor:     "Test",
					Category:   "Test",
					OsvVerdict: models.OsvUnknown,
					Confidence: models.ConfidenceHighLevel,
					MinMatches: 2,
					Conditions: []any{
						models.ExpressionCondition{Name: "first", Tier: 5, Expression: `Name = "web-01"`},
						models.ExpressionCondition{Name: "second", Tier: 1, Expression: `PowerState = "poweredOn"`},
					},
				})

				// Act
				detected, notDetected, err := srv.Detect(ctx, "")

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(notDetected).To(BeEmpty())
				Expect(detected).To(HaveLen(1))

				// MatchedConditions is built from a map, so its order is not stable.
				Expect(detected[0].VirtualMachineID).To(Equal(vmID))
				Expect(detected[0].RuleName).To(Equal(ruleName))
				Expect(detected[0].MatchedConditions).To(ConsistOf("first", "second"))
				Expect(detected[0].Confidence).To(Equal(models.ConfidenceHighLevel))
			})

			It("returns the VM in notDetected when only one of the required conditions matches", func() {
				// Arrange
				seedRule(models.Rule{
					Name:       ruleName,
					Vendor:     "Test",
					Category:   "Test",
					OsvVerdict: models.OsvUnknown,
					Confidence: models.ConfidenceHighLevel,
					MinMatches: 2,
					Conditions: []any{
						models.ExpressionCondition{Name: "first", Tier: 1, Expression: `Name = "web-01"`},
						models.ExpressionCondition{Name: "second", Tier: 1, Expression: `PowerState = "poweredOff"`},
					},
				})

				// Act
				detected, notDetected, err := srv.Detect(ctx, "")

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(detected).To(BeEmpty())
				Expect(notDetected).To(ConsistOf(models.DetectionResult{
					VirtualMachineID:  vmID,
					RuleName:          ruleName,
					MatchedConditions: []string{"first"},
					Confidence:        models.NoConfidence,
				}))
			})

			It("does not raise the confidence of a detected VM when more low-tier conditions match", func() {
				// Arrange
				seedRule(models.Rule{
					Name:       ruleName,
					Vendor:     "Test",
					Category:   "Test",
					OsvVerdict: models.OsvUnknown,
					Confidence: models.ConfidenceHighLevel,
					MinMatches: 3,
					Conditions: []any{
						models.ExpressionCondition{Name: "name", Tier: 5, Expression: `Name = "web-01"`},
						models.ExpressionCondition{Name: "power", Tier: 5, Expression: `PowerState = "poweredOn"`},
						models.ExpressionCondition{Name: "cluster", Tier: 5, Expression: `Cluster = "cluster-a"`},
					},
				})

				// Act
				detected, notDetected, err := srv.Detect(ctx, "")

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(notDetected).To(BeEmpty())
				Expect(detected).To(HaveLen(1))

				// MatchedConditions is built from a map, so its order is not stable.
				Expect(detected[0].VirtualMachineID).To(Equal(vmID))
				Expect(detected[0].RuleName).To(Equal(ruleName))
				Expect(detected[0].MatchedConditions).To(ConsistOf("name", "power", "cluster"))
				Expect(detected[0].Confidence).To(Equal(models.ConfidenceLowerLevel))
			})

			It("returns the VM in detected with the strongest tier when low and medium conditions match", func() {
				// Arrange
				seedRule(models.Rule{
					Name:       ruleName,
					Vendor:     "Test",
					Category:   "Test",
					OsvVerdict: models.OsvUnknown,
					Confidence: models.ConfidenceHighLevel,
					MinMatches: 2,
					Conditions: []any{
						models.ExpressionCondition{Name: "name", Tier: 5, Expression: `Name = "web-01"`},
						models.ExpressionCondition{Name: "cluster", Tier: 3, Expression: `Cluster = "cluster-a"`},
					},
				})

				// Act
				detected, notDetected, err := srv.Detect(ctx, "")

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(notDetected).To(BeEmpty())
				Expect(detected).To(HaveLen(1))

				// MatchedConditions is built from a map, so its order is not stable.
				Expect(detected[0].VirtualMachineID).To(Equal(vmID))
				Expect(detected[0].RuleName).To(Equal(ruleName))
				Expect(detected[0].MatchedConditions).To(ConsistOf("name", "cluster"))
				Expect(detected[0].Confidence).To(Equal(models.ConfidenceMediumLevel))
			})
		})
	})

	Context("Given a collection without VMs", func() {
		AfterEach(func() {
			for _, table := range []string{"rule_conditions", "rules"} {
				_, err := sqlDB.ExecContext(ctx, `DELETE FROM `+table)
				Expect(err).NotTo(HaveOccurred())
			}
		})

		When("the rules are detected", func() {
			It("should return no results in either bucket", func() {
				// Arrange
				seedRule(newExpressionRule(1, `Name = "web-01"`))

				// Act
				detected, notDetected, err := srv.Detect(ctx, "")

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(detected).To(BeEmpty())
				Expect(notDetected).To(BeEmpty())
			})
		})
	})
})
