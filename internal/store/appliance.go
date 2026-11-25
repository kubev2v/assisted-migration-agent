package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	sq "github.com/Masterminds/squirrel"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

const (
	rulesTable          = "rules"
	ruleConditionsTable = "rule_conditions"

	ruleColID         = "id"
	ruleColName       = "name"
	ruleColVendor     = "vendor"
	ruleColCategory   = "category"
	ruleColKind       = "kind"
	ruleColOsvVerdict = "osv_verdict"
	ruleColConfidence = "confidence"
	ruleColMinMatched = "min_matched"

	conditionColID         = "id"
	conditionColName       = "name"
	conditionColTier       = "tier"
	conditionColExpression = "expression"
	conditionColFuncName   = "func_name"
	conditionColRuleID     = "rule_id"

	diskGeometryTable        = "disk_geometry_condition"
	geometryColDisks         = "disks"
	geometryColTolerance     = "tolerance"
	geometryColMode          = "mode"
	geometryColRuleCondition = "rule_condition_id"

	// diskGeometryFuncPrefix prefixes every disk geometry predicate name, so a
	// persisted func_name can be checked against the payload mode.
	diskGeometryFuncPrefix = "disk_geometry_"
)

// listRulesStm reads one row per condition. Expression conditions carry the
// expression and no payload. Function conditions carry a func_name and their
// arguments live in disk_geometry_condition, joined through the rule_conditions
// foreign key. A func_name whose payload row is missing comes back with NULL
// payload columns, so the reader rejects it instead of dropping the condition.
var listRulesStm = sq.Select(
	"r.id",
	"r.name",
	"r.vendor",
	"COALESCE(r.kind, 0) AS kind",
	"r.category",
	"r.osv_verdict",
	"r.confidence",
	"r.min_matched",
	"c.name",
	"c.tier",
	"c.expression",
	"c.func_name",
	"d.disks",
	"d.tolerance",
	"d.mode",
).From("rules r").
	Join("rule_conditions c ON c.rule_id = r.id").
	LeftJoin("disk_geometry_condition d ON d.rule_condition_id = c.id").
	OrderBy("r.name", "r.id", "c.tier", "c.name", "c.id")

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
	return s.readRules(ctx, conditionFuncs, listRulesStm)
}

// GetRule returns the rule with the given id.
func (s *ApplianceStore) GetRule(ctx context.Context, conditionFuncs models.ConditionFuncs, id int64) (models.Rule, error) {
	rules, err := s.readRules(ctx, conditionFuncs, listRulesStm.Where(sq.Eq{"r.id": id}))
	if err != nil {
		return models.Rule{}, err
	}

	if len(rules) == 0 {
		return models.Rule{}, srvErrors.NewResourceNotFoundError("appliance rule", strconv.FormatInt(id, 10))
	}

	return rules[0], nil
}

// CreateRule inserts a rule and its conditions, and returns it with the
// generated id. The caller owns the transaction.
func (s *ApplianceStore) CreateRule(ctx context.Context, rule models.Rule) (models.Rule, error) {
	query, args, err := sq.Insert(rulesTable).
		Columns(
			ruleColName,
			ruleColVendor,
			ruleColCategory,
			ruleColKind,
			ruleColOsvVerdict,
			ruleColConfidence,
			ruleColMinMatched,
		).
		Values(
			rule.Name,
			rule.Vendor,
			rule.Category,
			int(rule.RuleKind),
			int(rule.OsvVerdict),
			int(rule.Confidence),
			rule.MinMatches,
		).
		Suffix("RETURNING " + ruleColID).
		ToSql()
	if err != nil {
		return models.Rule{}, fmt.Errorf("building create rule query: %w", err)
	}

	var ruleID int64
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&ruleID); err != nil {
		if isUniqueConstraintError(err) {
			return models.Rule{}, srvErrors.NewDuplicateResourceError("rule", "name", rule.Name)
		}

		return models.Rule{}, fmt.Errorf("creating rule: %w", err)
	}

	if err := s.insertConditions(ctx, ruleID, rule.Conditions); err != nil {
		return models.Rule{}, err
	}

	rule.ID = ruleID

	return rule, nil
}

// UpdateRule replaces the rule fields and its whole condition set. The rule
// kind is fixed at creation. The caller owns the transaction.
func (s *ApplianceStore) UpdateRule(ctx context.Context, rule models.Rule) error {
	// Conditions and their payloads are removed before the rule row is updated
	// because DuckDB refuses to update a row that is still referenced by a
	// foreign key.
	if err := s.deleteConditions(ctx, rule.ID); err != nil {
		return err
	}

	query, args, err := sq.Update(rulesTable).
		Set(ruleColName, rule.Name).
		Set(ruleColVendor, rule.Vendor).
		Set(ruleColCategory, rule.Category).
		Set(ruleColOsvVerdict, int(rule.OsvVerdict)).
		Set(ruleColConfidence, int(rule.Confidence)).
		Set(ruleColMinMatched, rule.MinMatches).
		Where(sq.Eq{ruleColID: rule.ID}).
		Suffix("RETURNING " + ruleColID).
		ToSql()
	if err != nil {
		return fmt.Errorf("building update rule query: %w", err)
	}

	// An update reports no changed rows when the stored values already match, so
	// the returned id is what tells a missing rule apart from a no-op update.
	var updatedID int64
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&updatedID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return srvErrors.NewResourceNotFoundError("appliance rule", strconv.FormatInt(rule.ID, 10))
		}

		if isUniqueConstraintError(err) {
			return srvErrors.NewDuplicateResourceError("rule", "name", rule.Name)
		}

		return fmt.Errorf("updating rule: %w", err)
	}

	return s.insertConditions(ctx, rule.ID, rule.Conditions)
}

// DeleteRule removes a rule and its conditions. It is idempotent: deleting a
// rule that does not exist is not an error. The caller owns the transaction.
func (s *ApplianceStore) DeleteRule(ctx context.Context, id int64) error {
	if err := s.deleteConditions(ctx, id); err != nil {
		return err
	}

	query, args, err := sq.Delete(rulesTable).Where(sq.Eq{ruleColID: id}).ToSql()
	if err != nil {
		return fmt.Errorf("building delete rule query: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("deleting rule: %w", err)
	}

	return nil
}

// insertConditions writes one rule_conditions row per condition. Function
// conditions also write their arguments to the table that owns them; the
// condition row id is the foreign key, so the payload row can only exist for a
// condition that exists.
func (s *ApplianceStore) insertConditions(ctx context.Context, ruleID int64, conditions []any) error {
	for _, condition := range conditions {
		switch c := condition.(type) {
		case models.ExpressionCondition:
			query, args, err := sq.Insert(ruleConditionsTable).
				Columns(conditionColName, conditionColTier, conditionColExpression, conditionColRuleID).
				Values(c.Name, c.Tier, c.Expression, ruleID).
				ToSql()
			if err != nil {
				return fmt.Errorf("building insert condition query: %w", err)
			}

			if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
				return fmt.Errorf("inserting rule condition: %w", err)
			}

		case models.FuncCondition[models.DiskGeometry]:
			geometry := c.Data
			if c.FuncName != diskGeometryFuncPrefix+string(geometry.Mode) {
				return fmt.Errorf("condition %q: function name %q does not match disk geometry mode %q", c.Name, c.FuncName, geometry.Mode)
			}

			query, args, err := sq.Insert(ruleConditionsTable).
				Columns(conditionColName, conditionColTier, conditionColFuncName, conditionColRuleID).
				Values(c.Name, c.Tier, c.FuncName, ruleID).
				Suffix("RETURNING " + conditionColID).
				ToSql()
			if err != nil {
				return fmt.Errorf("building insert condition query: %w", err)
			}

			var conditionID int64
			if err := s.db.QueryRowContext(ctx, query, args...).Scan(&conditionID); err != nil {
				return fmt.Errorf("inserting rule condition: %w", err)
			}

			query, args, err = sq.Insert(diskGeometryTable).
				Columns(geometryColDisks, geometryColTolerance, geometryColMode, geometryColRuleCondition).
				Values(geometry.Disks, geometry.Tolerance, string(geometry.Mode), conditionID).
				ToSql()
			if err != nil {
				return fmt.Errorf("building insert disk geometry query: %w", err)
			}

			if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
				return fmt.Errorf("inserting disk geometry condition: %w", err)
			}

		default:
			return fmt.Errorf("unsupported condition type %T", condition)
		}
	}

	return nil
}

// deleteConditions removes the payload rows of a rule before the conditions
// they belong to. DuckDB has no ON DELETE CASCADE and refuses to touch a row
// that is still referenced by a foreign key.
func (s *ApplianceStore) deleteConditions(ctx context.Context, ruleID int64) error {
	conditionIDs, conditionArgs, err := sq.Select(conditionColID).
		From(ruleConditionsTable).
		Where(sq.Eq{conditionColRuleID: ruleID}).
		ToSql()
	if err != nil {
		return fmt.Errorf("building delete disk geometry query: %w", err)
	}

	query, args, err := sq.Delete(diskGeometryTable).
		Where(sq.Expr(geometryColRuleCondition+" IN ("+conditionIDs+")", conditionArgs...)).
		ToSql()
	if err != nil {
		return fmt.Errorf("building delete disk geometry query: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("deleting disk geometry conditions: %w", err)
	}

	query, args, err = sq.Delete(ruleConditionsTable).Where(sq.Eq{conditionColRuleID: ruleID}).ToSql()
	if err != nil {
		return fmt.Errorf("building delete conditions query: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("deleting rule conditions: %w", err)
	}

	return nil
}

// readRules scans one row per condition of the given rules query.
func (s *ApplianceStore) readRules(ctx context.Context, conditionFuncs models.ConditionFuncs, query sq.SelectBuilder) ([]models.Rule, error) {
	stmt, args, err := query.ToSql()
	if err != nil {
		return nil, fmt.Errorf("building rules query: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, stmt, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var (
		rules      []models.Rule
		lastRuleID int64 = -1
	)

	for rows.Next() {
		var (
			ruleID, tier                                 int64
			ruleKind, osvVerdict, confidence, minMatched int
			ruleName, ruleVendor, ruleCategory           string
			conditionName                                string
			expression, funcName                         sql.NullString
			disks                                        IntArray
			tolerance                                    sql.NullInt64
			mode                                         sql.NullString
		)

		if err := rows.Scan(
			&ruleID,
			&ruleName,
			&ruleVendor,
			&ruleKind,
			&ruleCategory,
			&osvVerdict,
			&confidence,
			&minMatched,
			&conditionName,
			&tier,
			&expression,
			&funcName,
			&disks,
			&tolerance,
			&mode,
		); err != nil {
			return nil, err
		}

		if ruleID != lastRuleID {
			rules = append(rules, models.Rule{
				ID:         ruleID,
				Name:       ruleName,
				Vendor:     ruleVendor,
				RuleKind:   models.RuleKind(ruleKind),
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
				Tier:       int(tier),
				Expression: expression.String,
			})

			continue
		}

		if !funcName.Valid {
			return nil, fmt.Errorf("rule %q condition %q: neither expression nor function is set", ruleName, conditionName)
		}

		if !strings.HasPrefix(funcName.String, diskGeometryFuncPrefix) {
			return nil, fmt.Errorf("rule %q condition %q: condition function %q is not supported", ruleName, conditionName, funcName.String)
		}

		// The payload is joined through the rule_conditions foreign key, so a
		// missing row means the condition and its payload disagree.
		if !mode.Valid {
			return nil, fmt.Errorf("rule %q condition %q: condition function %q has no disk geometry row", ruleName, conditionName, funcName.String)
		}

		if funcName.String != diskGeometryFuncPrefix+mode.String {
			return nil, fmt.Errorf("rule %q condition %q: condition function %q does not match disk geometry mode %q", ruleName, conditionName, funcName.String, mode.String)
		}

		implementation, found := conditionFuncs[funcName.String]
		if !found {
			return nil, fmt.Errorf("rule %q condition %q: condition function %q is not registered", ruleName, conditionName, funcName.String)
		}

		fn, ok := implementation.(func(models.DiskGeometry, models.ApplianceVirtualMachine) (bool, error))
		if !ok {
			return nil, fmt.Errorf("rule %q condition %q: condition function %q is %T, expected func(models.DiskGeometry, models.ApplianceVirtualMachine) (bool, error)", ruleName, conditionName, funcName.String, implementation)
		}

		rule.Conditions = append(rule.Conditions, models.FuncCondition[models.DiskGeometry]{
			Data: models.DiskGeometry{
				Disks:     disks,
				Tolerance: int(tolerance.Int64),
				Mode:      models.DiskGeometryMode(mode.String),
			},
			Name:     conditionName,
			Tier:     int(tier),
			FuncName: funcName.String,
			Func:     fn,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return rules, nil
}
