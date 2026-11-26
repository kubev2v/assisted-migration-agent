package services_test

import (
	"context"
	"database/sql"
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
)

var _ = Describe("ApplianceService Detect", Ordered, func() {
	const (
		ruleName = "test-rule"
		vmID     = "vm-1"
	)

	var (
		ctx       context.Context
		pool      *store.Pool
		sqlDB     *sql.DB
		mainStore *store.Store
		rulesSrv  *services.ApplianceRulesService
		srv       *services.ApplianceService
		tmpDir    string
	)

	BeforeAll(func() {
		ctx = context.Background()

		var err error
		tmpDir, err = os.MkdirTemp("", "appliance-detect-test-*")
		Expect(err).NotTo(HaveOccurred())

		pool = store.NewPool(5 * time.Minute)

		db, err := pool.NewDatabase("collection", filepath.Join(tmpDir, "collection.duckdb"), time.Now(), store.EagerConnectionInitilization, store.ReadWriteDatabase)
		Expect(err).To(BeNil())
		mainDB, err := pool.NewDatabase(store.MainDatabaseID, filepath.Join(tmpDir, "agent.duckdb"), time.Now(), store.EagerConnectionInitilization, store.ReadWriteDatabase)
		Expect(err).NotTo(HaveOccurred())

		Expect(mainDB.Migrate(ctx, migrations.RunMain)).To(Succeed())
		pool.Add(mainDB)

		mainStore, err = mainDB.Store()
		Expect(err).NotTo(HaveOccurred())

		rulesSrv, err = services.NewApplianceRulesService(mainStore)
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

		srv, err = services.NewApplianceService(st, rulesSrv)
		Expect(err).NotTo(HaveOccurred())
	})

	AfterAll(func() {
		pool.Close()
		if tmpDir != "" {
			_ = os.RemoveAll(tmpDir)
		}
	})

	// Rules are created through the real rules service, so detection only sees
	// rules the product can store. The condition name also carries the tier.
	createRule := func(rule models.Rule) {
		_, err := rulesSrv.Create(ctx, rule)
		Expect(err).NotTo(HaveOccurred())
	}

	// clearRules empties the rule tables in the main database. Children go first
	// because DuckDB has no cascading deletes and the tables carry no foreign keys.
	clearRules := func() {
		for _, statement := range []string{
			`DELETE FROM disk_geometry_condition`,
			`DELETE FROM rule_conditions`,
			`DELETE FROM rules`,
		} {
			_, err := mainStore.Querier().ExecContext(ctx, statement)
			Expect(err).NotTo(HaveOccurred())
		}
	}

	Context("Given a collection seeded with a single VM", func() {
		BeforeEach(func() {
			_, err := sqlDB.ExecContext(ctx, `
				INSERT INTO vinfo ("VM ID", "VM", "Powerstate", "Cluster", "CPUs", "Memory")
				VALUES (?, 'web-01', 'poweredOn', 'cluster-a', 4, 8192)
			`, vmID)
			Expect(err).NotTo(HaveOccurred())

			_, err = sqlDB.ExecContext(ctx, `
				INSERT INTO vdisk ("VM ID", "Disk Key", "Capacity MiB") VALUES (?, '0', 2048)
			`, vmID)
			Expect(err).NotTo(HaveOccurred())

			_, err = sqlDB.ExecContext(ctx, `
				INSERT INTO vdisk ("VM ID", "Disk Key", "Capacity MiB") VALUES (?, '1', 4096)
			`, vmID)
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			clearRules()

			_, err := sqlDB.ExecContext(ctx, `DELETE FROM vdisk`)
			Expect(err).NotTo(HaveOccurred())

			_, err = sqlDB.ExecContext(ctx, `DELETE FROM vinfo`)
			Expect(err).NotTo(HaveOccurred())
		})

		Context("When the rule has an expression condition", func() {
			// Given a rule with one expression condition and a VM the expression matches
			// When detection runs
			// Then the VM is reported as detected with the condition name that matched
			DescribeTable("returns the VM in detected when the expression matches",
				func(expression string) {
					// Arrange
					createRule(models.Rule{
						Name:       ruleName,
						Vendor:     "Test",
						Category:   "Test",
						OsvVerdict: models.OsvUnknown,
						Confidence: models.ConfidenceHighLevel,
						MinMatches: 1,
						Conditions: []any{models.ConditionInput{Name: "annotation", Expression: expression}},
					})

					// Act
					detected, notDetected, err := srv.Detect(ctx, "")

					// Assert
					Expect(err).NotTo(HaveOccurred())
					Expect(notDetected).To(BeEmpty())
					Expect(detected).To(ConsistOf(models.DetectionResult{
						VirtualMachineID:  vmID,
						RuleName:          ruleName,
						MatchedConditions: []string{"annotation"},
						Confidence:        models.ConfidenceHighLevel,
					}))
				},
				Entry("equal", `Name = "web-01"`),
				Entry("not equal", `Name != "db-01"`),
				Entry("greater or equal", `CpuCount >= 4`),
				Entry("less", `CpuCount < 8`),
				Entry("quantity in MB", `MemoryMB >= 8GB`),
				Entry("regex", `Name ~ /^web-/`),
				Entry("negated regex", `Name !~ /^db-/`),
				Entry("in", `Name in ["db-01", "web-01"]`),
				Entry("not in", `Name not in ["db-01"]`),
				Entry("and", `Name = "web-01" and CpuCount = 4`),
				Entry("or", `Name = "db-01" or CpuCount = 4`),
				Entry("parentheses", `(Name = "db-01" or Name = "web-01") and CpuCount = 4`),
			)

			// Given a rule with one expression condition and a VM the expression does not match
			// When detection runs
			// Then the VM is reported as not detected with no matched condition
			DescribeTable("returns the VM in notDetected when the expression does not match",
				func(expression string) {
					// Arrange
					createRule(models.Rule{
						Name:       ruleName,
						Vendor:     "Test",
						Category:   "Test",
						OsvVerdict: models.OsvUnknown,
						Confidence: models.ConfidenceHighLevel,
						MinMatches: 1,
						Conditions: []any{models.ConditionInput{Name: "annotation", Expression: expression}},
					})

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
				Entry("regex that does not match", `Name ~ /^db-/`),
				Entry("in without a match", `Name in ["db-01", "api-01"]`),
				Entry("not in with a match", `Name not in ["db-01", "web-01"]`),
				Entry("and with one mismatch", `Name = "web-01" and CpuCount = 8`),
				Entry("or without a match", `Name = "db-01" or CpuCount = 8`),
			)

			// Given a rule whose expression spells the attribute in lower case
			// When detection runs
			// Then the VM is reported as detected, because attribute names are case insensitive
			It("returns the VM in detected when the expression uses a lower-case field name", func() {
				// Arrange
				createRule(models.Rule{
					Name:       ruleName,
					Vendor:     "Test",
					Category:   "Test",
					OsvVerdict: models.OsvUnknown,
					Confidence: models.ConfidenceHighLevel,
					MinMatches: 1,
					Conditions: []any{models.ConditionInput{Name: "annotation", Expression: `name = "web-01"`}},
				})

				// Act
				detected, notDetected, err := srv.Detect(ctx, "")

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(notDetected).To(BeEmpty())
				Expect(detected).To(ConsistOf(models.DetectionResult{
					VirtualMachineID:  vmID,
					RuleName:          ruleName,
					MatchedConditions: []string{"annotation"},
					Confidence:        models.ConfidenceHighLevel,
				}))
			})
		})

		Context("When the rule has a function condition", func() {
			// Given a rule whose disk geometry equals the VM disks in exact mode
			// When detection runs
			// Then the VM is reported as detected through the disk geometry condition
			It("returns the VM in detected when the disk geometry matches exactly", func() {
				// Arrange
				createRule(models.Rule{
					Name:       ruleName,
					Vendor:     "Test",
					Category:   "Test",
					OsvVerdict: models.OsvUnknown,
					Confidence: models.ConfidenceHighLevel,
					MinMatches: 1,
					Conditions: []any{models.ConditionInput{
						Name: "disk_geometry",
						Function: &models.FunctionInput{
							Name:         "disk_geometry",
							DiskGeometry: &models.DiskGeometry{Disks: []int64{2048, 4096}, Mode: models.DiskGeometryExact},
						},
					}},
				})

				// Act
				detected, notDetected, err := srv.Detect(ctx, "")

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(notDetected).To(BeEmpty())
				Expect(detected).To(ConsistOf(models.DetectionResult{
					VirtualMachineID:  vmID,
					RuleName:          ruleName,
					MatchedConditions: []string{"disk_geometry"},
					Confidence:        models.ConfidenceLowerLevel,
				}))
			})

			// Given an exact disk geometry that lists the same sizes in another order
			// When detection runs
			// Then the VM is reported as detected, because exact mode compares the set of sizes
			It("returns the VM in detected when the disk values are in a different order", func() {
				// Arrange
				createRule(models.Rule{
					Name:       ruleName,
					Vendor:     "Test",
					Category:   "Test",
					OsvVerdict: models.OsvUnknown,
					Confidence: models.ConfidenceHighLevel,
					MinMatches: 1,
					Conditions: []any{models.ConditionInput{
						Name: "disk_geometry",
						Function: &models.FunctionInput{
							Name:         "disk_geometry",
							DiskGeometry: &models.DiskGeometry{Disks: []int64{4096, 2048}, Mode: models.DiskGeometryExact},
						},
					}},
				})

				// Act
				detected, notDetected, err := srv.Detect(ctx, "")

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(notDetected).To(BeEmpty())
				Expect(detected).To(ConsistOf(models.DetectionResult{
					VirtualMachineID:  vmID,
					RuleName:          ruleName,
					MatchedConditions: []string{"disk_geometry"},
					Confidence:        models.ConfidenceLowerLevel,
				}))
			})

			// Given an exact disk geometry with a size the VM does not have
			// When detection runs
			// Then the VM is reported as not detected
			It("returns the VM in notDetected when the disk geometry differs from the VM disks", func() {
				// Arrange
				createRule(models.Rule{
					Name:       ruleName,
					Vendor:     "Test",
					Category:   "Test",
					OsvVerdict: models.OsvUnknown,
					Confidence: models.ConfidenceHighLevel,
					MinMatches: 1,
					Conditions: []any{models.ConditionInput{
						Name: "disk_geometry",
						Function: &models.FunctionInput{
							Name:         "disk_geometry",
							DiskGeometry: &models.DiskGeometry{Disks: []int64{2048, 8192}, Mode: models.DiskGeometryExact},
						},
					}},
				})

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

			// Given a superset disk geometry whose sizes are all present on the VM
			// When detection runs
			// Then the VM is reported as detected
			It("returns the VM in detected when the disk geometry is a superset of the VM disks", func() {
				// Arrange
				createRule(models.Rule{
					Name:       ruleName,
					Vendor:     "Test",
					Category:   "Test",
					OsvVerdict: models.OsvUnknown,
					Confidence: models.ConfidenceHighLevel,
					MinMatches: 1,
					Conditions: []any{models.ConditionInput{
						Name: "disk_geometry",
						Function: &models.FunctionInput{
							Name:         "disk_geometry",
							DiskGeometry: &models.DiskGeometry{Disks: []int64{2048, 4096}, Mode: models.DiskGeometrySuperset},
						},
					}},
				})

				// Act
				detected, notDetected, err := srv.Detect(ctx, "")

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(notDetected).To(BeEmpty())
				Expect(detected).To(ConsistOf(models.DetectionResult{
					VirtualMachineID:  vmID,
					RuleName:          ruleName,
					MatchedConditions: []string{"disk_geometry"},
					Confidence:        models.ConfidenceLowerLevel,
				}))
			})

			// Given a superset disk geometry with one size the VM does not have
			// When detection runs
			// Then the VM is reported as not detected
			It("returns the VM in notDetected when a superset disk is missing on the VM", func() {
				// Arrange
				createRule(models.Rule{
					Name:       ruleName,
					Vendor:     "Test",
					Category:   "Test",
					OsvVerdict: models.OsvUnknown,
					Confidence: models.ConfidenceHighLevel,
					MinMatches: 1,
					Conditions: []any{models.ConditionInput{
						Name: "disk_geometry",
						Function: &models.FunctionInput{
							Name:         "disk_geometry",
							DiskGeometry: &models.DiskGeometry{Disks: []int64{2048, 8192}, Mode: models.DiskGeometrySuperset},
						},
					}},
				})

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

			// Given an exact disk geometry whose size is off by exactly the tolerance
			// When detection runs
			// Then the VM is reported as detected, because the tolerance is inclusive
			It("returns the VM in detected when the size differs by exactly the tolerance", func() {
				// Arrange
				createRule(models.Rule{
					Name:       ruleName,
					Vendor:     "Test",
					Category:   "Test",
					OsvVerdict: models.OsvUnknown,
					Confidence: models.ConfidenceHighLevel,
					MinMatches: 1,
					Conditions: []any{models.ConditionInput{
						Name: "disk_geometry",
						Function: &models.FunctionInput{
							Name:         "disk_geometry",
							DiskGeometry: &models.DiskGeometry{Disks: []int64{2148, 4096}, Tolerance: 100, Mode: models.DiskGeometryExact},
						},
					}},
				})

				// Act
				detected, notDetected, err := srv.Detect(ctx, "")

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(notDetected).To(BeEmpty())
				Expect(detected).To(ConsistOf(models.DetectionResult{
					VirtualMachineID:  vmID,
					RuleName:          ruleName,
					MatchedConditions: []string{"disk_geometry"},
					Confidence:        models.ConfidenceLowerLevel,
				}))
			})

			// Given an exact disk geometry whose size is off by more than the tolerance
			// When detection runs
			// Then the VM is reported as not detected
			It("returns the VM in notDetected when the size differs by more than the tolerance", func() {
				// Arrange
				createRule(models.Rule{
					Name:       ruleName,
					Vendor:     "Test",
					Category:   "Test",
					OsvVerdict: models.OsvUnknown,
					Confidence: models.ConfidenceHighLevel,
					MinMatches: 1,
					Conditions: []any{models.ConditionInput{
						Name: "disk_geometry",
						Function: &models.FunctionInput{
							Name:         "disk_geometry",
							DiskGeometry: &models.DiskGeometry{Disks: []int64{2149, 4096}, Tolerance: 100, Mode: models.DiskGeometryExact},
						},
					}},
				})

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

			// Given an exact disk geometry with fewer disks than the VM, a tolerance wide enough to cover the difference
			// When detection runs
			// Then the VM is reported as not detected, because exact mode also counts the disks
			It("returns the VM in notDetected when the disk count differs even within the tolerance", func() {
				// Arrange
				createRule(models.Rule{
					Name:       ruleName,
					Vendor:     "Test",
					Category:   "Test",
					OsvVerdict: models.OsvUnknown,
					Confidence: models.ConfidenceHighLevel,
					MinMatches: 1,
					Conditions: []any{models.ConditionInput{
						Name: "disk_geometry",
						Function: &models.FunctionInput{
							Name:         "disk_geometry",
							DiskGeometry: &models.DiskGeometry{Disks: []int64{2048}, Tolerance: 10000, Mode: models.DiskGeometryExact},
						},
					}},
				})

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

			// Given a superset disk geometry whose size is within the tolerance
			// When detection runs
			// Then the VM is reported as detected, and its extra disk is allowed
			It("returns the VM in detected when a superset disk is within the tolerance", func() {
				// Arrange
				createRule(models.Rule{
					Name:       ruleName,
					Vendor:     "Test",
					Category:   "Test",
					OsvVerdict: models.OsvUnknown,
					Confidence: models.ConfidenceHighLevel,
					MinMatches: 1,
					Conditions: []any{models.ConditionInput{
						Name: "disk_geometry",
						Function: &models.FunctionInput{
							Name:         "disk_geometry",
							DiskGeometry: &models.DiskGeometry{Disks: []int64{1998}, Tolerance: 100, Mode: models.DiskGeometrySuperset},
						},
					}},
				})

				// Act
				detected, notDetected, err := srv.Detect(ctx, "")

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(notDetected).To(BeEmpty())
				Expect(detected).To(ConsistOf(models.DetectionResult{
					VirtualMachineID:  vmID,
					RuleName:          ruleName,
					MatchedConditions: []string{"disk_geometry"},
					Confidence:        models.ConfidenceLowerLevel,
				}))
			})
		})

		Context("When the rule confidence is computed", func() {
			// Given a rule with one condition of a known name
			// When detection runs and the condition matches
			// Then the confidence reported for the VM is the one the condition name maps to
			DescribeTable("returns the VM in detected with the confidence mapped from the strongest tier",
				func(condition string, expected models.ConfidenceLevel) {
					// Arrange
					createRule(models.Rule{
						Name:       ruleName,
						Vendor:     "Test",
						Category:   "Test",
						OsvVerdict: models.OsvUnknown,
						Confidence: models.ConfidenceHighLevel,
						MinMatches: 1,
						Conditions: []any{models.ConditionInput{Name: condition, Expression: `Name = "web-01"`}},
					})

					// Act
					detected, notDetected, err := srv.Detect(ctx, "")

					// Assert
					Expect(err).NotTo(HaveOccurred())
					Expect(notDetected).To(BeEmpty())
					Expect(detected).To(ConsistOf(models.DetectionResult{
						VirtualMachineID:  vmID,
						RuleName:          ruleName,
						MatchedConditions: []string{condition},
						Confidence:        expected,
					}))
				},
				Entry("vapp product is high", "vapp_product", models.ConfidenceHighLevel),
				Entry("annotation is high", "annotation", models.ConfidenceHighLevel),
				Entry("guest id is medium", "guest_id", models.ConfidenceMediumLevel),
				Entry("mount preffix is medium", "mount_preffix", models.ConfidenceMediumLevel),
				Entry("nic count is low", "nic_count", models.ConfidenceLowerLevel),
			)

			// Given a rule whose condition name maps to no confidence
			// When detection runs and the condition matches
			// Then the VM is reported as not detected, and the condition is listed as matched
			It("returns the VM in notDetected when the condition tier maps to no confidence", func() {
				// Arrange
				createRule(models.Rule{
					Name:       ruleName,
					Vendor:     "Test",
					Category:   "Test",
					OsvVerdict: models.OsvUnknown,
					Confidence: models.ConfidenceHighLevel,
					MinMatches: 1,
					Conditions: []any{models.ConditionInput{Name: "name", Expression: `Name = "web-01"`}},
				})

				// Act
				detected, notDetected, err := srv.Detect(ctx, "")

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(detected).To(BeEmpty())
				Expect(notDetected).To(ConsistOf(models.DetectionResult{
					VirtualMachineID:  vmID,
					RuleName:          ruleName,
					MatchedConditions: []string{"name"},
					Confidence:        models.NoConfidence,
				}))
			})

			// Given a rule with one condition and a rule level confidence
			// When detection runs and the condition matches
			// Then the reported confidence is the lower of the rule confidence and the condition tier
			DescribeTable("caps the confidence of a detected VM with the rule confidence",
				func(ruleConfidence models.ConfidenceLevel, condition string, expected models.ConfidenceLevel) {
					// Arrange
					createRule(models.Rule{
						Name:       ruleName,
						Vendor:     "Test",
						Category:   "Test",
						OsvVerdict: models.OsvUnknown,
						Confidence: ruleConfidence,
						MinMatches: 1,
						Conditions: []any{models.ConditionInput{Name: condition, Expression: `Name = "web-01"`}},
					})

					// Act
					detected, notDetected, err := srv.Detect(ctx, "")

					// Assert
					Expect(err).NotTo(HaveOccurred())
					Expect(notDetected).To(BeEmpty())
					Expect(detected).To(ConsistOf(models.DetectionResult{
						VirtualMachineID:  vmID,
						RuleName:          ruleName,
						MatchedConditions: []string{condition},
						Confidence:        expected,
					}))
				},
				Entry("rule high keeps detected high", models.ConfidenceHighLevel, "vapp_product", models.ConfidenceHighLevel),
				Entry("rule high does not raise detected medium", models.ConfidenceHighLevel, "guest_id", models.ConfidenceMediumLevel),
				Entry("rule medium caps detected high", models.ConfidenceMediumLevel, "vapp_product", models.ConfidenceMediumLevel),
				Entry("rule low caps detected high", models.ConfidenceLowerLevel, "vapp_product", models.ConfidenceLowerLevel),
			)

			// Given a rule that requires one match and a VM that matches one condition
			// When detection runs
			// Then the VM is reported as detected, because the minimum is reached
			It("returns the VM in detected when the matched count is exactly the minimum", func() {
				// Arrange
				createRule(models.Rule{
					Name:       ruleName,
					Vendor:     "Test",
					Category:   "Test",
					OsvVerdict: models.OsvUnknown,
					Confidence: models.ConfidenceHighLevel,
					MinMatches: 1,
					Conditions: []any{models.ConditionInput{Name: "annotation", Expression: `Name = "web-01"`}},
				})

				// Act
				detected, notDetected, err := srv.Detect(ctx, "")

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(notDetected).To(BeEmpty())
				Expect(detected).To(ConsistOf(models.DetectionResult{
					VirtualMachineID:  vmID,
					RuleName:          ruleName,
					MatchedConditions: []string{"annotation"},
					Confidence:        models.ConfidenceHighLevel,
				}))
			})

			// Given a rule that requires two matches and a VM that matches one condition
			// When detection runs
			// Then the VM is reported as not detected, and the matched condition is still listed
			It("returns the VM in notDetected when the matched count is below the minimum", func() {
				// Arrange
				createRule(models.Rule{
					Name:       ruleName,
					Vendor:     "Test",
					Category:   "Test",
					OsvVerdict: models.OsvUnknown,
					Confidence: models.ConfidenceHighLevel,
					MinMatches: 2,
					Conditions: []any{models.ConditionInput{Name: "annotation", Expression: `Name = "web-01"`}},
				})

				// Act
				detected, notDetected, err := srv.Detect(ctx, "")

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(detected).To(BeEmpty())
				Expect(notDetected).To(ConsistOf(models.DetectionResult{
					VirtualMachineID:  vmID,
					RuleName:          ruleName,
					MatchedConditions: []string{"annotation"},
					Confidence:        models.NoConfidence,
				}))
			})

			// Given a rule whose conditions are listed from the weakest tier to the strongest
			// When detection runs and both conditions match
			// Then the VM is reported as detected with the confidence of the strongest tier
			It("returns the VM in detected using the strongest tier regardless of condition order", func() {
				// Arrange
				createRule(models.Rule{
					Name:       ruleName,
					Vendor:     "Test",
					Category:   "Test",
					OsvVerdict: models.OsvUnknown,
					Confidence: models.ConfidenceHighLevel,
					MinMatches: 2,
					Conditions: []any{
						models.ConditionInput{Name: "nic_count", Expression: `Name = "web-01"`},
						models.ConditionInput{Name: "vapp_product", Expression: `PowerState = "poweredOn"`},
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
				Expect(detected[0].MatchedConditions).To(ConsistOf("nic_count", "vapp_product"))
				Expect(detected[0].Confidence).To(Equal(models.ConfidenceHighLevel))
			})

			// Given a rule that requires two matches and a VM that matches one of them
			// When detection runs
			// Then the VM is reported as not detected with only the matched condition listed
			It("returns the VM in notDetected when only one of the required conditions matches", func() {
				// Arrange
				createRule(models.Rule{
					Name:       ruleName,
					Vendor:     "Test",
					Category:   "Test",
					OsvVerdict: models.OsvUnknown,
					Confidence: models.ConfidenceHighLevel,
					MinMatches: 2,
					Conditions: []any{
						models.ConditionInput{Name: "vapp_product", Expression: `Name = "web-01"`},
						models.ConditionInput{Name: "annotation", Expression: `PowerState = "poweredOff"`},
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
					MatchedConditions: []string{"vapp_product"},
					Confidence:        models.NoConfidence,
				}))
			})

			// Given a rule whose conditions all belong to the weakest tier
			// When detection runs and all of them match
			// Then the VM is reported as detected with the weakest confidence, however many matched
			It("does not raise the confidence of a detected VM when more low-tier conditions match", func() {
				// Arrange
				createRule(models.Rule{
					Name:       ruleName,
					Vendor:     "Test",
					Category:   "Test",
					OsvVerdict: models.OsvUnknown,
					Confidence: models.ConfidenceHighLevel,
					MinMatches: 3,
					Conditions: []any{
						models.ConditionInput{Name: "nic_count", Expression: `Name = "web-01"`},
						models.ConditionInput{Name: "nested_hv", Expression: `PowerState = "poweredOn"`},
						models.ConditionInput{Name: "disk_geometry", Expression: `DiskSize >= 2048`},
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
				Expect(detected[0].MatchedConditions).To(ConsistOf("nic_count", "nested_hv", "disk_geometry"))
				Expect(detected[0].Confidence).To(Equal(models.ConfidenceLowerLevel))
			})

			// Given a rule with one weak and one medium condition
			// When detection runs and both match
			// Then the VM is reported as detected with the medium confidence
			It("returns the VM in detected with the strongest tier when low and medium conditions match", func() {
				// Arrange
				createRule(models.Rule{
					Name:       ruleName,
					Vendor:     "Test",
					Category:   "Test",
					OsvVerdict: models.OsvUnknown,
					Confidence: models.ConfidenceHighLevel,
					MinMatches: 2,
					Conditions: []any{
						models.ConditionInput{Name: "nic_count", Expression: `Name = "web-01"`},
						models.ConditionInput{Name: "guest_id", Expression: `PowerState = "poweredOn"`},
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
				Expect(detected[0].MatchedConditions).To(ConsistOf("nic_count", "guest_id"))
				Expect(detected[0].Confidence).To(Equal(models.ConfidenceMediumLevel))
			})
		})
	})

	Context("Given a collection without VMs", func() {
		AfterEach(func() {
			clearRules()
		})

		When("the rules are detected", func() {
			// Given a rule that would match and a collection without any VM
			// When detection runs
			// Then neither bucket holds a result
			It("should return no results in either bucket", func() {
				// Arrange
				createRule(models.Rule{
					Name:       ruleName,
					Vendor:     "Test",
					Category:   "Test",
					OsvVerdict: models.OsvUnknown,
					Confidence: models.ConfidenceHighLevel,
					MinMatches: 1,
					Conditions: []any{models.ConditionInput{Name: "annotation", Expression: `Name = "web-01"`}},
				})

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
