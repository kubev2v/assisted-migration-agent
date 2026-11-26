package v2_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v2api "github.com/kubev2v/assisted-migration-agent/agent/api/v2"
	handlers "github.com/kubev2v/assisted-migration-agent/agent/handlers/v2"
	"github.com/kubev2v/assisted-migration-agent/internal/config"
	sharedhandlers "github.com/kubev2v/assisted-migration-agent/internal/handlers"
	svc "github.com/kubev2v/assisted-migration-agent/internal/services"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
)

type applianceRulesProvider struct {
	stubServiceProvider
	rulesSvc *svc.ApplianceRulesService
	rulesErr error
}

func (p *applianceRulesProvider) ApplianceRulesService() (*svc.ApplianceRulesService, error) {
	if p.rulesErr != nil {
		return nil, p.rulesErr
	}
	return p.rulesSvc, nil
}

var _ = Describe("Appliance rule handlers", Ordered, func() {
	var (
		ctx     context.Context
		router  *gin.Engine
		tmpDir  string
		pool    *store.Pool
		sqlDB   *sql.DB
		handler *handlers.Handler
	)

	BeforeAll(func() {
		ctx = context.Background()
		gin.SetMode(gin.TestMode)

		var err error
		tmpDir, err = os.MkdirTemp("", "handler-appliance-rules-test-*")
		Expect(err).NotTo(HaveOccurred())

		pool = store.NewPool(5 * time.Minute)

		mainDB, err := pool.NewDatabase(
			store.MainDatabaseID,
			filepath.Join(tmpDir, "agent.duckdb"),
			time.Now(),
			store.EagerConnectionInitilization,
			store.ReadWriteDatabase,
		)
		Expect(err).NotTo(HaveOccurred())

		Expect(mainDB.Migrate(ctx, func(mCtx context.Context, db *sql.DB) error {
			sqlDB = db
			return migrations.RunMain(mCtx, db)
		})).To(Succeed())

		pool.Add(mainDB)

		st, err := mainDB.Store()
		Expect(err).NotTo(HaveOccurred())

		rulesSvc, err := svc.NewApplianceRulesService(st)
		Expect(err).NotTo(HaveOccurred())

		// The production server registers these validators; without them binding
		// tags such as notblank panic and field names stay in Go casing.
		sharedhandlers.RegisterValidators(binding.Validator.Engine().(*validator.Validate))

		handler = handlers.NewHandler(config.Configuration{}, &applianceRulesProvider{rulesSvc: rulesSvc})

		// RegisterHandlers takes the paths and parameter names from the generated
		// spec, so the spec and the handler cannot drift apart unnoticed.
		router = gin.New()
		v2api.RegisterHandlers(router, handler)
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

	// send issues one request and returns the recorder. A string body is sent as
	// it is, which is how a malformed body is produced; anything else is marshalled.
	send := func(method, path string, body any) *httptest.ResponseRecorder {
		var reader io.Reader

		switch value := body.(type) {
		case nil:
		case string:
			reader = strings.NewReader(value)
		default:
			raw, err := json.Marshal(value)
			Expect(err).NotTo(HaveOccurred())
			reader = bytes.NewReader(raw)
		}

		req := httptest.NewRequest(method, path, reader)
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		return w
	}

	// validRequest is the smallest request the handlers accept.
	validRequest := func() v2api.ApplianceRuleRequest {
		return v2api.ApplianceRuleRequest{
			Name:          "oracle-oda",
			Vendor:        "Oracle",
			Category:      "Database",
			OsvVerdict:    v2api.ApplianceRuleRequestOsvVerdictSupported,
			MaxConfidence: v2api.ApplianceRuleRequestMaxConfidenceHigh,
			MinMatched:    json.Number("1"),
			Conditions: []v2api.ApplianceRuleCondition{
				{Name: "annotation", Expression: ptr(`Name ~ /^ORA-/`)},
			},
		}
	}

	// geometryCondition builds a disk geometry condition for the request tables.
	geometryCondition := func(disks []json.Number, mode v2api.DiskGeometryConditionMode, tolerance json.Number) v2api.ApplianceRuleCondition {
		return v2api.ApplianceRuleCondition{
			Name: "disk_geometry",
			Function: &v2api.ApplianceRuleFunction{
				Name: "disk_geometry",
				DiskGeometry: &v2api.DiskGeometryCondition{
					Disks:        disks,
					Mode:         mode,
					ToleranceMib: tolerance,
				},
			},
		}
	}

	// createRule stores one rule through the HTTP surface and returns its id.
	createRule := func(req v2api.ApplianceRuleRequest) string {
		w := send(http.MethodPost, "/appliances/rules", req)
		Expect(w.Code).To(Equal(http.StatusCreated))

		var created v2api.ApplianceRule
		Expect(json.Unmarshal(w.Body.Bytes(), &created)).To(Succeed())
		Expect(created.Id).NotTo(BeNil())

		return *created.Id
	}

	AfterEach(func() {
		clearRules()
	})

	Context("GET /appliances/rules", func() {
		// Given no stored rules
		// When the rule list is requested
		// Then an empty list is returned, not null
		It("should return an empty list", func() {
			// Act
			w := send(http.MethodGet, "/appliances/rules", nil)

			// Assert
			Expect(w.Code).To(Equal(http.StatusOK))
			Expect(w.Body.String()).To(Equal(`{"rules":[]}`))
		})

		// Given one stored rule
		// When the rule list is requested
		// Then the rule is returned with its condition
		It("should return the stored rule", func() {
			// Arrange
			id := createRule(validRequest())

			// Act
			w := send(http.MethodGet, "/appliances/rules", nil)

			// Assert
			Expect(w.Code).To(Equal(http.StatusOK))

			var list v2api.ApplianceRuleListResponse
			Expect(json.Unmarshal(w.Body.Bytes(), &list)).To(Succeed())
			Expect(list.Rules).To(HaveLen(1))
			Expect(list.Rules[0].Id).To(HaveValue(Equal(id)))
			Expect(list.Rules[0].Name).To(Equal("oracle-oda"))
			Expect(list.Rules[0].Conditions).To(Equal([]v2api.ApplianceRuleCondition{
				{Name: "annotation", Expression: ptr(`Name ~ /^ORA-/`)},
			}))
		})

		// Given a rule service that cannot be built
		// When the rule list is requested
		// Then the failure is reported as a server error, not as a missing resource
		It("should return 500 when the service is unavailable", func() {
			// Arrange
			failingRouter := gin.New()
			failingHandler := handlers.NewHandler(config.Configuration{}, &applianceRulesProvider{rulesErr: errors.New("main database is gone")})
			failingRouter.GET("/appliances/rules", failingHandler.ListApplianceRules)

			// Act
			w := httptest.NewRecorder()
			failingRouter.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/appliances/rules", nil))

			// Assert
			Expect(w.Code).To(Equal(http.StatusInternalServerError))
		})
	})

	Context("POST /appliances/rules", func() {
		// Given a valid request
		// When the rule is created
		// Then it is stored as a custom rule and returned with a server assigned id
		It("should return 201 with the created rule", func() {
			// Act
			w := send(http.MethodPost, "/appliances/rules", validRequest())

			// Assert
			Expect(w.Code).To(Equal(http.StatusCreated))

			var created v2api.ApplianceRule
			Expect(json.Unmarshal(w.Body.Bytes(), &created)).To(Succeed())
			Expect(created.Id).NotTo(BeNil())
			Expect(created.Kind).To(Equal(v2api.Custom))
			Expect(created.MinMatched).To(Equal(json.Number("1")))
		})

		// Given a request whose condition carries a disk geometry with a tolerance
		// When the rule is created and read back
		// Then the tolerance and the mode survive the round trip
		It("should keep the disk geometry tolerance", func() {
			// Arrange
			req := validRequest()
			req.Conditions = []v2api.ApplianceRuleCondition{
				geometryCondition([]json.Number{"2048", "4096"}, v2api.Superset, json.Number("512")),
			}

			// Act
			id := createRule(req)
			w := send(http.MethodGet, "/appliances/rules/"+id, nil)

			// Assert
			Expect(w.Code).To(Equal(http.StatusOK))

			var stored v2api.ApplianceRule
			Expect(json.Unmarshal(w.Body.Bytes(), &stored)).To(Succeed())
			Expect(stored.Conditions).To(Equal(req.Conditions))
		})

		// Given bodies that break the shape the API declares
		// When each one is posted
		// Then the binding layer rejects it with a message naming the field as clients see it
		DescribeTable("rejects a body the binding layer refuses",
			func(body string, wanted string) {
				// Act
				w := send(http.MethodPost, "/appliances/rules", body)

				// Assert
				Expect(w.Code).To(Equal(http.StatusBadRequest))
				Expect(w.Body.String()).To(ContainSubstring(wanted))
			},
			Entry("no conditions", `{
				"name": "r", "vendor": "v", "category": "c",
				"osv_verdict": "unknown", "max_confidence": "low", "min_matched": "1",
				"conditions": []
			}`, "conditions must have at least 1 items"),
			Entry("no name", `{
				"vendor": "v", "category": "c",
				"osv_verdict": "unknown", "max_confidence": "low", "min_matched": "1",
				"conditions": [{"name": "annotation", "expression": "Name = \"a\""}]
			}`, "name is required"),
			Entry("a condition with neither expression nor function", `{
				"name": "r", "vendor": "v", "category": "c",
				"osv_verdict": "unknown", "max_confidence": "low", "min_matched": "1",
				"conditions": [{"name": "annotation"}]
			}`, "exactly one of expression or function must be set"),
			Entry("a condition with both expression and function", `{
				"name": "r", "vendor": "v", "category": "c",
				"osv_verdict": "unknown", "max_confidence": "low", "min_matched": "1",
				"conditions": [{
					"name": "annotation",
					"expression": "Name = \"a\"",
					"function": {"name": "disk_geometry", "disk_geometry": {"disks": ["2048"], "mode": "exact", "tolerance_mib": "0"}}
				}]
			}`, "exactly one of expression or function must be set"),
			Entry("no disk geometry tolerance", `{
				"name": "r", "vendor": "v", "category": "c",
				"osv_verdict": "unknown", "max_confidence": "low", "min_matched": "1",
				"conditions": [{"name": "disk_geometry", "function": {"name": "disk_geometry", "disk_geometry": {"disks": ["2048"], "mode": "exact"}}}]
			}`, "tolerance_mib is required"),
			Entry("an unknown disk geometry mode", `{
				"name": "r", "vendor": "v", "category": "c",
				"osv_verdict": "unknown", "max_confidence": "low", "min_matched": "1",
				"conditions": [{"name": "disk_geometry", "function": {"name": "disk_geometry", "disk_geometry": {"disks": ["2048"], "mode": "loose", "tolerance_mib": "0"}}}]
			}`, "mode must be one of: exact superset"),
			Entry("no disk sizes", `{
				"name": "r", "vendor": "v", "category": "c",
				"osv_verdict": "unknown", "max_confidence": "low", "min_matched": "1",
				"conditions": [{"name": "disk_geometry", "function": {"name": "disk_geometry", "disk_geometry": {"disks": [], "mode": "exact", "tolerance_mib": "0"}}}]
			}`, "disks"),
			Entry("a malformed body", `not json`, "invalid request body"),
		)

		// Given a valid body with one rule broken
		// When each one is posted
		// Then the handler rejects it and says why
		DescribeTable("rejects a rule the handler refuses",
			func(mutate func(*v2api.ApplianceRuleRequest), wanted string) {
				// Arrange
				req := validRequest()
				mutate(&req)

				// Act
				w := send(http.MethodPost, "/appliances/rules", req)

				// Assert
				Expect(w.Code).To(Equal(http.StatusBadRequest))
				Expect(w.Body.String()).To(ContainSubstring(wanted))
			},
			Entry("a blank name", func(req *v2api.ApplianceRuleRequest) {
				req.Name = "   "
			}, "name must not be blank"),
			Entry("a blank condition name", func(req *v2api.ApplianceRuleRequest) {
				req.Conditions = []v2api.ApplianceRuleCondition{{Name: " ", Expression: ptr(`Name = "a"`)}}
			}, "condition[0]: name must not be blank"),
			Entry("two conditions with the same name", func(req *v2api.ApplianceRuleRequest) {
				req.Conditions = append(req.Conditions, req.Conditions[0])
			}, "condition[1]: duplicate condition name"),
			Entry("an expression that does not parse", func(req *v2api.ApplianceRuleRequest) {
				req.Conditions = []v2api.ApplianceRuleCondition{{Name: "annotation", Expression: ptr("Name =")}}
			}, "invalid expression"),
			Entry("min_matched below one", func(req *v2api.ApplianceRuleRequest) {
				req.MinMatched = json.Number("0")
			}, "min_matched must be at least 1"),
			Entry("a fractional min_matched", func(req *v2api.ApplianceRuleRequest) {
				req.MinMatched = json.Number("1.5")
			}, "min_matched must be an integer"),
			Entry("min_matched above the condition count", func(req *v2api.ApplianceRuleRequest) {
				req.MinMatched = json.Number("2")
			}, "min_matched is 2 but the rule has 1 conditions"),
			Entry("a negative tolerance", func(req *v2api.ApplianceRuleRequest) {
				req.Conditions = []v2api.ApplianceRuleCondition{
					geometryCondition([]json.Number{"2048"}, v2api.Exact, json.Number("-1")),
				}
			}, "tolerance must be zero or greater"),
			Entry("a fractional tolerance", func(req *v2api.ApplianceRuleRequest) {
				req.Conditions = []v2api.ApplianceRuleCondition{
					geometryCondition([]json.Number{"2048"}, v2api.Exact, json.Number("0.5")),
				}
			}, "tolerance_mib must be an integer"),
			Entry("a fractional disk size", func(req *v2api.ApplianceRuleRequest) {
				req.Conditions = []v2api.ApplianceRuleCondition{
					geometryCondition([]json.Number{"2048.5"}, v2api.Exact, json.Number("0")),
				}
			}, "disk size"),
			Entry("a blank vendor", func(req *v2api.ApplianceRuleRequest) {
				req.Vendor = "   "
			}, "vendor must not be blank"),
			Entry("a blank category", func(req *v2api.ApplianceRuleRequest) {
				req.Category = "   "
			}, "category must not be blank"),
			Entry("an empty expression", func(req *v2api.ApplianceRuleRequest) {
				req.Conditions = []v2api.ApplianceRuleCondition{{Name: "annotation", Expression: ptr("   ")}}
			}, "expression is empty"),
			Entry("a zero disk size", func(req *v2api.ApplianceRuleRequest) {
				req.Conditions = []v2api.ApplianceRuleCondition{
					geometryCondition([]json.Number{"0"}, v2api.Exact, json.Number("0")),
				}
			}, "must be positive"),
			Entry("a negative disk size", func(req *v2api.ApplianceRuleRequest) {
				req.Conditions = []v2api.ApplianceRuleCondition{
					geometryCondition([]json.Number{"-2048"}, v2api.Exact, json.Number("0")),
				}
			}, "must be positive"),
			Entry("a disk size smaller than the tolerance", func(req *v2api.ApplianceRuleRequest) {
				req.Conditions = []v2api.ApplianceRuleCondition{
					geometryCondition([]json.Number{"1024"}, v2api.Exact, json.Number("4096")),
				}
			}, "must be greater than the tolerance"),
			Entry("a tolerance above the int32 range", func(req *v2api.ApplianceRuleRequest) {
				req.Conditions = []v2api.ApplianceRuleCondition{
					geometryCondition([]json.Number{"2048"}, v2api.Exact, json.Number("4294967296")),
				}
			}, "tolerance should be an int32"),
			Entry("a function without its payload", func(req *v2api.ApplianceRuleRequest) {
				req.Conditions = []v2api.ApplianceRuleCondition{{
					Name:     "disk_geometry",
					Function: &v2api.ApplianceRuleFunction{Name: "disk_geometry"},
				}}
			}, "requires a disk_geometry payload"),
		)

		// Given a request whose condition name is not in the rule vocabulary
		// When the rule is created
		// Then the service rejects it and the handler reports a bad request
		It("should return 400 when the condition name is unknown", func() {
			// Arrange
			req := validRequest()
			req.Conditions = []v2api.ApplianceRuleCondition{{Name: "cpu", Expression: ptr(`CpuCount > 3`)}}

			// Act
			w := send(http.MethodPost, "/appliances/rules", req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusBadRequest))
			Expect(w.Body.String()).To(ContainSubstring("unknown condition"))
		})

		// Given a rule name that is already stored
		// When another rule with the same name is created
		// Then the duplicate is reported as a bad request
		It("should return 400 when the name is already used", func() {
			// Arrange
			createRule(validRequest())

			// Act
			w := send(http.MethodPost, "/appliances/rules", validRequest())

			// Assert
			Expect(w.Code).To(Equal(http.StatusBadRequest))
			Expect(w.Body.String()).To(ContainSubstring("already exists"))
		})
	})

	Context("GET /appliances/rules/{ruleId}", func() {
		// Given a stored rule
		// When it is read by id
		// Then the rule is returned
		It("should return the rule", func() {
			// Arrange
			id := createRule(validRequest())

			// Act
			w := send(http.MethodGet, "/appliances/rules/"+id, nil)

			// Assert
			Expect(w.Code).To(Equal(http.StatusOK))

			var rule v2api.ApplianceRule
			Expect(json.Unmarshal(w.Body.Bytes(), &rule)).To(Succeed())
			Expect(rule.Id).To(HaveValue(Equal(id)))
			Expect(rule.Kind).To(Equal(v2api.Custom))
		})

		// Given no rule with the requested id
		// When it is read by id
		// Then the response is a not found
		It("should return 404 for an unknown id", func() {
			// Act
			w := send(http.MethodGet, "/appliances/rules/4040", nil)

			// Assert
			Expect(w.Code).To(Equal(http.StatusNotFound))
		})

		// Given an id that is not a number
		// When it is read by id
		// Then the response is a bad request
		It("should return 400 for a non numeric id", func() {
			// Act
			w := send(http.MethodGet, "/appliances/rules/not-a-number", nil)

			// Assert
			Expect(w.Code).To(Equal(http.StatusBadRequest))
			Expect(w.Body.String()).To(ContainSubstring("invalid rule ID"))
		})
	})

	Context("PUT /appliances/rules/{ruleId}", func() {
		// Given a stored rule
		// When it is replaced with a different condition
		// Then the response reports an update and the old condition is gone
		It("should return 200 and replace the conditions", func() {
			// Arrange
			id := createRule(validRequest())

			req := validRequest()
			req.Conditions = []v2api.ApplianceRuleCondition{{Name: "name", Expression: ptr(`Name ~ /^ora/`)}}

			// Act
			w := send(http.MethodPut, "/appliances/rules/"+id, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusOK))

			var updated v2api.ApplianceRule
			Expect(json.Unmarshal(w.Body.Bytes(), &updated)).To(Succeed())
			Expect(updated.Id).To(HaveValue(Equal(id)))
			Expect(updated.Conditions).To(Equal([]v2api.ApplianceRuleCondition{
				{Name: "name", Expression: ptr(`Name ~ /^ora/`)},
			}))
		})

		// Given no rule with the requested id
		// When it is replaced
		// Then a rule is created and the response carries the id the server assigned
		It("should return 201 with a new id for an unknown id", func() {
			// Act
			w := send(http.MethodPut, "/appliances/rules/4040", validRequest())

			// Assert
			Expect(w.Code).To(Equal(http.StatusCreated))

			var created v2api.ApplianceRule
			Expect(json.Unmarshal(w.Body.Bytes(), &created)).To(Succeed())
			Expect(created.Id).NotTo(BeNil())
			Expect(*created.Id).NotTo(Equal("4040"))
		})

		// Given an id that is not a positive number
		// When it is replaced
		// Then the request is rejected rather than creating a rule with a new id
		DescribeTable("rejects an id that is not a positive number",
			func(ruleId string) {
				// Act
				w := send(http.MethodPut, "/appliances/rules/"+ruleId, validRequest())

				// Assert
				Expect(w.Code).To(Equal(http.StatusBadRequest))
				Expect(w.Body.String()).To(ContainSubstring("invalid rule ID"))
			},
			Entry("not a number", "not-a-number"),
			Entry("zero", "0"),
			Entry("negative", "-3"),
		)

		// Given a stored rule
		// When it is replaced with an invalid body
		// Then it is rejected and the stored rule is left alone
		It("should return 400 and keep the stored rule", func() {
			// Arrange
			id := createRule(validRequest())

			req := validRequest()
			req.Conditions = nil

			// Act
			w := send(http.MethodPut, "/appliances/rules/"+id, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusBadRequest))

			stored := send(http.MethodGet, "/appliances/rules/"+id, nil)
			Expect(stored.Code).To(Equal(http.StatusOK))
			Expect(stored.Body.String()).To(ContainSubstring("annotation"))
		})
	})

	Context("DELETE /appliances/rules/{ruleId}", func() {
		// Given a stored rule
		// When it is deleted
		// Then the response is empty and the rule is gone
		It("should return 204 and remove the rule", func() {
			// Arrange
			id := createRule(validRequest())

			// Act
			w := send(http.MethodDelete, "/appliances/rules/"+id, nil)

			// Assert
			Expect(w.Code).To(Equal(http.StatusNoContent))
			Expect(w.Body.String()).To(BeEmpty())
			Expect(send(http.MethodGet, "/appliances/rules/"+id, nil).Code).To(Equal(http.StatusNotFound))
		})

		// Given no rule with the requested id
		// When it is deleted
		// Then the response is still empty, because deleting is idempotent
		It("should return 204 for an unknown id", func() {
			// Act
			w := send(http.MethodDelete, "/appliances/rules/4040", nil)

			// Assert
			Expect(w.Code).To(Equal(http.StatusNoContent))
		})

		// Given an id that is not a number
		// When it is deleted
		// Then the response is a bad request
		It("should return 400 for a non numeric id", func() {
			// Act
			w := send(http.MethodDelete, "/appliances/rules/not-a-number", nil)

			// Assert
			Expect(w.Code).To(Equal(http.StatusBadRequest))
		})
	})
})

// ptr returns a pointer to value, for the optional request fields.
func ptr[T any](value T) *T {
	return &value
}
