package store

import (
	"context"
	"database/sql"
	"fmt"

	sq "github.com/Masterminds/squirrel"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
)

// listRulesQuery folds the row-per-value conditions into one row per condition.
// A condition is either a filter expression or a named function with typed
// values. The aggregated value column that survives tells the func condition
// kind: a single value becomes a single value condition, more than one becomes
// a list value condition. expression and func_name are constant within a
// group, so they are selected directly instead of being aggregated.
const listRulesQuery = `
	SELECT
		r.id,
		r.name,
		r.vendor,
		r.category,
		r.osv_verdict,
		r.confidence,
		r.min_matched,
		c.name,
		c.tier,
		c.expression,
		c.func_name,
		list_filter(list(c.string_value ORDER BY c.id), lambda x: x IS NOT NULL) AS string_values,
		list_filter(list(c.int_value ORDER BY c.id), lambda x: x IS NOT NULL) AS int_values,
		list_filter(list(c.bool_value ORDER BY c.id), lambda x: x IS NOT NULL) AS bool_values
	FROM rules r
	JOIN rule_conditions c ON c.rule_id = r.id
	GROUP BY ALL
	ORDER BY r.name, r.id, c.tier, c.name
`

type ApplianceStore struct {
	db QueryInterceptor
}

func NewApplianceStore(db QueryInterceptor) *ApplianceStore {
	return &ApplianceStore{db: db}
}

// List returns appliances grouped by product name, sorted alphabetically.
func (s *ApplianceStore) List(ctx context.Context, filter sq.Sqlizer) ([]models.Appliance, error) {
	selectQuery := sq.Select(
		`p.name`,
		`p.vendor`,
		`p.version`,
		`p.full_version`,
		`p.vendor_url`,
		`p.product_url`,
		`p.app_url`,
		`p.vm_id`,
		`v."VM"`,
	).From("vm_vapp_products p").
		LeftJoin(`vinfo v ON v."VM ID" = p.vm_id`).
		Where(sq.And{sq.NotEq{"p.name": nil}, sq.NotEq{"p.name": ""}})

	if filter != nil {
		selectQuery = selectQuery.Where(filter)
	}

	selectQuery = selectQuery.OrderBy("p.name", "p.vm_id", "p.key")

	query, args, err := selectQuery.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building list query: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var results []models.Appliance
	var current *models.Appliance

	for rows.Next() {
		var (
			vmID, name                                                          string
			vendor, version, fullVersion, vendorUrl, productUrl, appUrl, vmName sql.NullString
		)

		if err := rows.Scan(
			&name,
			&vendor,
			&version,
			&fullVersion,
			&vendorUrl,
			&productUrl,
			&appUrl,
			&vmID,
			&vmName,
		); err != nil {
			return nil, err
		}

		if current == nil || current.Name != name {
			if current != nil {
				results = append(results, *current)
			}

			current = &models.Appliance{
				Name:            name,
				Vendor:          "Unknown",
				Confidence:      100,
				VirtualMachines: models.VirtualMachinesIdentites{},
			}

			if vendor.Valid && vendor.String != "" {
				current.Vendor = vendor.String
			}
			if version.Valid {
				current.Version = version.String
			}
			if fullVersion.Valid {
				current.FullVersion = fullVersion.String
			}
			if vendorUrl.Valid {
				current.VendorUrl = vendorUrl.String
			}
			if productUrl.Valid {
				current.ProductUrl = productUrl.String
			}
			if appUrl.Valid {
				current.ApplicationUrl = appUrl.String
			}
		}

		n := ""
		if vmName.Valid {
			n = vmName.String
		}
		current.VirtualMachines[vmID] = n
	}

	if current != nil {
		results = append(results, *current)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return results, nil
}

// ListRules returns the appliance detection rules with their conditions.
// Function conditions are persisted by name, so the caller supplies the map
// that turns those names back into predicates.
func (s *ApplianceStore) ListRules(ctx context.Context, conditionFuncs models.ConditionFuncs) ([]models.Rule, error) {
	rows, err := s.db.QueryContext(ctx, listRulesQuery)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var (
		rules      []models.Rule
		lastRuleID = -1
	)

	for rows.Next() {
		var (
			ruleID, osvVerdict, confidence, minMatched int
			tier                                       int
			ruleName, ruleVendor, ruleCategory         string
			conditionName                              string
			expression, funcName                       sql.NullString
			stringValues                               StringArray
			intValues                                  IntArray
			boolValues                                 BoolArray
		)

		if err := rows.Scan(
			&ruleID,
			&ruleName,
			&ruleVendor,
			&ruleCategory,
			&osvVerdict,
			&confidence,
			&minMatched,
			&conditionName,
			&tier,
			&expression,
			&funcName,
			&stringValues,
			&intValues,
			&boolValues,
		); err != nil {
			return nil, err
		}

		if ruleID != lastRuleID {
			rules = append(rules, models.Rule{
				Name:       ruleName,
				Vendor:     ruleVendor,
				Category:   ruleCategory,
				OsvVerdict: models.OsvVerdict(osvVerdict),
				Confidence: models.ConfidenceLevel(confidence),
				MinMatches: minMatched,
			})

			lastRuleID = ruleID
		}

		rule := &rules[len(rules)-1]

		if expression.Valid {
			rule.Conditions = append(rule.Conditions, models.ExpressionCondition{
				Name:       conditionName,
				Tier:       tier,
				Expression: expression.String,
			})

			continue
		}

		implementation, found := conditionFuncs[funcName.String]
		if !found {
			return nil, fmt.Errorf("rule %q condition %q: condition function %q is not registered", ruleName, conditionName, funcName.String)
		}

		switch {
		case len(boolValues) == 1:
			fn, ok := implementation.(func(bool, models.ApplianceVirtualMachine) (bool, error))
			if !ok {
				return nil, fmt.Errorf("rule %q condition %q: condition function %q is %T, expected func(bool, models.ApplianceVirtualMachine) (bool, error)", ruleName, conditionName, funcName.String, implementation)
			}

			rule.Conditions = append(rule.Conditions, models.SingleValueFuncCondition[bool]{
				Value: boolValues[0],
				Name:  conditionName,
				Tier:  tier,
				Func:  fn,
			})

		case len(boolValues) > 1:
			fn, ok := implementation.(func([]bool, models.ApplianceVirtualMachine) (bool, error))
			if !ok {
				return nil, fmt.Errorf("rule %q condition %q: condition function %q is %T, expected func([]bool, models.ApplianceVirtualMachine) (bool, error)", ruleName, conditionName, funcName.String, implementation)
			}

			rule.Conditions = append(rule.Conditions, models.ListValueFuncCondition[[]bool, bool]{
				Values: boolValues,
				Name:   conditionName,
				Tier:   tier,
				Func:   fn,
			})

		case len(stringValues) == 1:
			fn, ok := implementation.(func(string, models.ApplianceVirtualMachine) (bool, error))
			if !ok {
				return nil, fmt.Errorf("rule %q condition %q: condition function %q is %T, expected func(string, models.ApplianceVirtualMachine) (bool, error)", ruleName, conditionName, funcName.String, implementation)
			}

			rule.Conditions = append(rule.Conditions, models.SingleValueFuncCondition[string]{
				Value: stringValues[0],
				Name:  conditionName,
				Tier:  tier,
				Func:  fn,
			})

		case len(stringValues) > 1:
			fn, ok := implementation.(func([]string, models.ApplianceVirtualMachine) (bool, error))
			if !ok {
				return nil, fmt.Errorf("rule %q condition %q: condition function %q is %T, expected func([]string, models.ApplianceVirtualMachine) (bool, error)", ruleName, conditionName, funcName.String, implementation)
			}

			rule.Conditions = append(rule.Conditions, models.ListValueFuncCondition[[]string, string]{
				Values: stringValues,
				Name:   conditionName,
				Tier:   tier,
				Func:   fn,
			})

		case len(intValues) == 1:
			fn, ok := implementation.(func(int, models.ApplianceVirtualMachine) (bool, error))
			if !ok {
				return nil, fmt.Errorf("rule %q condition %q: condition function %q is %T, expected func(int, models.ApplianceVirtualMachine) (bool, error)", ruleName, conditionName, funcName.String, implementation)
			}

			rule.Conditions = append(rule.Conditions, models.SingleValueFuncCondition[int]{
				Value: intValues[0],
				Name:  conditionName,
				Tier:  tier,
				Func:  fn,
			})

		case len(intValues) > 1:
			fn, ok := implementation.(func([]int, models.ApplianceVirtualMachine) (bool, error))
			if !ok {
				return nil, fmt.Errorf("rule %q condition %q: condition function %q is %T, expected func([]int, models.ApplianceVirtualMachine) (bool, error)", ruleName, conditionName, funcName.String, implementation)
			}

			rule.Conditions = append(rule.Conditions, models.ListValueFuncCondition[[]int, int]{
				Values: intValues,
				Name:   conditionName,
				Tier:   tier,
				Func:   fn,
			})
		}
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return rules, nil
}
