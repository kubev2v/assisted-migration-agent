package v2

import (
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	v2 "github.com/kubev2v/assisted-migration-agent/agent/api/v2"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
	pkgfilter "github.com/kubev2v/assisted-migration-agent/pkg/filter"
)

// ListApplianceRules returns all appliance rules.
// (GET /appliances/rules)
func (h *Handler) ListApplianceRules(c *gin.Context) {
	svc, err := h.svc.ApplianceRulesService()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	rules, err := svc.List(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to list appliance rules: %v", err)})
		return
	}

	apiRules := make([]v2.ApplianceRule, 0, len(rules))

	for _, rule := range rules {
		apiRule, err := v2.NewApplianceRuleFromModel(rule)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		apiRules = append(apiRules, apiRule)
	}

	c.JSON(http.StatusOK, v2.ApplianceRuleListResponse{Rules: apiRules})
}

// CreateApplianceRule creates a custom appliance rule.
// (POST /appliances/rules)
func (h *Handler) CreateApplianceRule(c *gin.Context) {
	svc, err := h.svc.ApplianceRulesService()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var req v2.CreateApplianceRuleJSONRequestBody
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": validationErrorMessage(err)})
		return
	}

	if err := validateApplianceRuleRequest(req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	rule, err := v2.NewRuleFromRequest(req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	created, err := svc.Create(c.Request.Context(), rule)
	if err != nil {
		writeApplianceRuleError(c, err)
		return
	}

	apiRule, err := v2.NewApplianceRuleFromModel(created)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, apiRule)
}

// GetApplianceRule returns a single appliance rule.
// (GET /appliances/rules/{ruleId})
func (h *Handler) GetApplianceRule(c *gin.Context, ruleId string) {
	svc, err := h.svc.ApplianceRulesService()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	rid, ok := parseRuleID(c, ruleId)
	if !ok {
		return
	}

	rule, err := svc.Get(c.Request.Context(), rid)
	if err != nil {
		writeApplianceRuleError(c, err)
		return
	}

	apiRule, err := v2.NewApplianceRuleFromModel(rule)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, apiRule)
}

// UpdateApplianceRule replaces an appliance rule and its whole condition set. A
// rule that does not exist is created.
// (PUT /appliances/rules/{ruleId})
func (h *Handler) UpdateApplianceRule(c *gin.Context, ruleId string) {
	svc, err := h.svc.ApplianceRulesService()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	rid, ok := parseRuleID(c, ruleId)
	if !ok {
		return
	}

	var req v2.UpdateApplianceRuleJSONRequestBody
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": validationErrorMessage(err)})
		return
	}

	if err := validateApplianceRuleRequest(req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	rule, err := v2.NewRuleFromRequest(req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	rule.ID = rid

	updated, created, err := svc.Update(c.Request.Context(), rule)
	if err != nil {
		writeApplianceRuleError(c, err)
		return
	}

	apiRule, err := v2.NewApplianceRuleFromModel(updated)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}

	c.JSON(status, apiRule)
}

// DeleteApplianceRule removes an appliance rule. Deleting a rule that does not
// exist is not an error.
// (DELETE /appliances/rules/{ruleId})
func (h *Handler) DeleteApplianceRule(c *gin.Context, ruleId string) {
	svc, err := h.svc.ApplianceRulesService()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	rid, ok := parseRuleID(c, ruleId)
	if !ok {
		return
	}

	if err := svc.Delete(c.Request.Context(), rid); err != nil {
		writeApplianceRuleError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

// validateApplianceRuleRequest checks the request against the rules the API can
// express: no blank text, unique condition names, a parseable expression, a disk
// geometry payload with whole numbers and a non negative tolerance, and a
// min_matched inside the condition count. What a condition or function name means
// stays in the service.
func validateApplianceRuleRequest(req v2.ApplianceRuleRequest) error {
	if strings.TrimSpace(req.Name) == "" {
		return fmt.Errorf("name must not be blank")
	}

	if strings.TrimSpace(req.Vendor) == "" {
		return fmt.Errorf("vendor must not be blank")
	}

	if strings.TrimSpace(req.Category) == "" {
		return fmt.Errorf("category must not be blank")
	}

	names := make(map[string]struct{}, len(req.Conditions))

	for i, condition := range req.Conditions {
		if strings.TrimSpace(condition.Name) == "" {
			return fmt.Errorf("condition[%d]: name must not be blank", i)
		}

		if _, ok := names[condition.Name]; ok {
			return fmt.Errorf("condition[%d]: duplicate condition name %q", i, condition.Name)
		}
		names[condition.Name] = struct{}{}

		if condition.Function == nil && condition.Expression == nil {
			return fmt.Errorf("condition[%s]: invalid condition: missing either function name or expression", condition.Name)
		}

		if condition.Function != nil && condition.Expression != nil {
			return fmt.Errorf("condition[%s]: invalid condition: both function name and expression are provided", condition.Name)
		}

		if condition.Function != nil {
			if err := validateConditionFunction(i, condition.Function); err != nil {
				return err
			}

			continue
		}

		if len(strings.TrimSpace(*condition.Expression)) == 0 {
			return fmt.Errorf("condition[%d]: invalid condition: expression is empty", i)
		}

		if err := pkgfilter.Validate([]byte(*condition.Expression)); err != nil {
			return fmt.Errorf("condition[%d]: invalid expression: %v", i, err)
		}
	}

	minMatched, err := req.MinMatched.Int64()
	if err != nil {
		return fmt.Errorf("min_matched must be an integer, got %q", req.MinMatched)
	}

	if minMatched < 1 {
		return fmt.Errorf("min_matched must be at least 1")
	}

	if minMatched > int64(len(req.Conditions)) {
		return fmt.Errorf("min_matched is %d but the rule has %d conditions", minMatched, len(req.Conditions))
	}

	return nil
}

// parseRuleID reads the rule id from the path. A rule id is a positive integer.
func parseRuleID(c *gin.Context, ruleId string) (int64, bool) {
	id, err := strconv.ParseInt(ruleId, 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid rule ID"})
		return 0, false
	}

	return id, true
}

// validateConditionFunction checks the payload of a function condition. Which
// functions exist, and what a condition name means, are the service's business
// rules.
func validateConditionFunction(index int, function *v2.ApplianceRuleFunction) error {
	geometry := function.DiskGeometry
	if geometry == nil {
		return fmt.Errorf("condition[%d]: function %q requires a disk_geometry payload", index, function.Name)
	}

	tolerance, err := geometry.ToleranceMib.Int64()
	if err != nil {
		return fmt.Errorf("condition[%d]: tolerance_mib must be an integer, got %q", index, geometry.ToleranceMib)
	}

	if tolerance < 0 {
		return fmt.Errorf("condition[%d]: disk_geometry tolerance must be zero or greater", index)
	}

	if math.MaxInt32 < tolerance {
		return fmt.Errorf("condition[%d]: tolerance should be an int32 %d", index, tolerance)
	}

	if len(geometry.Disks) == 0 {
		return fmt.Errorf("condition[%d]: at least one disk size should be specified", index)
	}

	for _, size := range geometry.Disks {
		actual, err := size.Int64()
		if err != nil {
			return fmt.Errorf("condition[%d]: disk size %q must be an integer", index, size)
		}
		if actual <= 0 {
			return fmt.Errorf("condition[%d]: disk size %q must be positive and greater than 1", index, size)
		}

		if actual < tolerance {
			return fmt.Errorf("condition[%d]: disk size %d must be greater than the tolerance %d", index, actual, tolerance)
		}
	}

	return nil
}

// writeApplianceRuleError maps service errors to status codes.
func writeApplianceRuleError(c *gin.Context, err error) {
	switch {
	case srvErrors.IsInvalidRuleError(err), srvErrors.IsDuplicateResourceError(err):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case srvErrors.IsResourceNotFoundError(err):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	}
}
