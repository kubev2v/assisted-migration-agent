package store_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

var _ = Describe("ApplianceRuleStore", Ordered, func() {
	var (
		ctx    context.Context
		s      *store.Store
		pool   *store.Pool
		tmpDir string

		// The store only wires a registered function into FuncCondition.Func, so the
		// body is a stub: what a predicate matches is covered by the service tests.
		// It answers with a sentinel error, so a spec can prove the wiring happened.
		errPredicateCalled = errors.New("predicate called")

		diskGeometryFunc = func(models.DiskGeometry, models.ApplianceVirtualMachine) (bool, error) {
			return false, errPredicateCalled
		}

		conditionFuncs = models.ConditionFuncs{
			"disk_geometry_exact":    diskGeometryFunc,
			"disk_geometry_superset": diskGeometryFunc,
		}
	)

	BeforeAll(func() {
		ctx = context.Background()

		var err error
		tmpDir, err = os.MkdirTemp("", "appliance-rule-store-test-*")
		Expect(err).NotTo(HaveOccurred())

		pool = store.NewPool(5 * time.Minute)
		mainDB, dbErr := pool.NewDatabase(store.MainDatabaseID, filepath.Join(tmpDir, "agent.duckdb"), time.Now(), store.EagerConnectionInitilization, store.ReadWriteDatabase)
		Expect(dbErr).NotTo(HaveOccurred())

		Expect(mainDB.Migrate(ctx, migrations.RunMain)).To(Succeed())
		pool.Add(mainDB)

		s, err = mainDB.Store()
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

	clearRules := func() {
		// Payload rows reference the condition rows, so they are removed first.
		_, err := s.Querier().ExecContext(ctx, `DELETE FROM disk_geometry_condition`)
		Expect(err).NotTo(HaveOccurred())

		_, err = s.Querier().ExecContext(ctx, `DELETE FROM rule_conditions`)
		Expect(err).NotTo(HaveOccurred())

		_, err = s.Querier().ExecContext(ctx, `DELETE FROM rules`)
		Expect(err).NotTo(HaveOccurred())
	}

	// newRule builds the minimal rule body the store accepts, so tests only spell
	// out the conditions they care about.
	newRule := func(name string, conditions []any) models.Rule {
		return models.Rule{
			Name:       name,
			Vendor:     "Test",
			Category:   "Test",
			Confidence: models.ConfidenceHighLevel,
			MinMatches: 1,
			Conditions: conditions,
		}
	}

	newDiskGeometryCondition := func(funcName string, mode models.DiskGeometryMode, disks []int64, tolerance int) models.FuncCondition[models.DiskGeometry] {
		return models.FuncCondition[models.DiskGeometry]{
			Data: models.DiskGeometry{
				Disks:     disks,
				Tolerance: tolerance,
				Mode:      mode,
			},
			Name:     "disk_geometry",
			Tier:     5,
			FuncName: funcName,
			Func:     diskGeometryFunc,
		}
	}

	insertRule := func(name string) int64 {
		var id int64
		err := s.Querier().QueryRowContext(ctx, `
			INSERT INTO rules (name, vendor, category, confidence, min_matched, osv_verdict)
			VALUES (?, 'Test', 'Test', ?, 1, ?)
			RETURNING id
		`, name, int(models.ConfidenceHighLevel), int(models.OsvUnknown)).Scan(&id)
		Expect(err).NotTo(HaveOccurred())

		return id
	}

	insertExpressionCondition := func(ruleID int64, name string, tier int, expression string) {
		_, err := s.Querier().ExecContext(ctx, `
			INSERT INTO rule_conditions (rule_id, name, tier, expression)
			VALUES (?, ?, ?, ?)
		`, ruleID, name, tier, expression)
		Expect(err).NotTo(HaveOccurred())
	}

	// insertDiskGeometryCondition writes the condition row and the payload row it
	// is joined with, the way the store writes them.
	insertDiskGeometryCondition := func(ruleID int64, name string, tier int, funcName, mode string, disks []int64, tolerance int) {
		var conditionID int64
		err := s.Querier().QueryRowContext(ctx, `
			INSERT INTO rule_conditions (rule_id, name, tier, func_name)
			VALUES (?, ?, ?, ?)
			RETURNING id
		`, ruleID, name, tier, funcName).Scan(&conditionID)
		Expect(err).NotTo(HaveOccurred())

		_, err = s.Querier().ExecContext(ctx, `
			INSERT INTO disk_geometry_condition (disks, tolerance, mode, rule_condition_id)
			VALUES (?, ?, ?, ?)
		`, disks, tolerance, mode, conditionID)
		Expect(err).NotTo(HaveOccurred())
	}

	Context("Given an agent seeded with appliance rules", func() {
		AfterEach(func() {
			clearRules()
		})

		// Given a rule with one expression condition
		// When all rules are listed
		// Then the condition is read back with its name, tier and expression
		It("reads an expression condition", func() {
			// Arrange
			ruleID := insertRule("rule")
			insertExpressionCondition(ruleID, "hostname", 1, `Name ~ /^ORA-/`)

			// Act
			rules, err := s.Appliance().ListRules(ctx, conditionFuncs)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(rules).To(HaveLen(1))
			Expect(rules[0].ID).To(Equal(ruleID))
			Expect(rules[0].Conditions).To(ConsistOf(models.ExpressionCondition{
				Name:       "hostname",
				Tier:       1,
				Expression: `Name ~ /^ORA-/`,
			}))
		})

		// Given a rule with a disk geometry condition and its payload row
		// When the rules are listed
		// Then the condition is read with both disk sizes, its mode and its tier, and the predicate is bound
		It("reads a disk geometry condition with its payload", func() {
			// Arrange
			ruleID := insertRule("rule")
			insertDiskGeometryCondition(ruleID, "disk_geometry", 5, "disk_geometry_exact", "exact", []int64{10240, 20480}, 0)

			// Act
			rules, err := s.Appliance().ListRules(ctx, conditionFuncs)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(rules).To(HaveLen(1))
			Expect(rules[0].Conditions).To(HaveLen(1))

			condition, ok := rules[0].Conditions[0].(models.FuncCondition[models.DiskGeometry])
			Expect(ok).To(BeTrue())
			Expect(condition.Name).To(Equal("disk_geometry"))
			Expect(condition.Tier).To(Equal(5))
			Expect(condition.FuncName).To(Equal("disk_geometry_exact"))
			Expect(condition.Data).To(Equal(models.DiskGeometry{
				Disks:     []int64{10240, 20480},
				Tolerance: 0,
				Mode:      models.DiskGeometryExact,
			}))
			Expect(condition.Func).NotTo(BeNil())
		})

		// Given a disk geometry condition that holds a single disk size
		// When the rules are listed
		// Then the condition holds exactly that one size
		It("reads a disk geometry condition with a single disk", func() {
			// Arrange
			ruleID := insertRule("rule")
			insertDiskGeometryCondition(ruleID, "disk_geometry", 5, "disk_geometry_exact", "exact", []int64{2048}, 0)

			// Act
			rules, err := s.Appliance().ListRules(ctx, conditionFuncs)

			// Assert
			Expect(err).NotTo(HaveOccurred())

			condition, ok := rules[0].Conditions[0].(models.FuncCondition[models.DiskGeometry])
			Expect(ok).To(BeTrue())
			Expect(condition.Data.Disks).To(Equal([]int64{2048}))
		})

		// Given a superset disk geometry condition with a tolerance
		// When the rules are listed
		// Then the mode and the tolerance come from the payload row
		It("reads the disk geometry mode and tolerance", func() {
			// Arrange
			ruleID := insertRule("rule")
			insertDiskGeometryCondition(ruleID, "disk_geometry", 3, "disk_geometry_superset", "superset", []int64{4096}, 512)

			// Act
			rules, err := s.Appliance().ListRules(ctx, conditionFuncs)

			// Assert
			Expect(err).NotTo(HaveOccurred())

			condition, ok := rules[0].Conditions[0].(models.FuncCondition[models.DiskGeometry])
			Expect(ok).To(BeTrue())
			Expect(condition.Data.Mode).To(Equal(models.DiskGeometrySuperset))
			Expect(condition.Data.Tolerance).To(Equal(512))
		})

		// Given a disk geometry condition whose function name is registered
		// When the rules are listed
		// Then the condition carries the registered predicate, not a placeholder
		It("wires the registered function into the condition", func() {
			// Arrange
			ruleID := insertRule("rule")
			insertDiskGeometryCondition(ruleID, "disk_geometry", 5, "disk_geometry_exact", "exact", []int64{2048}, 0)

			// Act
			rules, err := s.Appliance().ListRules(ctx, conditionFuncs)

			// Assert
			Expect(err).NotTo(HaveOccurred())

			condition, ok := rules[0].Conditions[0].(models.FuncCondition[models.DiskGeometry])
			Expect(ok).To(BeTrue())

			_, matchErr := condition.Func(condition.Data, models.ApplianceVirtualMachine{})
			Expect(matchErr).To(MatchError(errPredicateCalled))
		})

		// Given one rule whose three conditions are written out of order
		// When the rules are listed
		// Then the conditions come back sorted by tier and then by name
		It("orders conditions by tier and then by name", func() {
			// Arrange
			ruleID := insertRule("rule")
			insertExpressionCondition(ruleID, "zeta", 2, `Name = "zeta"`)
			insertExpressionCondition(ruleID, "beta", 1, `Name = "beta"`)
			insertExpressionCondition(ruleID, "alpha", 1, `Name = "alpha"`)

			// Act
			rules, err := s.Appliance().ListRules(ctx, conditionFuncs)

			// Assert
			Expect(err).NotTo(HaveOccurred())

			Expect(rules[0].Conditions).To(Equal([]any{
				models.ExpressionCondition{Name: "alpha", Tier: 1, Expression: `Name = "alpha"`},
				models.ExpressionCondition{Name: "beta", Tier: 1, Expression: `Name = "beta"`},
				models.ExpressionCondition{Name: "zeta", Tier: 2, Expression: `Name = "zeta"`},
			}))
		})

		// Given three rules whose names are not alphabetical
		// When the rules are listed
		// Then the rules come back sorted by name
		It("orders rules by name", func() {
			// Arrange
			for _, name := range []string{"bravo", "alpha", "charlie"} {
				ruleID := insertRule(name)
				insertExpressionCondition(ruleID, "hostname", 1, `Name = "alpha"`)
			}

			// Act
			rules, err := s.Appliance().ListRules(ctx, conditionFuncs)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(rules).To(HaveLen(3))
			Expect(rules[0].Name).To(Equal("alpha"))
			Expect(rules[1].Name).To(Equal("bravo"))
			Expect(rules[2].Name).To(Equal("charlie"))
		})

		// Given a rule whose kind column says custom
		// When the rules are listed
		// Then the reported kind is custom
		It("reads the rule kind", func() {
			// Arrange
			ruleID := insertRule("rule")
			_, err := s.Querier().ExecContext(ctx, `UPDATE rules SET kind = ? WHERE id = ?`, int(models.CustomRule), ruleID)
			Expect(err).NotTo(HaveOccurred())
			insertExpressionCondition(ruleID, "hostname", 1, `Name = "alpha"`)

			// Act
			rules, err := s.Appliance().ListRules(ctx, conditionFuncs)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(rules[0].RuleKind).To(Equal(models.CustomRule))
		})

		// Given a rule without any condition
		// When the rules are listed
		// Then the rule is left out, because a rule is read through its conditions
		It("skips a rule that has no conditions", func() {
			// Arrange
			insertRule("no-conditions")

			// Act
			rules, err := s.Appliance().ListRules(ctx, conditionFuncs)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(rules).To(BeEmpty())
		})

		// Given a disk geometry condition without its payload row
		// When the rules are listed
		// Then listing fails, because the payload is part of the condition
		It("fails when the disk geometry payload row is missing", func() {
			// Arrange
			ruleID := insertRule("rule")
			_, err := s.Querier().ExecContext(ctx, `
				INSERT INTO rule_conditions (rule_id, name, tier, func_name)
				VALUES (?, 'disk_geometry', 5, 'disk_geometry_exact')
			`, ruleID)
			Expect(err).NotTo(HaveOccurred())

			// Act
			_, err = s.Appliance().ListRules(ctx, conditionFuncs)

			// Assert
			Expect(err).To(MatchError(ContainSubstring("has no disk geometry row")))
		})

		// Given a condition whose function name says exact while its payload says superset
		// When the rules are listed
		// Then listing fails, because no predicate can honour an inconsistent pair
		It("fails when the function name does not match the payload mode", func() {
			// Arrange
			ruleID := insertRule("rule")
			insertDiskGeometryCondition(ruleID, "disk_geometry", 5, "disk_geometry_exact", "superset", []int64{2048}, 0)

			// Act
			_, err := s.Appliance().ListRules(ctx, conditionFuncs)

			// Assert
			Expect(err).To(MatchError(ContainSubstring("does not match disk geometry mode")))
		})

		// Given a condition whose function is not a disk geometry function
		// When the rules are listed
		// Then listing fails instead of returning a condition nothing can evaluate
		It("fails when the function name is not a disk geometry function", func() {
			// Arrange
			ruleID := insertRule("rule")
			_, err := s.Querier().ExecContext(ctx, `
				INSERT INTO rule_conditions (rule_id, name, tier, func_name)
				VALUES (?, 'cpu', 1, 'cpu-at-least')
			`, ruleID)
			Expect(err).NotTo(HaveOccurred())

			// Act
			_, err = s.Appliance().ListRules(ctx, conditionFuncs)

			// Assert
			Expect(err).To(MatchError(ContainSubstring("is not supported")))
		})

		// Given a disk geometry condition whose function is not in the registry
		// When the rules are listed
		// Then listing fails, because the predicate would be nil
		It("fails when the disk geometry function is not registered", func() {
			// Arrange
			ruleID := insertRule("rule")
			insertDiskGeometryCondition(ruleID, "disk_geometry", 5, "disk_geometry_fuzzy", "fuzzy", []int64{2048}, 0)

			// Act
			_, err := s.Appliance().ListRules(ctx, conditionFuncs)

			// Assert
			Expect(err).To(MatchError(ContainSubstring("is not registered")))
		})
	})

	Context("Given a stored appliance rule", func() {
		AfterEach(func() {
			clearRules()
		})

		// Given two stored rules
		// When one of them is read by id
		// Then only that rule and its condition are returned
		It("returns the requested rule and not the others", func() {
			// Arrange
			wantedID := insertRule("wanted")
			insertExpressionCondition(wantedID, "hostname", 1, `Name = "wanted"`)

			otherID := insertRule("other")
			insertExpressionCondition(otherID, "hostname", 1, `Name = "other"`)

			// Act
			rule, err := s.Appliance().GetRule(ctx, conditionFuncs, wantedID)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(rule.ID).To(Equal(wantedID))
			Expect(rule.Name).To(Equal("wanted"))
			Expect(rule.Conditions).To(ConsistOf(models.ExpressionCondition{
				Name:       "hostname",
				Tier:       1,
				Expression: `Name = "wanted"`,
			}))
		})

		// Given no rule with the requested id
		// When the rule is read by id
		// Then a resource not found error is returned
		It("returns not found for an unknown id", func() {
			// Act
			_, err := s.Appliance().GetRule(ctx, conditionFuncs, 4040)

			// Assert
			Expect(srvErrors.IsResourceNotFoundError(err)).To(BeTrue())
		})
	})

	Context("Given an agent with no appliance rules", func() {
		BeforeEach(func() {
			clearRules()
		})

		// Given a rule whose every field differs from its column default
		// When the rule is created and read back
		// Then an id is assigned and every column reports the value it was given
		It("assigns an id and persists the rule fields", func() {
			// Arrange
			rule := newRule("oracle-oda", nil)
			rule.Vendor = "Oracle"
			rule.Category = "database"
			rule.RuleKind = models.CustomRule
			rule.OsvVerdict = models.OsvUnknown
			rule.Confidence = models.ConfidenceHighLevel
			rule.MinMatches = 2
			rule.Conditions = []any{models.ExpressionCondition{Name: "hostname", Tier: 1, Expression: `Name ~ /^ORA-/`}}

			// Act
			created, err := s.Appliance().CreateRule(ctx, rule)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(created.ID).NotTo(BeZero())

			stored, err := s.Appliance().GetRule(ctx, conditionFuncs, created.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(stored.Name).To(Equal("oracle-oda"))
			Expect(stored.Vendor).To(Equal("Oracle"))
			Expect(stored.Category).To(Equal("database"))
			Expect(stored.RuleKind).To(Equal(models.CustomRule))
			Expect(stored.OsvVerdict).To(Equal(models.OsvUnknown))
			Expect(stored.Confidence).To(Equal(models.ConfidenceHighLevel))
			Expect(stored.MinMatches).To(Equal(2))
		})

		// Given a rule with a disk geometry condition
		// When the rule is created
		// Then one condition row and one payload row are written, and the payload reads back unchanged
		It("writes a payload row for a disk geometry condition", func() {
			// Arrange
			rule := newRule("disk-geometry", []any{newDiskGeometryCondition("disk_geometry_exact", models.DiskGeometryExact, []int64{2048, 4096}, 0)})

			// Act
			created, err := s.Appliance().CreateRule(ctx, rule)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			var conditions int
			Expect(s.Querier().QueryRowContext(ctx, `SELECT COUNT(*) FROM rule_conditions WHERE rule_id = ?`, created.ID).Scan(&conditions)).To(Succeed())
			Expect(conditions).To(Equal(1))

			var payloads int
			Expect(s.Querier().QueryRowContext(ctx, `
				SELECT COUNT(*)
				FROM disk_geometry_condition d
					JOIN rule_conditions c ON c.id = d.rule_condition_id
				WHERE c.rule_id = ?
			`, created.ID).Scan(&payloads)).To(Succeed())
			Expect(payloads).To(Equal(1))

			stored, err := s.Appliance().GetRule(ctx, conditionFuncs, created.ID)
			Expect(err).NotTo(HaveOccurred())

			condition, ok := stored.Conditions[0].(models.FuncCondition[models.DiskGeometry])
			Expect(ok).To(BeTrue())
			Expect(condition.Data).To(Equal(models.DiskGeometry{
				Disks:     []int64{2048, 4096},
				Tolerance: 0,
				Mode:      models.DiskGeometryExact,
			}))
		})

		// Given a rule with an expression condition
		// When the rule is created
		// Then only a condition row is written and no payload row
		It("writes only condition rows for expression conditions", func() {
			// Arrange
			rule := newRule("expression", []any{models.ExpressionCondition{Name: "hostname", Tier: 1, Expression: `Name = "alpha"`}})

			// Act
			created, err := s.Appliance().CreateRule(ctx, rule)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			var conditions int
			Expect(s.Querier().QueryRowContext(ctx, `SELECT COUNT(*) FROM rule_conditions WHERE rule_id = ?`, created.ID).Scan(&conditions)).To(Succeed())
			Expect(conditions).To(Equal(1))

			var payloads int
			Expect(s.Querier().QueryRowContext(ctx, `SELECT COUNT(*) FROM disk_geometry_condition`).Scan(&payloads)).To(Succeed())
			Expect(payloads).To(BeZero())
		})

		// Given a rule name that is already stored
		// When another rule with the same name is created
		// Then a duplicate resource error is returned
		It("rejects a duplicate rule name", func() {
			// Arrange
			_, err := s.Appliance().CreateRule(ctx, newRule("duplicate", nil))
			Expect(err).NotTo(HaveOccurred())

			// Act
			_, err = s.Appliance().CreateRule(ctx, newRule("duplicate", nil))

			// Assert
			Expect(srvErrors.IsDuplicateResourceError(err)).To(BeTrue())
		})

		// Given a condition of a type the store cannot persist
		// When the rule is created
		// Then it is rejected as an unsupported condition type
		It("rejects an unsupported condition type", func() {
			// Arrange
			rule := newRule("unsupported", []any{models.FuncCondition[string]{Name: "cpu", Tier: 1, FuncName: "cpu-at-least"}})

			// Act
			_, err := s.Appliance().CreateRule(ctx, rule)

			// Assert
			Expect(err).To(MatchError(ContainSubstring("unsupported condition type")))
		})

		// Given a condition whose function name does not match its payload mode
		// When the rule is created
		// Then it is rejected before anything is written
		It("rejects a function name that does not match the payload mode", func() {
			// Arrange
			condition := newDiskGeometryCondition("disk_geometry_exact", models.DiskGeometrySuperset, []int64{2048}, 0)
			rule := newRule("mismatch", []any{condition})

			// Act
			_, err := s.Appliance().CreateRule(ctx, rule)

			// Assert
			Expect(err).To(MatchError(ContainSubstring("does not match disk geometry mode")))
		})
	})

	Context("Given a stored appliance rule to update", func() {
		BeforeEach(func() {
			clearRules()
		})

		// Given a stored rule with an expression condition and a disk geometry condition
		// When it is updated with a single expression condition
		// Then the old rows and their payload are replaced
		It("replaces the conditions and their payload rows", func() {
			// Arrange
			rule := newRule("rule", []any{
				models.ExpressionCondition{Name: "hostname", Tier: 1, Expression: `Name = "alpha"`},
				newDiskGeometryCondition("disk_geometry_exact", models.DiskGeometryExact, []int64{2048}, 0),
			})

			created, err := s.Appliance().CreateRule(ctx, rule)
			Expect(err).NotTo(HaveOccurred())

			created.Vendor = "Updated"
			created.Conditions = []any{models.ExpressionCondition{Name: "hostname", Tier: 2, Expression: `Name = "beta"`}}

			// Act
			err = s.Appliance().UpdateRule(ctx, created)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			var conditions int
			Expect(s.Querier().QueryRowContext(ctx, `SELECT COUNT(*) FROM rule_conditions WHERE rule_id = ?`, created.ID).Scan(&conditions)).To(Succeed())
			Expect(conditions).To(Equal(1))

			var payloads int
			Expect(s.Querier().QueryRowContext(ctx, `SELECT COUNT(*) FROM disk_geometry_condition`).Scan(&payloads)).To(Succeed())
			Expect(payloads).To(BeZero())

			stored, err := s.Appliance().GetRule(ctx, conditionFuncs, created.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(stored.Vendor).To(Equal("Updated"))
			Expect(stored.Conditions).To(ConsistOf(models.ExpressionCondition{
				Name:       "hostname",
				Tier:       2,
				Expression: `Name = "beta"`,
			}))
		})

		// Given a stored rule
		// When it is updated with a different kind in the payload
		// Then the id stays the same and the stored kind is not changed
		It("keeps the id and the rule kind", func() {
			// Arrange
			created, err := s.Appliance().CreateRule(ctx, newRule("rule", []any{models.ExpressionCondition{Name: "hostname", Tier: 1, Expression: `Name = "alpha"`}}))
			Expect(err).NotTo(HaveOccurred())

			created.RuleKind = models.CustomRule
			created.Confidence = models.ConfidenceLowerLevel

			// Act
			err = s.Appliance().UpdateRule(ctx, created)

			// Assert
			Expect(err).NotTo(HaveOccurred())

			stored, err := s.Appliance().GetRule(ctx, conditionFuncs, created.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(stored.ID).To(Equal(created.ID))
			Expect(stored.RuleKind).To(Equal(models.DefaultRule))
			Expect(stored.Confidence).To(Equal(models.ConfidenceLowerLevel))
		})

		// Given no rule with the requested id
		// When the rule is updated
		// Then a resource not found error is returned
		It("returns not found for an unknown id", func() {
			// Arrange
			rule := newRule("missing", []any{models.ExpressionCondition{Name: "hostname", Tier: 1, Expression: `Name = "alpha"`}})
			rule.ID = 4040

			// Act
			err := s.Appliance().UpdateRule(ctx, rule)

			// Assert
			Expect(srvErrors.IsResourceNotFoundError(err)).To(BeTrue())
		})

		// Given two stored rules
		// When one of them is updated to the name of the other
		// Then a duplicate resource error is returned
		It("rejects a name used by another rule", func() {
			// Arrange
			_, err := s.Appliance().CreateRule(ctx, newRule("taken", nil))
			Expect(err).NotTo(HaveOccurred())

			other, err := s.Appliance().CreateRule(ctx, newRule("mine", nil))
			Expect(err).NotTo(HaveOccurred())

			other.Name = "taken"

			// Act
			err = s.Appliance().UpdateRule(ctx, other)

			// Assert
			Expect(srvErrors.IsDuplicateResourceError(err)).To(BeTrue())
		})
	})

	Context("Given a stored appliance rule to delete", func() {
		BeforeEach(func() {
			clearRules()
		})

		// Given a stored rule with an expression condition and a disk geometry condition
		// When the rule is deleted
		// Then no rule row, condition row or payload row is left
		It("removes the rule, its conditions and their payload rows", func() {
			// Arrange
			rule := newRule("rule", []any{
				models.ExpressionCondition{Name: "hostname", Tier: 1, Expression: `Name = "alpha"`},
				newDiskGeometryCondition("disk_geometry_exact", models.DiskGeometryExact, []int64{2048}, 0),
			})

			created, err := s.Appliance().CreateRule(ctx, rule)
			Expect(err).NotTo(HaveOccurred())

			// Act
			err = s.Appliance().DeleteRule(ctx, created.ID)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			var rules int
			Expect(s.Querier().QueryRowContext(ctx, `SELECT COUNT(*) FROM rules WHERE id = ?`, created.ID).Scan(&rules)).To(Succeed())
			Expect(rules).To(BeZero())

			var conditions int
			Expect(s.Querier().QueryRowContext(ctx, `SELECT COUNT(*) FROM rule_conditions WHERE rule_id = ?`, created.ID).Scan(&conditions)).To(Succeed())
			Expect(conditions).To(BeZero())

			var payloads int
			Expect(s.Querier().QueryRowContext(ctx, `SELECT COUNT(*) FROM disk_geometry_condition`).Scan(&payloads)).To(Succeed())
			Expect(payloads).To(BeZero())
		})

		// Given two stored rules that both have a disk geometry condition
		// When one of them is deleted
		// Then only the rows of that rule are gone
		It("keeps the conditions of other rules", func() {
			// Arrange
			kept, err := s.Appliance().CreateRule(ctx, newRule("kept", []any{newDiskGeometryCondition("disk_geometry_exact", models.DiskGeometryExact, []int64{1024}, 0)}))
			Expect(err).NotTo(HaveOccurred())

			removed, err := s.Appliance().CreateRule(ctx, newRule("removed", []any{newDiskGeometryCondition("disk_geometry_exact", models.DiskGeometryExact, []int64{2048}, 0)}))
			Expect(err).NotTo(HaveOccurred())

			// Act
			err = s.Appliance().DeleteRule(ctx, removed.ID)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			var conditions int
			Expect(s.Querier().QueryRowContext(ctx, `SELECT COUNT(*) FROM rule_conditions WHERE rule_id = ?`, kept.ID).Scan(&conditions)).To(Succeed())
			Expect(conditions).To(Equal(1))

			var payloads int
			Expect(s.Querier().QueryRowContext(ctx, `
				SELECT COUNT(*)
				FROM disk_geometry_condition d
					JOIN rule_conditions c ON c.id = d.rule_condition_id
				WHERE c.rule_id = ?
			`, kept.ID).Scan(&payloads)).To(Succeed())
			Expect(payloads).To(Equal(1))

			stored, err := s.Appliance().GetRule(ctx, conditionFuncs, kept.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(stored.Conditions).To(HaveLen(1))
		})

		// Given no rule with the requested id
		// When the rule is deleted
		// Then the call succeeds
		It("succeeds when the rule does not exist", func() {
			// Act
			err := s.Appliance().DeleteRule(ctx, 4040)

			// Assert
			Expect(err).NotTo(HaveOccurred())
		})

		// Given a rule that has already been deleted
		// When it is deleted again
		// Then the call succeeds
		It("succeeds when the rule is deleted twice", func() {
			// Arrange
			created, err := s.Appliance().CreateRule(ctx, newRule("rule", nil))
			Expect(err).NotTo(HaveOccurred())

			Expect(s.Appliance().DeleteRule(ctx, created.ID)).To(Succeed())

			// Act
			err = s.Appliance().DeleteRule(ctx, created.ID)

			// Assert
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
