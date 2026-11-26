package main

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	ginkgo "github.com/onsi/ginkgo/v2"
	gm "github.com/onsi/gomega"

	v2 "github.com/kubev2v/assisted-migration-agent/agent/api/v2"

	"github.com/kubev2v/assisted-migration-agent/pkg/e2e/infra"
	"github.com/kubev2v/assisted-migration-agent/test/e2e-v2/service"
)

var _ = ginkgo.Describe("Appliance rules endpoint v2 e2e tests", ginkgo.Ordered, func() {
	var agentSvc *service.AgentSvc

	ginkgo.BeforeAll(func() {
		ginkgo.GinkgoWriter.Println("Starting postgres...")
		err := infraManager.StartPostgres()
		gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start postgres")
		time.Sleep(2 * time.Second)

		agentSvc = service.DefaultAgentSvc(cfg.AgentAPIUrl)

		// Rules live in the agent database, not in a collection, so this suite
		// needs no vCenter and no collector run.
		agentID := uuid.NewString()
		ginkgo.GinkgoWriter.Printf("Starting agent %s in disconnected mode (v2)...\n", agentID)
		_, err = infraManager.StartAgent(infra.AgentConfig{
			AgentID:        agentID,
			SourceID:       uuid.NewString(),
			Mode:           "disconnected",
			ConsoleURL:     cfg.AgentProxyUrl,
			UpdateInterval: "1s",
			APIVersion:     "v2",
		})
		gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to start agent")

		gm.Eventually(func() error {
			_, err := agentSvc.Status()
			return err
		}, 30*time.Second, 1*time.Second).Should(gm.BeNil(), "agent did not become ready")

		ginkgo.GinkgoWriter.Println("Appliance rules endpoint v2 test setup complete")
	})

	ginkgo.AfterAll(func() {
		ginkgo.GinkgoWriter.Println("Cleaning up appliance rules endpoint v2 tests...")
		_ = infraManager.RemoveAgent()
		_ = infraManager.StopPostgres()
	})

	// The agent database survives the whole suite, so every spec uses its own
	// rule name and deletes what it created.
	uniqueRuleName := func(prefix string) string {
		return "e2e-" + prefix + "-" + uuid.NewString()[:8]
	}

	// ruleRequest builds a valid request. A spec changes the field it cares about.
	ruleRequest := func(name string) v2.ApplianceRuleRequest {
		expression := `Name = 'e2e'`

		return v2.ApplianceRuleRequest{
			Name:          name,
			Vendor:        "E2E",
			Category:      "e2e",
			OsvVerdict:    v2.ApplianceRuleRequestOsvVerdictSupported,
			MaxConfidence: v2.ApplianceRuleRequestMaxConfidenceHigh,
			MinMatched:    json.Number("1"),
			Conditions: []v2.ApplianceRuleCondition{{
				Name:       "annotation",
				Expression: &expression,
			}},
		}
	}

	geometryCondition := func(disks []json.Number, mode v2.DiskGeometryConditionMode, tolerance json.Number) v2.ApplianceRuleCondition {
		return v2.ApplianceRuleCondition{
			Name: "disk_geometry",
			Function: &v2.ApplianceRuleFunction{
				Name: "disk_geometry",
				DiskGeometry: &v2.DiskGeometryCondition{
					Disks:        disks,
					Mode:         mode,
					ToleranceMib: tolerance,
				},
			},
		}
	}

	createRule := func(req v2.ApplianceRuleRequest) string {
		created, err := agentSvc.CreateApplianceRule(req)
		gm.Expect(err).ToNot(gm.HaveOccurred(), "failed to create rule")
		gm.Expect(created.Id).ToNot(gm.BeNil(), "created rule has no id")

		id := *created.Id
		ginkgo.DeferCleanup(func() { _, _ = agentSvc.DeleteApplianceRule(id) })

		return id
	}

	ginkgo.Context("Listing", func() {
		// Given an agent with at least one custom rule
		// When the rules are listed
		// Then the created rule is in the list with a kind of custom
		ginkgo.It("should list the created rule", func() {
			// Arrange
			name := uniqueRuleName("list")
			createRule(ruleRequest(name))

			// Act
			result, err := agentSvc.ListApplianceRules()

			// Assert
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(result.Rules).ToNot(gm.BeNil())

			names := make([]string, 0, len(result.Rules))
			for _, rule := range result.Rules {
				gm.Expect(rule.Id).ToNot(gm.BeNil(), "rule %q has no id", rule.Name)
				names = append(names, rule.Name)
			}
			gm.Expect(names).To(gm.ContainElement(name))
		})
	})

	ginkgo.Context("Creating", func() {
		// Given a rule with an expression condition
		// When it is created
		// Then it is stored as a custom rule and returned with a server assigned id
		ginkgo.It("should create a custom rule", func() {
			// Arrange
			req := ruleRequest(uniqueRuleName("create"))

			// Act
			created, err := agentSvc.CreateApplianceRule(req)

			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(created.Id).ToNot(gm.BeNil())
			ginkgo.DeferCleanup(func() { _, _ = agentSvc.DeleteApplianceRule(*created.Id) })

			// Assert
			gm.Expect(created.Kind).To(gm.Equal(v2.Custom))
			gm.Expect(created.Name).To(gm.Equal(req.Name))
			gm.Expect(created.Vendor).To(gm.Equal("E2E"))
			gm.Expect(created.Category).To(gm.Equal("e2e"))
			gm.Expect(created.OsvVerdict).To(gm.Equal(v2.ApplianceRuleOsvVerdictSupported))
			gm.Expect(created.MaxConfidence).To(gm.Equal(v2.ApplianceRuleMaxConfidenceHigh))
			gm.Expect(created.MinMatched).To(gm.Equal(json.Number("1")))
			gm.Expect(created.Conditions).To(gm.Equal(req.Conditions))
		})

		// Given a rule whose condition carries a disk geometry payload
		// When it is created and read back
		// Then the disk sizes, the mode and the tolerance survive the round trip
		ginkgo.It("should keep the disk geometry payload", func() {
			// Arrange
			req := ruleRequest(uniqueRuleName("geometry"))
			req.Conditions = []v2.ApplianceRuleCondition{
				geometryCondition([]json.Number{"20480", "40960"}, v2.Superset, json.Number("512")),
			}

			// Act
			id := createRule(req)

			// Assert
			stored, err := agentSvc.GetApplianceRule(id)
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(stored.Conditions).To(gm.Equal(req.Conditions))
		})

		// Given a request body with one rule broken
		// When each one is posted
		// Then the request is rejected and the response says why
		ginkgo.DescribeTable("rejects an invalid rule",
			func(body, wanted string) {
				// Act
				status, responseBody, err := agentSvc.CreateApplianceRuleRaw([]byte(body))

				// Assert
				gm.Expect(err).ToNot(gm.HaveOccurred())
				gm.Expect(status).To(gm.Equal(400))
				gm.Expect(responseBody).To(gm.ContainSubstring(wanted))
			},
			ginkgo.Entry("a blank name", `{
				"name": "   ",
				"vendor": "E2E",
				"category": "e2e",
				"osv_verdict": "supported",
				"max_confidence": "high",
				"min_matched": 1,
				"conditions": [{"name": "annotation", "expression": "Name = 'e2e'"}]
			}`, "name must not be blank"),
			ginkgo.Entry("a blank vendor", `{
				"name": "e2e-reject",
				"vendor": "   ",
				"category": "e2e",
				"osv_verdict": "supported",
				"max_confidence": "high",
				"min_matched": 1,
				"conditions": [{"name": "annotation", "expression": "Name = 'e2e'"}]
			}`, "vendor must not be blank"),
			ginkgo.Entry("a blank category", `{
				"name": "e2e-reject",
				"vendor": "E2E",
				"category": "   ",
				"osv_verdict": "supported",
				"max_confidence": "high",
				"min_matched": 1,
				"conditions": [{"name": "annotation", "expression": "Name = 'e2e'"}]
			}`, "category must not be blank"),
			ginkgo.Entry("an unknown condition name", `{
				"name": "e2e-reject",
				"vendor": "E2E",
				"category": "e2e",
				"osv_verdict": "supported",
				"max_confidence": "high",
				"min_matched": 1,
				"conditions": [{"name": "cpu", "expression": "Name = 'e2e'"}]
			}`, "unknown condition"),
			ginkgo.Entry("an expression that does not parse", `{
				"name": "e2e-reject",
				"vendor": "E2E",
				"category": "e2e",
				"osv_verdict": "supported",
				"max_confidence": "high",
				"min_matched": 1,
				"conditions": [{"name": "annotation", "expression": "Name ="}]
			}`, "invalid expression"),
			ginkgo.Entry("a blank condition name", `{
				"name": "e2e-reject",
				"vendor": "E2E",
				"category": "e2e",
				"osv_verdict": "supported",
				"max_confidence": "high",
				"min_matched": 1,
				"conditions": [{"name": "   ", "expression": "Name = 'e2e'"}]
			}`, "condition[0]: name must not be blank"),
			ginkgo.Entry("an empty expression", `{
				"name": "e2e-reject",
				"vendor": "E2E",
				"category": "e2e",
				"osv_verdict": "supported",
				"max_confidence": "high",
				"min_matched": 1,
				"conditions": [{"name": "annotation", "expression": "   "}]
			}`, "expression is empty"),
			ginkgo.Entry("a condition with both an expression and a function", `{
				"name": "e2e-reject",
				"vendor": "E2E",
				"category": "e2e",
				"osv_verdict": "supported",
				"max_confidence": "high",
				"min_matched": 1,
				"conditions": [{
					"name": "disk_geometry",
					"expression": "Name = 'e2e'",
					"function": {
						"name": "disk_geometry",
						"disk_geometry": {"disks": [20480], "mode": "exact", "tolerance_mib": 0}
					}
				}]
			}`, "exactly one of expression or function must be set"),
			ginkgo.Entry("a condition with neither an expression nor a function", `{
				"name": "e2e-reject",
				"vendor": "E2E",
				"category": "e2e",
				"osv_verdict": "supported",
				"max_confidence": "high",
				"min_matched": 1,
				"conditions": [{"name": "annotation"}]
			}`, "exactly one of expression or function must be set"),
			ginkgo.Entry("two conditions with the same name", `{
				"name": "e2e-reject",
				"vendor": "E2E",
				"category": "e2e",
				"osv_verdict": "supported",
				"max_confidence": "high",
				"min_matched": 1,
				"conditions": [
					{"name": "annotation", "expression": "Name = 'e2e'"},
					{"name": "annotation", "expression": "Name = 'other'"}
				]
			}`, "duplicate condition name"),
			ginkgo.Entry("min_matched above the condition count", `{
				"name": "e2e-reject",
				"vendor": "E2E",
				"category": "e2e",
				"osv_verdict": "supported",
				"max_confidence": "high",
				"min_matched": 2,
				"conditions": [{"name": "annotation", "expression": "Name = 'e2e'"}]
			}`, "min_matched is 2 but the rule has 1 conditions"),
			ginkgo.Entry("min_matched below one", `{
				"name": "e2e-reject",
				"vendor": "E2E",
				"category": "e2e",
				"osv_verdict": "supported",
				"max_confidence": "high",
				"min_matched": 0,
				"conditions": [{"name": "annotation", "expression": "Name = 'e2e'"}]
			}`, "min_matched must be at least 1"),
			ginkgo.Entry("a negative tolerance", `{
				"name": "e2e-reject",
				"vendor": "E2E",
				"category": "e2e",
				"osv_verdict": "supported",
				"max_confidence": "high",
				"min_matched": 1,
				"conditions": [{
					"name": "disk_geometry",
					"function": {
						"name": "disk_geometry",
						"disk_geometry": {"disks": [20480], "mode": "exact", "tolerance_mib": -1}
					}
				}]
			}`, "tolerance must be zero or greater"),
			ginkgo.Entry("a tolerance above the int32 range", `{
				"name": "e2e-reject",
				"vendor": "E2E",
				"category": "e2e",
				"osv_verdict": "supported",
				"max_confidence": "high",
				"min_matched": 1,
				"conditions": [{
					"name": "disk_geometry",
					"function": {
						"name": "disk_geometry",
						"disk_geometry": {"disks": [20480], "mode": "exact", "tolerance_mib": 4294967296}
					}
				}]
			}`, "tolerance should be an int32"),
			ginkgo.Entry("a zero disk size", `{
				"name": "e2e-reject",
				"vendor": "E2E",
				"category": "e2e",
				"osv_verdict": "supported",
				"max_confidence": "high",
				"min_matched": 1,
				"conditions": [{
					"name": "disk_geometry",
					"function": {
						"name": "disk_geometry",
						"disk_geometry": {"disks": [0], "mode": "exact", "tolerance_mib": 0}
					}
				}]
			}`, "must be positive"),
			ginkgo.Entry("a negative disk size", `{
				"name": "e2e-reject",
				"vendor": "E2E",
				"category": "e2e",
				"osv_verdict": "supported",
				"max_confidence": "high",
				"min_matched": 1,
				"conditions": [{
					"name": "disk_geometry",
					"function": {
						"name": "disk_geometry",
						"disk_geometry": {"disks": [-20480], "mode": "exact", "tolerance_mib": 0}
					}
				}]
			}`, "must be positive"),
			ginkgo.Entry("a disk size smaller than the tolerance", `{
				"name": "e2e-reject",
				"vendor": "E2E",
				"category": "e2e",
				"osv_verdict": "supported",
				"max_confidence": "high",
				"min_matched": 1,
				"conditions": [{
					"name": "disk_geometry",
					"function": {
						"name": "disk_geometry",
						"disk_geometry": {"disks": [1024], "mode": "exact", "tolerance_mib": 4096}
					}
				}]
			}`, "must be greater than the tolerance"),
			ginkgo.Entry("no disk sizes", `{
				"name": "e2e-reject",
				"vendor": "E2E",
				"category": "e2e",
				"osv_verdict": "supported",
				"max_confidence": "high",
				"min_matched": 1,
				"conditions": [{
					"name": "disk_geometry",
					"function": {
						"name": "disk_geometry",
						"disk_geometry": {"disks": [], "mode": "exact", "tolerance_mib": 0}
					}
				}]
			}`, "disks"),
			ginkgo.Entry("an unknown disk geometry mode", `{
				"name": "e2e-reject",
				"vendor": "E2E",
				"category": "e2e",
				"osv_verdict": "supported",
				"max_confidence": "high",
				"min_matched": 1,
				"conditions": [{
					"name": "disk_geometry",
					"function": {
						"name": "disk_geometry",
						"disk_geometry": {"disks": [20480], "mode": "loose", "tolerance_mib": 0}
					}
				}]
			}`, "value is not one of the allowed values"),
			ginkgo.Entry("a fractional condition count", `{
				"name": "e2e-reject",
				"vendor": "E2E",
				"category": "e2e",
				"osv_verdict": "supported",
				"max_confidence": "high",
				"min_matched": 1.5,
				"conditions": [{"name": "annotation", "expression": "Name = 'e2e'"}]
			}`, "value must be an integer"),
			ginkgo.Entry("no conditions", `{
				"name": "e2e-reject",
				"vendor": "E2E",
				"category": "e2e",
				"osv_verdict": "supported",
				"max_confidence": "high",
				"min_matched": 1,
				"conditions": []
			}`, "conditions"),
		)

		// Given a name that is already stored
		// When another rule with the same name is created
		// Then the duplicate is rejected
		ginkgo.It("should reject a duplicate rule name", func() {
			// Arrange
			name := uniqueRuleName("duplicate")
			createRule(ruleRequest(name))

			// Act
			_, err := agentSvc.CreateApplianceRule(ruleRequest(name))

			// Assert
			gm.Expect(err).To(gm.HaveOccurred())
			gm.Expect(err.Error()).To(gm.ContainSubstring("already exists"))
		})
	})

	ginkgo.Context("Reading", func() {
		// Given a stored rule
		// When it is read by id
		// Then every field the client sent is returned
		ginkgo.It("should return the stored rule", func() {
			// Arrange
			req := ruleRequest(uniqueRuleName("read"))
			id := createRule(req)

			// Act
			stored, err := agentSvc.GetApplianceRule(id)

			// Assert
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(stored.Id).To(gm.HaveValue(gm.Equal(id)))
			gm.Expect(stored.Name).To(gm.Equal(req.Name))
			gm.Expect(stored.Vendor).To(gm.Equal(req.Vendor))
			gm.Expect(stored.Category).To(gm.Equal(req.Category))
			gm.Expect(stored.Kind).To(gm.Equal(v2.Custom))
			gm.Expect(stored.Conditions).To(gm.Equal(req.Conditions))
		})

		// Given no rule with the requested id
		// When it is read by id
		// Then the response is a not found
		ginkgo.It("should return 404 for an unknown id", func() {
			// Act
			status, err := agentSvc.GetApplianceRuleRaw("4040")

			// Assert
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(status).To(gm.Equal(404))
		})

		// Given an id that is not a number
		// When it is read by id
		// Then the response is a bad request
		ginkgo.It("should return 400 for an id that is not a number", func() {
			// Act
			status, err := agentSvc.GetApplianceRuleRaw("not-a-number")

			// Assert
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(status).To(gm.Equal(400))
		})
	})

	ginkgo.Context("Replacing", func() {
		// Given a stored rule
		// When it is replaced with a different condition set
		// Then the rule is updated and the old condition is gone
		ginkgo.It("should replace the whole condition set", func() {
			// Arrange
			req := ruleRequest(uniqueRuleName("replace"))
			id := createRule(req)

			replacement := req
			replacement.Conditions = []v2.ApplianceRuleCondition{
				geometryCondition([]json.Number{"20480"}, v2.Exact, json.Number("0")),
			}

			// Act
			updated, status, err := agentSvc.UpdateApplianceRule(id, replacement)

			// Assert
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(status).To(gm.Equal(200))
			gm.Expect(updated.Id).To(gm.HaveValue(gm.Equal(id)))
			gm.Expect(updated.Conditions).To(gm.Equal(replacement.Conditions))
		})

		// Given an id no rule has
		// When it is replaced
		// Then a rule is created and the response carries a server assigned id
		ginkgo.It("should create the rule when the id is unknown", func() {
			// Arrange
			req := ruleRequest(uniqueRuleName("upsert"))

			// Act
			created, status, err := agentSvc.UpdateApplianceRule("999999", req)

			// Assert
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(status).To(gm.Equal(201))
			gm.Expect(created.Id).ToNot(gm.BeNil())
			ginkgo.DeferCleanup(func() { _, _ = agentSvc.DeleteApplianceRule(*created.Id) })

			gm.Expect(*created.Id).ToNot(gm.Equal("999999"))
			gm.Expect(created.Name).To(gm.Equal(req.Name))
		})

		// Given an invalid body
		// When a stored rule is replaced
		// Then the request is rejected and the stored rule is left alone
		ginkgo.It("should keep the stored rule when the body is invalid", func() {
			// Arrange
			req := ruleRequest(uniqueRuleName("replace-invalid"))
			id := createRule(req)

			invalid := req
			invalid.MinMatched = json.Number("0")
			raw, err := json.Marshal(invalid)
			gm.Expect(err).ToNot(gm.HaveOccurred())

			// Act
			status, body, err := agentSvc.UpdateApplianceRuleRaw(id, raw)

			// Assert
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(status).To(gm.Equal(400))
			gm.Expect(body).To(gm.ContainSubstring("min_matched must be at least 1"))

			unchanged, err := agentSvc.GetApplianceRule(id)
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(unchanged.MinMatched).To(gm.Equal(json.Number("1")))
			gm.Expect(unchanged.Conditions).To(gm.Equal(req.Conditions))
		})

		// Given a name that another rule already uses
		// When a rule is renamed to it
		// Then the rename is rejected
		ginkgo.It("should reject renaming to a name that is taken", func() {
			// Arrange
			taken := uniqueRuleName("taken")
			createRule(ruleRequest(taken))

			req := ruleRequest(uniqueRuleName("rename"))
			id := createRule(req)

			renamed := req
			renamed.Name = taken

			// Act
			_, status, err := agentSvc.UpdateApplianceRule(id, renamed)

			// Assert
			gm.Expect(err).To(gm.HaveOccurred())
			gm.Expect(status).To(gm.Equal(400))
			gm.Expect(err.Error()).To(gm.ContainSubstring("already exists"))
		})

		// Given an id that is not a positive number
		// When it is replaced
		// Then the request is rejected instead of creating a rule
		ginkgo.DescribeTable("rejects an id that is not a positive number",
			func(ruleID string) {
				// Arrange
				raw, err := json.Marshal(ruleRequest(uniqueRuleName("bad-id")))
				gm.Expect(err).ToNot(gm.HaveOccurred())

				// Act
				status, body, err := agentSvc.UpdateApplianceRuleRaw(ruleID, raw)

				// Assert
				gm.Expect(err).ToNot(gm.HaveOccurred())
				gm.Expect(status).To(gm.Equal(400))
				gm.Expect(body).To(gm.ContainSubstring("invalid rule ID"))
			},
			ginkgo.Entry("not a number", "not-a-number"),
			ginkgo.Entry("zero", "0"),
			ginkgo.Entry("negative", "-3"),
		)
	})

	ginkgo.Context("Deleting", func() {
		// Given a stored rule
		// When it is deleted
		// Then the id answers 404 and deleting it again is not an error
		ginkgo.It("should delete the rule and be idempotent", func() {
			// Arrange
			id := createRule(ruleRequest(uniqueRuleName("delete")))

			// Act
			status, err := agentSvc.DeleteApplianceRule(id)

			// Assert
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(status).To(gm.Equal(204))

			goneStatus, err := agentSvc.GetApplianceRuleRaw(id)
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(goneStatus).To(gm.Equal(404))

			againStatus, err := agentSvc.DeleteApplianceRule(id)
			gm.Expect(err).ToNot(gm.HaveOccurred())
			gm.Expect(againStatus).To(gm.Equal(204))
		})
	})
})
