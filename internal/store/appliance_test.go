package store_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	sq "github.com/Masterminds/squirrel"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"

	vmfilter "github.com/kubev2v/assisted-migration-agent/internal/filter"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	"github.com/kubev2v/assisted-migration-agent/test"
)

var _ = Describe("ApplianceStore", Ordered, func() {
	var (
		ctx    context.Context
		s      *store.Store
		pool   *store.Pool
		tmpDir string

		conditionFuncs = models.ConditionFuncs{
			"device-is-template": func(value bool, vm models.ApplianceVirtualMachine) (bool, error) {
				return vm.IsTemplate == value, nil
			},
			"cpu-at-least": func(value int, vm models.ApplianceVirtualMachine) (bool, error) {
				return int(vm.CpuCount) >= value, nil
			},
			"disk-sizes-exact": func(values []int, vm models.ApplianceVirtualMachine) (bool, error) {
				sizes := make([]int, 0, len(vm.Disks))
				for _, disk := range vm.Disks {
					sizes = append(sizes, int(disk.Size))
				}

				return slices.Equal(sizes, values), nil
			},
		}
	)

	BeforeAll(func() {
		ctx = context.Background()
		var err error
		tmpDir, err = os.MkdirTemp("", "appliance-store-test-*")
		Expect(err).NotTo(HaveOccurred())

		pool = store.NewPool(5 * time.Minute)
		collDB, dbErr := pool.NewDatabase("coll", filepath.Join(tmpDir, "collection.duckdb"), time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
		Expect(dbErr).NotTo(HaveOccurred())

		Expect(collDB.Migrate(ctx, func(mCtx context.Context, sqlDB *sql.DB) error {
			st, stErr := collDB.Store()
			if stErr != nil {
				return stErr
			}
			if pErr := duckdb_parser.New(st.Querier(), test.NewMockValidator()).Init(); pErr != nil {
				return pErr
			}
			return migrations.RunCollection(mCtx, sqlDB, "collection")
		})).To(Succeed())

		pool.Add(collDB)

		s, err = collDB.Store()
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

	insertVM := func(id, name string) {
		_, err := s.Querier().ExecContext(ctx, `
			INSERT INTO vinfo ("VM ID", "VM", "Powerstate", "Cluster", "Memory", "Template", "guest_apps")
			VALUES (?, ?, 'poweredOn', 'cluster-a', 4096, false, '[]')
		`, id, name)
		Expect(err).NotTo(HaveOccurred())
	}

	insertProduct := func(vmID string, key int, name, vendor, version string) {
		_, err := s.Querier().ExecContext(ctx, `
			INSERT INTO vm_vapp_products (vm_id, key, name, vendor, version)
			VALUES (?, ?, ?, ?, ?)
		`, vmID, key, name, vendor, version)
		Expect(err).NotTo(HaveOccurred())
	}

	findAppliance := func(appliances []models.Appliance, name string) models.Appliance {
		for _, a := range appliances {
			if a.Name == name {
				return a
			}
		}
		Fail("appliance not found: " + name)
		return models.Appliance{}
	}

	Context("Given an empty collection database", func() {
		When("appliances are listed", func() {
			It("should return no appliances", func() {
				// Act
				appliances, err := s.Appliance().List(ctx, nil)

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(appliances).To(BeEmpty())
			})
		})
	})

	Context("Given a collection seeded with vApp products", func() {
		BeforeEach(func() {
			insertVM("vm-1", "edge-01")
			insertVM("vm-2", "edge-02")

			insertProduct("vm-1", 1, "DataSync", "AWS", "1.0")
			insertProduct("vm-1", 2, "DataSync", "AWS", "2.0")
			insertProduct("vm-2", 1, "DataSync", "AWS", "2.0")
			insertProduct("vm-1", 3, "BIG-IP", "F5", "17.1")
			insertProduct("vm-2", 4, "BIG-IP", "F5", "17.1")
			insertProduct("vm-orphan", 1, "Ghost", "AWS", "2.0")

			_, err := s.Querier().ExecContext(ctx, `
				INSERT INTO vm_vapp_products (vm_id, key, name, vendor, version)
				VALUES
					('vm-1', 5, NULL, 'AWS', NULL),
					('vm-1', 6, '', 'AWS', NULL),
					('vm-1', 7, 'Bare', NULL, NULL)
			`)
			Expect(err).NotTo(HaveOccurred())

			_, err = s.Querier().ExecContext(ctx, `
				INSERT INTO vm_vapp_products (vm_id, key, name, vendor, version, full_version, vendor_url, product_url, app_url)
				VALUES ('vm-1', 8, 'Meta', 'AWS', '2.0', '2.0-build-7', 'https://example.test/vendor', 'https://example.test/product', 'https://example.test/app')
			`)
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			_, err := s.Querier().ExecContext(ctx, `DELETE FROM vm_vapp_products`)
			Expect(err).NotTo(HaveOccurred())

			_, err = s.Querier().ExecContext(ctx, `DELETE FROM vinfo`)
			Expect(err).NotTo(HaveOccurred())
		})

		When("appliances are listed", func() {
			It("should return one appliance per product sorted by name", func() {
				// Act
				appliances, err := s.Appliance().List(ctx, nil)
				Expect(err).NotTo(HaveOccurred())

				// Assert
				names := make([]string, 0, len(appliances))
				for _, a := range appliances {
					names = append(names, a.Name)
				}
				Expect(names).To(Equal([]string{"BIG-IP", "Bare", "DataSync", "Ghost", "Meta"}))
			})
		})

		When("the same product runs on multiple VMs", func() {
			It("should group them into a single appliance with all VMs", func() {
				// Act
				appliances, err := s.Appliance().List(ctx, nil)
				Expect(err).NotTo(HaveOccurred())

				// Assert
				dataSync := findAppliance(appliances, "DataSync")
				Expect(dataSync.Vendor).To(Equal("AWS"))
				Expect(dataSync.VirtualMachines).To(HaveLen(2))
				Expect(dataSync.VirtualMachines).To(HaveKeyWithValue("vm-1", "edge-01"))
				Expect(dataSync.VirtualMachines).To(HaveKeyWithValue("vm-2", "edge-02"))
			})
		})

		When("a VM reports the same product under different keys", func() {
			It("should list the VM once and use the lowest key row metadata", func() {
				// Act
				appliances, err := s.Appliance().List(ctx, nil)
				Expect(err).NotTo(HaveOccurred())

				// Assert
				dataSync := findAppliance(appliances, "DataSync")
				Expect(dataSync.Version).To(Equal("1.0"))
				Expect(dataSync.VirtualMachines).To(HaveLen(2))
			})
		})

		When("product rows lack a name", func() {
			It("should skip NULL and empty names", func() {
				// Act
				appliances, err := s.Appliance().List(ctx, nil)
				Expect(err).NotTo(HaveOccurred())

				// Assert
				Expect(appliances).To(HaveLen(5))
			})
		})

		When("vendor and version are missing", func() {
			It("should default vendor to Unknown and version to empty", func() {
				// Act
				appliances, err := s.Appliance().List(ctx, nil)
				Expect(err).NotTo(HaveOccurred())

				// Assert
				bare := findAppliance(appliances, "Bare")
				Expect(bare.Vendor).To(Equal("Unknown"))
				Expect(bare.Version).To(BeEmpty())
			})
		})

		When("full product metadata is reported", func() {
			It("should carry the metadata and default confidence to 100", func() {
				// Act
				appliances, err := s.Appliance().List(ctx, nil)
				Expect(err).NotTo(HaveOccurred())

				// Assert
				meta := findAppliance(appliances, "Meta")
				Expect(meta.FullVersion).To(Equal("2.0-build-7"))
				Expect(meta.VendorUrl).To(Equal("https://example.test/vendor"))
				Expect(meta.ProductUrl).To(Equal("https://example.test/product"))
				Expect(meta.ApplicationUrl).To(Equal("https://example.test/app"))
				Expect(meta.Confidence).To(Equal(100))
			})
		})

		When("a product row has no matching inventory VM", func() {
			It("should keep the VM with an empty name", func() {
				// Act
				appliances, err := s.Appliance().List(ctx, nil)
				Expect(err).NotTo(HaveOccurred())

				// Assert
				ghost := findAppliance(appliances, "Ghost")
				Expect(ghost.VirtualMachines).To(HaveLen(1))
				Expect(ghost.VirtualMachines).To(HaveKeyWithValue("vm-orphan", ""))
			})
		})

		When("filtered by vm_id", func() {
			It("should return only appliances running on that VM", func() {
				// Act
				appliances, err := s.Appliance().List(ctx, sq.Eq{"vm_id": "vm-1"})
				Expect(err).NotTo(HaveOccurred())

				// Assert
				Expect(appliances).To(HaveLen(4))
				dataSync := findAppliance(appliances, "DataSync")
				Expect(dataSync.VirtualMachines).To(HaveLen(1))
				Expect(dataSync.VirtualMachines).To(HaveKeyWithValue("vm-1", "edge-01"))
				bigIP := findAppliance(appliances, "BIG-IP")
				Expect(bigIP.VirtualMachines).To(HaveLen(1))
			})
		})

		When("filtered by a vm_id list expression", func() {
			It("should return appliances running on any of the given VMs", func() {
				// Arrange
				applianceFilter, err := vmfilter.ParseWithApplianceMap([]byte("vm_id in ['vm-1', 'vm-2']"))
				Expect(err).NotTo(HaveOccurred())

				// Act
				appliances, err := s.Appliance().List(ctx, applianceFilter)
				Expect(err).NotTo(HaveOccurred())

				// Assert
				Expect(appliances).To(HaveLen(4))
				dataSync := findAppliance(appliances, "DataSync")
				Expect(dataSync.VirtualMachines).To(HaveLen(2))
				Expect(dataSync.VirtualMachines).To(HaveKeyWithValue("vm-1", "edge-01"))
				Expect(dataSync.VirtualMachines).To(HaveKeyWithValue("vm-2", "edge-02"))
			})
		})

		When("filtered by a name expression", func() {
			It("should return only the matching appliance", func() {
				// Arrange
				applianceFilter, err := vmfilter.ParseWithApplianceMap([]byte("name = 'DataSync'"))
				Expect(err).NotTo(HaveOccurred())

				// Act
				appliances, err := s.Appliance().List(ctx, applianceFilter)
				Expect(err).NotTo(HaveOccurred())

				// Assert
				Expect(appliances).To(HaveLen(1))
				Expect(appliances[0].Name).To(Equal("DataSync"))
			})
		})

		When("filtered by a vendor expression", func() {
			It("should return only appliances from that vendor", func() {
				// Arrange
				applianceFilter, err := vmfilter.ParseWithApplianceMap([]byte("vendor = 'F5'"))
				Expect(err).NotTo(HaveOccurred())

				// Act
				appliances, err := s.Appliance().List(ctx, applianceFilter)
				Expect(err).NotTo(HaveOccurred())

				// Assert
				Expect(appliances).To(HaveLen(1))
				Expect(appliances[0].Name).To(Equal("BIG-IP"))
			})
		})

		When("filtered by a version expression", func() {
			It("should return only appliances with that version", func() {
				// Arrange
				applianceFilter, err := vmfilter.ParseWithApplianceMap([]byte("version = '17.1'"))
				Expect(err).NotTo(HaveOccurred())

				// Act
				appliances, err := s.Appliance().List(ctx, applianceFilter)
				Expect(err).NotTo(HaveOccurred())

				// Assert
				Expect(appliances).To(HaveLen(1))
				Expect(appliances[0].Name).To(Equal("BIG-IP"))
			})
		})
	})

	Context("Given a collection seeded with appliance rules", func() {
		insertRule := func(name, vendor, category string, confidence, minMatched, osvVerdict int) int {
			var id int
			err := s.Querier().QueryRowContext(ctx, `
				INSERT INTO rules (name, vendor, category, confidence, min_matched, osv_verdict)
				VALUES (?, ?, ?, ?, ?, ?)
				RETURNING id
			`, name, vendor, category, confidence, minMatched, osvVerdict).Scan(&id)
			Expect(err).NotTo(HaveOccurred())

			return id
		}

		// insertTestRule inserts a rule with the metadata most tests do not care
		// about, so the Arrange step stays focused on the condition under test.
		insertTestRule := func(name string) int {
			return insertRule(name, "Test", "Test", int(models.ConfidenceHighLevel), 1, int(models.OsvUnknown))
		}

		// insertExpressionCondition writes an expression condition. The schema
		// allows either an expression or a func_name, and expression conditions
		// carry no values.
		insertExpressionCondition := func(ruleID int, name string, tier int, expression string) {
			_, err := s.Querier().ExecContext(ctx, `
				INSERT INTO rule_conditions (rule_id, name, tier, expression)
				VALUES (?, ?, ?, ?)
			`, ruleID, name, tier, expression)
			Expect(err).NotTo(HaveOccurred())
		}

		// insertFuncCondition writes a function condition the way the rule store
		// expects it: the function name in func_name and its arguments in a single
		// value column, one row per argument.
		insertFuncCondition := func(ruleID int, name string, tier int, funcName, valueColumn string, values ...any) {
			for _, value := range values {
				_, err := s.Querier().ExecContext(ctx, fmt.Sprintf(`
					INSERT INTO rule_conditions (rule_id, name, tier, func_name, %s)
					VALUES (?, ?, ?, ?, ?)
				`, valueColumn), ruleID, name, tier, funcName, value)
				Expect(err).NotTo(HaveOccurred())
			}
		}

		AfterEach(func() {
			_, err := s.Querier().ExecContext(ctx, `DELETE FROM rule_conditions`)
			Expect(err).NotTo(HaveOccurred())

			_, err = s.Querier().ExecContext(ctx, `DELETE FROM rules`)
			Expect(err).NotTo(HaveOccurred())
		})

		When("the rule has an expression condition", func() {
			It("rule with a single expression condition", func() {
				// Arrange
				ruleID := insertTestRule("rule")
				insertExpressionCondition(ruleID, "template", 2, "IsTemplate = false")

				// Act
				rules, err := s.Appliance().ListRules(ctx, conditionFuncs)

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(rules).To(HaveLen(1))
				Expect(rules[0].Conditions).To(ConsistOf(models.ExpressionCondition{
					Name:       "template",
					Tier:       2,
					Expression: "IsTemplate = false",
				}))
			})

			It("rejects a condition that carries both an expression and a function name", func() {
				// Arrange
				ruleID := insertTestRule("rule")

				// Act
				_, err := s.Querier().ExecContext(ctx, `
				INSERT INTO rule_conditions (rule_id, name, tier, expression, func_name)
				VALUES (?, 'template', 1, 'IsTemplate = false', 'device-is-template')
			`, ruleID)

				// Assert
				Expect(err).To(MatchError(ContainSubstring("CHECK constraint")))
			})

			It("rejects a condition that carries neither an expression nor a function name", func() {
				// Arrange
				ruleID := insertTestRule("rule")

				// Act
				_, err := s.Querier().ExecContext(ctx, `
				INSERT INTO rule_conditions (rule_id, name, tier, bool_value)
				VALUES (?, 'template', 1, true)
			`, ruleID)

				// Assert
				Expect(err).To(MatchError(ContainSubstring("CHECK constraint")))
			})

			It("rejects an expression condition that carries a value", func() {
				// Arrange
				ruleID := insertTestRule("rule")

				// Act
				_, err := s.Querier().ExecContext(ctx, `
				INSERT INTO rule_conditions (rule_id, name, tier, expression, bool_value)
				VALUES (?, 'template', 1, 'IsTemplate = false', true)
			`, ruleID)

				// Assert
				Expect(err).To(MatchError(ContainSubstring("CHECK constraint")))
			})
		})

		When("the rule has a function condition", func() {
			It("rule with a single bool function condition", func() {
				// Arrange
				ruleID := insertTestRule("rule")
				insertFuncCondition(ruleID, "template", 1, "device-is-template", "bool_value", true)

				// Act
				rules, err := s.Appliance().ListRules(ctx, conditionFuncs)

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(rules).To(HaveLen(1))
				Expect(rules[0].Conditions).To(HaveLen(1))

				condition, ok := rules[0].Conditions[0].(models.SingleValueFuncCondition[bool])
				Expect(ok).To(BeTrue())
				Expect(condition.Name).To(Equal("template"))
				Expect(condition.Tier).To(Equal(1))
				Expect(condition.Value).To(BeTrue())

				matched, err := condition.Func(condition.Value, models.ApplianceVirtualMachine{IsTemplate: true})
				Expect(err).NotTo(HaveOccurred())
				Expect(matched).To(BeTrue())

				matched, err = condition.Func(condition.Value, models.ApplianceVirtualMachine{IsTemplate: false})
				Expect(err).NotTo(HaveOccurred())
				Expect(matched).To(BeFalse())
			})

			It("rule with a single number function condition", func() {
				// Arrange
				ruleID := insertTestRule("rule")
				insertFuncCondition(ruleID, "cpu", 2, "cpu-at-least", "int_value", 4)

				// Act
				rules, err := s.Appliance().ListRules(ctx, conditionFuncs)

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(rules).To(HaveLen(1))
				Expect(rules[0].Conditions).To(HaveLen(1))

				condition, ok := rules[0].Conditions[0].(models.SingleValueFuncCondition[int])
				Expect(ok).To(BeTrue())
				Expect(condition.Value).To(Equal(4))
				Expect(condition.Tier).To(Equal(2))

				matched, err := condition.Func(condition.Value, models.ApplianceVirtualMachine{CpuCount: 4})
				Expect(err).NotTo(HaveOccurred())
				Expect(matched).To(BeTrue())
			})

			It("rule with a list function condition", func() {
				// Arrange
				ruleID := insertTestRule("rule")
				insertFuncCondition(ruleID, "disks", 3, "disk-sizes-exact", "int_value", 10240, 20480)

				// Act
				rules, err := s.Appliance().ListRules(ctx, conditionFuncs)

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(rules).To(HaveLen(1))
				Expect(rules[0].Conditions).To(HaveLen(1))

				condition, ok := rules[0].Conditions[0].(models.ListValueFuncCondition[[]int, int])
				Expect(ok).To(BeTrue())
				Expect(condition.Values).To(Equal([]int{10240, 20480}))
				Expect(condition.Tier).To(Equal(3))

				matched, err := condition.Func(condition.Values, models.ApplianceVirtualMachine{
					Disks: []models.ApplianceDisk{{Size: 10240}, {Size: 20480}},
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(matched).To(BeTrue())
			})

			It("fails when a function name is not registered", func() {
				// Arrange
				ruleID := insertTestRule("rule")
				insertFuncCondition(ruleID, "missing", 1, "unregistered-func", "bool_value", true)

				// Act
				_, err := s.Appliance().ListRules(ctx, conditionFuncs)

				// Assert
				Expect(err).To(MatchError(ContainSubstring(`condition function "unregistered-func" is not registered`)))
			})

			It("fails when the value count does not match the function arity", func() {
				// Arrange
				ruleID := insertTestRule("rule")
				insertFuncCondition(ruleID, "disks", 1, "disk-sizes-exact", "int_value", 10240)

				// Act
				_, err := s.Appliance().ListRules(ctx, conditionFuncs)

				// Assert
				Expect(err).To(MatchError(ContainSubstring("expected func(int, models.ApplianceVirtualMachine) (bool, error)")))
			})

			It("rejects a condition that carries no value", func() {
				// Arrange
				ruleID := insertTestRule("rule")

				// Act
				_, err := s.Querier().ExecContext(ctx, `
				INSERT INTO rule_conditions (rule_id, name, tier, func_name)
				VALUES (?, 'disks', 1, 'disk-sizes-exact')
			`, ruleID)

				// Assert
				Expect(err).To(MatchError(ContainSubstring("CHECK constraint")))
			})

			It("rejects a condition that carries two values", func() {
				// Arrange
				ruleID := insertTestRule("rule")

				// Act
				_, err := s.Querier().ExecContext(ctx, `
				INSERT INTO rule_conditions (rule_id, name, tier, func_name, bool_value, int_value)
				VALUES (?, 'template', 1, 'device-is-template', true, 4)
			`, ruleID)

				// Assert
				Expect(err).To(MatchError(ContainSubstring("CHECK constraint")))
			})
		})

		When("the rule itself is read", func() {
			It("rule with several conditions ordered by tier", func() {
				// Arrange
				ruleID := insertTestRule("rule")
				insertExpressionCondition(ruleID, "disk", 5, "DiskSize > 51200")
				insertExpressionCondition(ruleID, "template", 1, "IsTemplate = false")

				// Act
				rules, err := s.Appliance().ListRules(ctx, conditionFuncs)

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(rules).To(HaveLen(1))
				Expect(rules[0].Conditions).To(Equal([]any{
					models.ExpressionCondition{Name: "template", Tier: 1, Expression: "IsTemplate = false"},
					models.ExpressionCondition{Name: "disk", Tier: 5, Expression: "DiskSize > 51200"},
				}))
			})

			It("rule with full metadata", func() {
				// Arrange
				ruleID := insertRule("Microsoft SQL Server", "Microsoft", "Database", int(models.ConfidenceHighLevel), 2, int(models.OsvConditional))
				insertExpressionCondition(ruleID, "template", 1, "IsTemplate = false")

				// Act
				rules, err := s.Appliance().ListRules(ctx, conditionFuncs)

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(rules).To(HaveLen(1))
				Expect(rules[0].Name).To(Equal("Microsoft SQL Server"))
				Expect(rules[0].Vendor).To(Equal("Microsoft"))
				Expect(rules[0].Category).To(Equal("Database"))
				Expect(rules[0].Confidence).To(Equal(models.ConfidenceHighLevel))
				Expect(rules[0].MinMatches).To(Equal(2))
				Expect(rules[0].OsvVerdict).To(Equal(models.OsvConditional))
			})

			It("rule without conditions is skipped", func() {
				// Arrange
				insertTestRule("rule")

				// Act
				rules, err := s.Appliance().ListRules(ctx, conditionFuncs)

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(rules).To(BeEmpty())
			})

			It("rules ordered by name", func() {
				// Arrange
				ruleID := insertTestRule("Redis")
				insertExpressionCondition(ruleID, "name", 1, `Name = "redis-01"`)

				ruleID = insertTestRule("Microsoft SQL Server")
				insertExpressionCondition(ruleID, "name", 1, `Name = "web-01"`)

				ruleID = insertTestRule("BIG-IP")
				insertExpressionCondition(ruleID, "template", 1, "IsTemplate = false")

				// Act
				rules, err := s.Appliance().ListRules(ctx, conditionFuncs)

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(rules).To(HaveLen(3))

				names := make([]string, 0, len(rules))
				for _, r := range rules {
					names = append(names, r.Name)
				}
				Expect(names).To(Equal([]string{"BIG-IP", "Microsoft SQL Server", "Redis"}))
			})
		})

		When("a rule row is written", func() {
			It("rejects a rule with fewer than one minimum match", func() {
				// Arrange
				var id int

				// Act
				err := s.Querier().QueryRowContext(ctx, `
				INSERT INTO rules (name, vendor, category, confidence, min_matched, osv_verdict)
				VALUES ('rule', 'Test', 'Test', ?, 0, ?)
				RETURNING id
			`, int(models.ConfidenceHighLevel), int(models.OsvUnknown)).Scan(&id)

				// Assert
				Expect(err).To(MatchError(ContainSubstring("CHECK constraint")))
			})
		})

	})
})
