package services_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/services"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

var _ = Describe("ApplianceRulesService", Ordered, func() {
	var (
		ctx    context.Context
		s      *store.Store
		sqlDB  *sql.DB
		pool   *store.Pool
		srv    *services.ApplianceRulesService
		tmpDir string
	)

	BeforeAll(func() {
		ctx = context.Background()

		var err error
		tmpDir, err = os.MkdirTemp("", "appliance-rule-service-test-*")
		Expect(err).NotTo(HaveOccurred())

		pool = store.NewPool(5 * time.Minute)

		mainDB, dbErr := pool.NewDatabase(store.MainDatabaseID, filepath.Join(tmpDir, "agent.duckdb"), time.Now(), store.EagerConnectionInitilization, store.ReadWriteDatabase)
		Expect(dbErr).NotTo(HaveOccurred())

		Expect(mainDB.Migrate(ctx, func(mCtx context.Context, d *sql.DB) error {
			sqlDB = d
			return migrations.RunMain(mCtx, d)
		})).To(Succeed())

		pool.Add(mainDB)

		s, err = mainDB.Store()
		Expect(err).NotTo(HaveOccurred())

		srv, err = services.NewApplianceRulesService(s)
		Expect(err).NotTo(HaveOccurred())
	})

	AfterAll(func() {
		if pool != nil {
			pool.Close()
		}
		if tmpDir != "" {
			_ = os.RemoveAll(tmpDir)
		}
	})

	// clearRules empties the rule tables. Children go first because DuckDB has no
	// cascading deletes and the tables carry no foreign keys.
	clearRules := func() {
		for _, statement := range []string{
			`DELETE FROM disk_geometry_condition`,
			`DELETE FROM rule_conditions`,
			`DELETE FROM rules`,
		} {
			_, err := sqlDB.ExecContext(ctx, statement)
			Expect(err).NotTo(HaveOccurred())
		}
	}

	Context("Given an agent without rules", func() {
		AfterEach(func() {
			clearRules()
		})

		When("the rules are listed", func() {
			// Given no stored rules
			// When the rule list is requested
			// Then the list is empty and no error is returned
			It("should return an empty list", func() {
				// Act
				rules, err := srv.List(ctx)

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(rules).To(BeEmpty())
			})
		})

		When("an expression rule is created", func() {
			// Given a rule with one expression condition named annotation
			// When the rule is created
			// Then the stored condition carries the tier the service assigns to that name
			It("should return the rule with the tier assigned by the service", func() {
				// Arrange
				rule := models.Rule{
					Name:       "annotation-rule",
					Vendor:     "VMware",
					Category:   "Backup",
					OsvVerdict: models.OsvSupported,
					Confidence: models.ConfidenceHighLevel,
					MinMatches: 1,
					Conditions: []any{
						models.ConditionInput{Name: "annotation", Expression: `Annotation = "appliance"`},
					},
				}

				// Act
				created, err := srv.Create(ctx, rule)

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(created.ID).To(BeNumerically(">", 0))
				Expect(created.Conditions).To(HaveLen(1))

				condition, ok := created.Conditions[0].(models.ExpressionCondition)
				Expect(ok).To(BeTrue())
				Expect(condition.Name).To(Equal("annotation"))
				Expect(condition.Expression).To(Equal(`Annotation = "appliance"`))
				Expect(condition.Tier).To(Equal(2))
			})

			// Given a rule that asks to be stored as a default rule
			// When the rule is created
			// Then it is stored as a custom rule, because the kind is owned by the server
			It("should drop the client supplied kind", func() {
				// Arrange
				rule := models.Rule{
					Name:       "default-rule",
					Vendor:     "VMware",
					RuleKind:   models.DefaultRule,
					Category:   "Backup",
					OsvVerdict: models.OsvSupported,
					Confidence: models.ConfidenceHighLevel,
					MinMatches: 1,
					Conditions: []any{
						models.ConditionInput{Name: "name", Expression: `Name = "vm-01"`},
					},
				}

				// Act
				created, err := srv.Create(ctx, rule)

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(created.RuleKind).To(Equal(models.CustomRule))
			})

			// Given a rule whose name, vendor and category are padded with spaces
			// When the rule is created
			// Then the stored values are trimmed
			It("should trim the text fields", func() {
				// Arrange
				rule := models.Rule{
					Name:       "  padded-rule  ",
					Vendor:     "  VMware  ",
					Category:   "  Backup  ",
					OsvVerdict: models.OsvSupported,
					Confidence: models.ConfidenceHighLevel,
					MinMatches: 1,
					Conditions: []any{
						models.ConditionInput{Name: "name", Expression: `Name = "vm-01"`},
					},
				}

				// Act
				created, err := srv.Create(ctx, rule)

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(created.Name).To(Equal("padded-rule"))
				Expect(created.Vendor).To(Equal("VMware"))
				Expect(created.Category).To(Equal("Backup"))
			})
		})

		When("a disk geometry rule is created", func() {
			// Given a disk geometry condition in exact mode
			// When the rule is created
			// Then the condition is bound to the exact predicate and keeps its disks, mode and tier
			It("should store the exact predicate for the exact mode", func() {
				// Arrange
				rule := models.Rule{
					Name:       "disk-exact",
					Vendor:     "VMware",
					Category:   "Backup",
					OsvVerdict: models.OsvSupported,
					Confidence: models.ConfidenceHighLevel,
					MinMatches: 1,
					Conditions: []any{
						models.ConditionInput{
							Name: "disk_geometry",
							Function: &models.FunctionInput{
								Name:         "disk_geometry",
								DiskGeometry: &models.DiskGeometry{Disks: []int64{2048, 4096}, Mode: models.DiskGeometryExact},
							},
						},
					},
				}

				// Act
				created, err := srv.Create(ctx, rule)

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(created.Conditions).To(HaveLen(1))

				condition, ok := created.Conditions[0].(models.FuncCondition[models.DiskGeometry])
				Expect(ok).To(BeTrue())
				Expect(condition.Name).To(Equal("disk_geometry"))
				Expect(condition.FuncName).To(Equal("disk_geometry_exact"))
				Expect(condition.Data).To(Equal(models.DiskGeometry{Disks: []int64{2048, 4096}, Mode: models.DiskGeometryExact}))
				Expect(condition.Tier).To(Equal(5))
				Expect(condition.Func).NotTo(BeNil())
			})

			// Given a disk geometry condition in superset mode with a tolerance
			// When the rule is created
			// Then the condition is bound to the superset predicate and keeps its disk, mode, tolerance and tier
			It("should store the superset predicate for the superset mode", func() {
				// Arrange
				rule := models.Rule{
					Name:       "disk-superset",
					Vendor:     "VMware",
					Category:   "Backup",
					OsvVerdict: models.OsvSupported,
					Confidence: models.ConfidenceHighLevel,
					MinMatches: 1,
					Conditions: []any{
						models.ConditionInput{
							Name: "disk_geometry",
							Function: &models.FunctionInput{
								Name:         "disk_geometry",
								DiskGeometry: &models.DiskGeometry{Disks: []int64{2048}, Tolerance: 30, Mode: models.DiskGeometrySuperset},
							},
						},
					},
				}

				// Act
				created, err := srv.Create(ctx, rule)

				// Assert
				Expect(err).NotTo(HaveOccurred())

				condition, ok := created.Conditions[0].(models.FuncCondition[models.DiskGeometry])
				Expect(ok).To(BeTrue())
				Expect(condition.Name).To(Equal("disk_geometry"))
				Expect(condition.FuncName).To(Equal("disk_geometry_superset"))
				Expect(condition.Data).To(Equal(models.DiskGeometry{Disks: []int64{2048}, Tolerance: 30, Mode: models.DiskGeometrySuperset}))
				Expect(condition.Tier).To(Equal(5))
				Expect(condition.Func).NotTo(BeNil())
			})
		})

		// Given a rule with a single condition the service cannot resolve
		// When the rule is created
		// Then it fails as an invalid rule and the message names the condition and the reason
		DescribeTable("rejects a condition the service cannot resolve",
			func(condition models.ConditionInput, message string) {
				// Arrange
				rule := models.Rule{
					Name:       "invalid-rule",
					Vendor:     "VMware",
					Category:   "Backup",
					OsvVerdict: models.OsvSupported,
					Confidence: models.ConfidenceHighLevel,
					MinMatches: 1,
					Conditions: []any{condition},
				}

				// Act
				_, err := srv.Create(ctx, rule)

				// Assert
				Expect(err).To(HaveOccurred())
				Expect(srvErrors.IsInvalidRuleError(err)).To(BeTrue())
				Expect(err.Error()).To(ContainSubstring("condition[0]"))
				Expect(err.Error()).To(ContainSubstring(message))
			},
			Entry("unknown condition name",
				models.ConditionInput{Name: "bogus", Expression: `Name = "vm-01"`},
				`unknown condition "bogus"`),
			Entry("unknown function name",
				models.ConditionInput{Name: "disk_geometry", Function: &models.FunctionInput{Name: "bogus"}},
				`unknown function "bogus"`),
			Entry("function without a payload",
				models.ConditionInput{Name: "disk_geometry", Function: &models.FunctionInput{Name: "disk_geometry"}},
				"requires a disk_geometry payload"),
			Entry("unknown disk geometry mode",
				models.ConditionInput{Name: "disk_geometry", Function: &models.FunctionInput{Name: "disk_geometry", DiskGeometry: &models.DiskGeometry{Disks: []int64{2048}, Mode: "fuzzy"}}},
				`unknown disk geometry mode "fuzzy"`),
			Entry("invalid expression syntax",
				models.ConditionInput{Name: "name", Expression: "Name ="},
				"invalid expression"),
			Entry("invalid regex literal",
				models.ConditionInput{Name: "name", Expression: `Name ~ /(/`},
				"invalid expression"),
			Entry("expression on an attribute rules cannot match",
				models.ConditionInput{Name: "name", Expression: `Bogus = "vm-01"`},
				"cannot be evaluated"),
			Entry("condition with neither expression nor function",
				models.ConditionInput{Name: "name"},
				"either expression or function must be set"),
		)

		When("a missing rule is read", func() {
			// Given no rule with the requested id
			// When that rule is read
			// Then a resource not found error is returned
			It("should return a resource not found error", func() {
				// Act
				_, err := srv.Get(ctx, 999)

				// Assert
				Expect(err).To(HaveOccurred())
				Expect(srvErrors.IsResourceNotFoundError(err)).To(BeTrue())
			})
		})
	})

	// A rule expression is validated by evaluating it against an empty
	// models.ApplianceVirtualMachine and throwing the result away. These specs
	// prove only that an expression is accepted or rejected; whether it matches a
	// real virtual machine is covered by the detection specs.
	Context("Given rule expressions", func() {
		AfterEach(func() {
			clearRules()
		})

		// The DSL looks up attribute names without regard to case, so every
		// attribute appears once with its own spelling and once in lower case.
		//
		// Given a rule with one expression condition
		// When the rule is created
		// Then the expression is accepted and stored as written together with its tier
		DescribeTable("accepts an expression it can evaluate",
			func(expression string) {
				// Arrange
				rule := models.Rule{
					Name:       "expression-rule",
					Vendor:     "VMware",
					Category:   "Backup",
					OsvVerdict: models.OsvSupported,
					Confidence: models.ConfidenceHighLevel,
					MinMatches: 1,
					Conditions: []any{
						models.ConditionInput{Name: "annotation", Expression: expression},
					},
				}

				// Act
				created, err := srv.Create(ctx, rule)

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(created.Conditions).To(HaveLen(1))

				condition, ok := created.Conditions[0].(models.ExpressionCondition)
				Expect(ok).To(BeTrue())
				Expect(condition.Expression).To(Equal(expression))
				Expect(condition.Tier).To(Equal(2))
			},
			Entry("ID", `ID = "vm-1"`),
			Entry("ID in lower case", `id = "vm-1"`),
			Entry("Name", `Name = "web-01"`),
			Entry("Name in lower case", `name = "web-01"`),
			Entry("Annotation", `Annotation ~ /^appliance/`),
			Entry("Annotation in lower case", `annotation ~ /^appliance/`),
			Entry("Folder", `Folder != "prod"`),
			Entry("Folder in lower case", `folder != "prod"`),
			Entry("Host", `Host in ["esx-1", "esx-2"]`),
			Entry("Host in lower case", `host in ["esx-1", "esx-2"]`),
			Entry("UUID", `UUID not in ["4210", "4220"]`),
			Entry("UUID in lower case", `uuid not in ["4210", "4220"]`),
			Entry("Firmware", `Firmware = "efi"`),
			Entry("Firmware in lower case", `firmware = "efi"`),
			Entry("PowerState", `PowerState = "poweredOn"`),
			Entry("PowerState in lower case", `powerstate = "poweredOn"`),
			Entry("ConnectionState", `ConnectionState = "connected"`),
			Entry("ConnectionState in lower case", `connectionstate = "connected"`),
			Entry("GuestID", `GuestID ~ /_64Guest$/`),
			Entry("GuestID in lower case", `guestid ~ /_64Guest$/`),
			Entry("NestedHVEnabled", `NestedHVEnabled = true`),
			Entry("NestedHVEnabled in lower case", `nestedhvenabled = true`),
			Entry("CpuCount", `CpuCount >= 4`),
			Entry("CpuCount in lower case", `cpucount >= 4`),
			Entry("CoresPerSocket", `CoresPerSocket < 8`),
			Entry("CoresPerSocket in lower case", `corespersocket < 8`),
			Entry("MemoryMB", `MemoryMB > 8GB`),
			Entry("MemoryMB in lower case", `memorymb > 8GB`),
			Entry("DiskSize", `DiskSize <= 1024`),
			Entry("DiskSize in lower case", `disksize <= 1024`),
			Entry("NICCount", `NICCount = 2`),
			Entry("NICCount in lower case", `niccount = 2`),
			Entry("Datacenter", `Datacenter = "dc-1"`),
			Entry("Datacenter in lower case", `datacenter = "dc-1"`),
			Entry("Cluster", `Cluster = "cluster-a"`),
			Entry("Cluster in lower case", `cluster = "cluster-a"`),
			Entry("HostName", `HostName = "web-01.corp"`),
			Entry("HostName in lower case", `hostname = "web-01.corp"`),
			Entry("IPAddress", `IPAddress = "10.0.0.5"`),
			Entry("IPAddress in lower case", `ipaddress = "10.0.0.5"`),
			Entry("CpuCount in mixed case", `cPuCount >= 4`),
		)

		// Given a rule with an expression the DSL cannot evaluate against a virtual machine
		// When the rule is created
		// Then it fails as an invalid rule and the message names the condition and the reason
		DescribeTable("rejects an expression it cannot evaluate",
			func(expression string) {
				// Arrange
				rule := models.Rule{
					Name:       "invalid-rule",
					Vendor:     "VMware",
					Category:   "Backup",
					OsvVerdict: models.OsvSupported,
					Confidence: models.ConfidenceHighLevel,
					MinMatches: 1,
					Conditions: []any{
						models.ConditionInput{Name: "annotation", Expression: expression},
					},
				}

				// Act
				_, err := srv.Create(ctx, rule)

				// Assert
				Expect(err).To(HaveOccurred())
				Expect(srvErrors.IsInvalidRuleError(err)).To(BeTrue())
				Expect(err.Error()).To(ContainSubstring("condition[0]"))
				Expect(err.Error()).To(ContainSubstring("cannot be evaluated"))
			},
			Entry("a number compared with a text attribute", `Name = 4`),
			Entry("a quoted string compared with a numeric attribute", `CpuCount = "4"`),
			Entry("a number compared with a boolean attribute", `NestedHVEnabled = 4`),
			Entry("in with a list of strings on a numeric attribute", `CpuCount in ["4"]`),
			Entry("a regex used on a numeric attribute", `CpuCount ~ /4/`),
			Entry("an attribute the virtual machine does not have", `Bogus = "vm-01"`),
		)

		// Given a rule whose first condition is valid and whose second one is not
		// When the rule is created
		// Then the error names condition 1, not condition 0, and nothing is stored
		It("should name the second condition when it cannot be evaluated", func() {
			// Arrange
			rule := models.Rule{
				Name:       "two-conditions",
				Vendor:     "VMware",
				Category:   "Backup",
				OsvVerdict: models.OsvSupported,
				Confidence: models.ConfidenceHighLevel,
				MinMatches: 2,
				Conditions: []any{
					models.ConditionInput{Name: "annotation", Expression: `Name = "web-01"`},
					models.ConditionInput{Name: "annotation", Expression: `Name = 4`},
				},
			}

			// Act
			_, err := srv.Create(ctx, rule)

			// Assert
			Expect(srvErrors.IsInvalidRuleError(err)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("condition[1]"))

			var rules int
			Expect(sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM rules`).Scan(&rules)).To(Succeed())
			Expect(rules).To(BeZero())
		})
	})

	Context("Given an agent with a stored rule", func() {
		var ruleID int64

		BeforeEach(func() {
			created, err := srv.Create(ctx, models.Rule{
				Name:       "stored-rule",
				Vendor:     "VMware",
				Category:   "Backup",
				OsvVerdict: models.OsvSupported,
				Confidence: models.ConfidenceHighLevel,
				MinMatches: 1,
				Conditions: []any{
					models.ConditionInput{Name: "name", Expression: `Name = "vm-01"`},
					models.ConditionInput{Name: "annotation", Expression: `Annotation = "appliance"`},
				},
			})
			Expect(err).NotTo(HaveOccurred())

			ruleID = created.ID
		})

		AfterEach(func() {
			clearRules()
		})

		When("the rule is read by id", func() {
			// Given a stored rule with two conditions
			// When the rule is read by id
			// Then the rule and both of its conditions are returned
			It("should return the rule with its conditions", func() {
				// Act
				rule, err := srv.Get(ctx, ruleID)

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(rule.ID).To(Equal(ruleID))
				Expect(rule.Name).To(Equal("stored-rule"))
				Expect(rule.Conditions).To(HaveLen(2))
			})
		})

		When("the rule is updated", func() {
			// Given a stored rule with two conditions
			// When it is updated with a single condition
			// Then only that condition is stored and the call reports an update, not a creation
			It("should replace the whole condition set", func() {
				// Arrange
				updated := models.Rule{
					ID:         ruleID,
					Name:       "stored-rule",
					Vendor:     "VMware",
					Category:   "Backup",
					OsvVerdict: models.OsvSupported,
					Confidence: models.ConfidenceHighLevel,
					MinMatches: 1,
					Conditions: []any{
						models.ConditionInput{Name: "guest_id", Expression: `GuestID = "rhel9_64Guest"`},
					},
				}

				// Act
				rule, created, err := srv.Update(ctx, updated)

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(created).To(BeFalse())
				Expect(rule.ID).To(Equal(ruleID))
				Expect(rule.Conditions).To(HaveLen(1))

				condition, ok := rule.Conditions[0].(models.ExpressionCondition)
				Expect(ok).To(BeTrue())
				Expect(condition.Name).To(Equal("guest_id"))
				Expect(condition.Tier).To(Equal(3))
			})

			// Given a stored rule of the default kind
			// When it is updated with a rule that asks for the custom kind
			// Then the stored kind is still default, because an update never changes it
			It("should keep the kind of the stored rule", func() {
				// Arrange
				var defaultRuleID int64
				err := sqlDB.QueryRowContext(ctx, `
					INSERT INTO rules (name, vendor, kind, category, confidence, min_matched, osv_verdict)
					VALUES ('default-kind', 'VMware', 0, 'Backup', 3, 1, 2)
					RETURNING id
				`).Scan(&defaultRuleID)
				Expect(err).NotTo(HaveOccurred())

				// A rule is only visible to the read path when it has at least
				// one condition, so the seeded rule needs one.
				_, err = sqlDB.ExecContext(ctx, `
					INSERT INTO rule_conditions (name, expression, tier, rule_id)
					VALUES ('name', 'Name = "vm-01"', 6, ?)
				`, defaultRuleID)
				Expect(err).NotTo(HaveOccurred())

				updated := models.Rule{
					ID:         defaultRuleID,
					Name:       "default-kind",
					Vendor:     "VMware",
					RuleKind:   models.CustomRule,
					Category:   "Backup",
					OsvVerdict: models.OsvSupported,
					Confidence: models.ConfidenceHighLevel,
					MinMatches: 1,
					Conditions: []any{
						models.ConditionInput{Name: "name", Expression: `Name = "vm-02"`},
					},
				}

				// Act
				rule, created, err := srv.Update(ctx, updated)

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(created).To(BeFalse())
				Expect(rule.RuleKind).To(Equal(models.DefaultRule))
			})

			// Given a stored rule with two expression conditions
			// When it is updated with a disk geometry condition
			// Then one condition with one payload row is stored and the previous rows are gone
			It("should replace the expression conditions with a disk geometry payload", func() {
				// Arrange
				updated := models.Rule{
					ID:         ruleID,
					Name:       "stored-rule",
					Vendor:     "VMware",
					Category:   "Backup",
					OsvVerdict: models.OsvSupported,
					Confidence: models.ConfidenceHighLevel,
					MinMatches: 1,
					Conditions: []any{
						models.ConditionInput{
							Name: "disk_geometry",
							Function: &models.FunctionInput{
								Name:         "disk_geometry",
								DiskGeometry: &models.DiskGeometry{Disks: []int64{1024, 2048}, Mode: models.DiskGeometryExact},
							},
						},
					},
				}

				// Act
				rule, created, err := srv.Update(ctx, updated)

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(created).To(BeFalse())
				Expect(rule.Conditions).To(HaveLen(1))

				condition, ok := rule.Conditions[0].(models.FuncCondition[models.DiskGeometry])
				Expect(ok).To(BeTrue())
				Expect(condition.FuncName).To(Equal("disk_geometry_exact"))
				Expect(condition.Data).To(Equal(models.DiskGeometry{Disks: []int64{1024, 2048}, Mode: models.DiskGeometryExact}))

				// The two conditions of the stored rule are gone with their
				// payloads; only the new couple is left.
				var conditions int
				Expect(sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM rule_conditions`).Scan(&conditions)).To(Succeed())
				Expect(conditions).To(Equal(1))

				var payloads int
				Expect(sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM disk_geometry_condition`).Scan(&payloads)).To(Succeed())
				Expect(payloads).To(Equal(1))
			})
		})

		When("a rule that does not exist is updated", func() {
			// Given no rule with the requested id
			// When a rule with that id is updated
			// Then it is created with a new id and the call reports a creation
			It("should create it and report that it was created", func() {
				// Arrange
				missing := models.Rule{
					ID:         999,
					Name:       "upserted-rule",
					Vendor:     "VMware",
					Category:   "Backup",
					OsvVerdict: models.OsvSupported,
					Confidence: models.ConfidenceHighLevel,
					MinMatches: 1,
					Conditions: []any{
						models.ConditionInput{Name: "name", Expression: `Name = "vm-03"`},
					},
				}

				// Act
				rule, created, err := srv.Update(ctx, missing)

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(created).To(BeTrue())
				Expect(rule.ID).NotTo(Equal(999))
				Expect(rule.ID).To(BeNumerically(">", 0))
				Expect(rule.RuleKind).To(Equal(models.CustomRule))
				Expect(rule.Name).To(Equal("upserted-rule"))
			})
		})

		When("the rule is deleted", func() {
			// Given a stored rule with two conditions
			// When the rule is deleted
			// Then no rule row and no condition rows remain
			It("should remove the rule and its conditions", func() {
				// Act
				err := srv.Delete(ctx, ruleID)

				// Assert
				Expect(err).NotTo(HaveOccurred())

				var conditions int
				err = sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM rule_conditions WHERE rule_id = ?`, ruleID).Scan(&conditions)
				Expect(err).NotTo(HaveOccurred())
				Expect(conditions).To(BeZero())

				var rules int
				err = sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM rules WHERE id = ?`, ruleID).Scan(&rules)
				Expect(err).NotTo(HaveOccurred())
				Expect(rules).To(BeZero())
			})

			// Given a stored rule with a disk geometry condition and another rule next to it
			// When the first rule is deleted
			// Then its payload row is gone and the other rule keeps its own
			It("should remove the disk geometry payload rows of the rule", func() {
				// Arrange
				_, _, err := srv.Update(ctx, models.Rule{
					ID:         ruleID,
					Name:       "stored-rule",
					Vendor:     "VMware",
					Category:   "Backup",
					OsvVerdict: models.OsvSupported,
					Confidence: models.ConfidenceHighLevel,
					MinMatches: 1,
					Conditions: []any{
						models.ConditionInput{
							Name: "disk_geometry",
							Function: &models.FunctionInput{
								Name:         "disk_geometry",
								DiskGeometry: &models.DiskGeometry{Disks: []int64{2048}, Mode: models.DiskGeometrySuperset},
							},
						},
					},
				})
				Expect(err).NotTo(HaveOccurred())

				var payloads int
				Expect(sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM disk_geometry_condition`).Scan(&payloads)).To(Succeed())
				Expect(payloads).To(Equal(1))

				// Act
				Expect(srv.Delete(ctx, ruleID)).To(Succeed())

				// Assert
				Expect(sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM disk_geometry_condition`).Scan(&payloads)).To(Succeed())
				Expect(payloads).To(BeZero())

				var conditions int
				Expect(sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM rule_conditions`).Scan(&conditions)).To(Succeed())
				Expect(conditions).To(BeZero())

				var rules int
				Expect(sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM rules`).Scan(&rules)).To(Succeed())
				Expect(rules).To(BeZero())
			})
		})

		When("a missing rule is deleted", func() {
			// Given no rule with the requested id
			// When that rule is deleted
			// Then the call succeeds, because deleting a missing rule is not an error
			It("should not return an error", func() {
				// Act
				err := srv.Delete(ctx, 999)

				// Assert
				Expect(err).NotTo(HaveOccurred())
			})
		})
	})
})
