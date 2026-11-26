package services

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/pkg/errors"
	pkgfilter "github.com/kubev2v/assisted-migration-agent/pkg/filter"
)

const (
	diskGeometryCondition = "disk_geometry"
)

// ruleConditionTiers is the closed vocabulary of condition names a rule may
// use. The tier tells how strongly a matched condition implies the appliance:
// 1-2 high, 3-4 medium, 5 low. A condition name missing here is rejected,
// because the tier drives the confidence the rule can produce.
var ruleConditionTiers = map[string]int{
	"vapp_product":  1,
	"annotation":    2,
	"guest_id":      3,
	"mount_preffix": 4,
	"nic_count":     5,
	"disk_geometry": 5,
	"nested_hv":     5,
	"name":          6,
}

type ApplianceRulesService struct {
	store          *store.Store
	conditionFuncs models.ConditionFuncs
}

func NewApplianceRulesService(st *store.Store) (*ApplianceRulesService, error) {
	return &ApplianceRulesService{
		store: st,
		conditionFuncs: models.ConditionFuncs{
			"disk_geometry_exact":    matchDiskGeometry,
			"disk_geometry_superset": matchDiskGeometry,
		},
	}, nil
}

func (s *ApplianceRulesService) List(ctx context.Context) ([]models.Rule, error) {
	return s.store.Appliance().ListRules(ctx, s.conditionFuncs)
}

func (s *ApplianceRulesService) Get(ctx context.Context, ruleId int64) (models.Rule, error) {
	return s.store.Appliance().GetRule(ctx, s.conditionFuncs, ruleId)
}

func (s *ApplianceRulesService) Create(ctx context.Context, rule models.Rule) (models.Rule, error) {
	resolved, err := s.resolveRule(rule)
	if err != nil {
		return models.Rule{}, err
	}

	// The kind is server owned: anything a client sends is dropped.
	resolved.RuleKind = models.CustomRule

	var created models.Rule
	err = s.store.WithTx(ctx, func(txCtx context.Context) error {
		stored, err := s.store.Appliance().CreateRule(txCtx, resolved)
		if err != nil {
			return err
		}

		created, err = s.store.Appliance().GetRule(txCtx, s.conditionFuncs, stored.ID)
		return err
	})
	if err != nil {
		return models.Rule{}, err
	}

	return created, nil
}

// UpdateRule replaces a rule and its conditions, or creates the rule when its
// id does not exist. The second return value reports which of the two happened.
// The kind of an existing rule is never changed.
func (s *ApplianceRulesService) Update(ctx context.Context, r models.Rule) (rule models.Rule, created bool, err error) {
	resolved, err := s.resolveRule(r)
	if err != nil {
		return models.Rule{}, false, err
	}

	err = s.store.WithTx(ctx, func(txCtx context.Context) error {
		_, err := s.store.Appliance().GetRule(txCtx, s.conditionFuncs, resolved.ID)
		switch {
		case err == nil:
			if err := s.store.Appliance().UpdateRule(txCtx, resolved); err != nil {
				return err
			}

			rule, err = s.store.Appliance().GetRule(txCtx, s.conditionFuncs, resolved.ID)

			return err
		case errors.IsResourceNotFoundError(err):
			// The id is assigned by the store, so the client id is dropped.
			resolved.ID = 0
			resolved.RuleKind = models.CustomRule

			stored, err := s.store.Appliance().CreateRule(txCtx, resolved)
			if err != nil {
				return err
			}

			created = true
			rule, err = s.store.Appliance().GetRule(txCtx, s.conditionFuncs, stored.ID)

			return err
		default:
			return err
		}
	})
	if err != nil {
		return models.Rule{}, false, err
	}

	return rule, created, nil
}

// DeleteRule removes a rule and its conditions. Deleting a rule that does not
// exist is not an error.
func (s *ApplianceRulesService) Delete(ctx context.Context, id int64) error {
	return s.store.WithTx(ctx, func(txCtx context.Context) error {
		return s.store.Appliance().DeleteRule(txCtx, id)
	})
}

func (s *ApplianceRulesService) resolveRule(rule models.Rule) (models.Rule, error) {
	rule.Name = strings.TrimSpace(rule.Name)
	rule.Vendor = strings.TrimSpace(rule.Vendor)
	rule.Category = strings.TrimSpace(rule.Category)

	conditions := make([]any, 0, len(rule.Conditions))

	// A models.ConditionInput is resolved here. Any other type already carries
	// an implementation and is used as it is.
	for i, condition := range rule.Conditions {
		input, ok := condition.(models.ConditionInput)
		if !ok {
			conditions = append(conditions, condition)
			continue
		}

		resolved, err := s.resolveCondition(i, input)
		if err != nil {
			return models.Rule{}, err
		}

		conditions = append(conditions, resolved)
	}

	rule.Conditions = conditions

	return rule, nil
}

// resolveCondition turns one client condition into an executable one. The index
// is only used to point at the offending condition in errors.
func (s *ApplianceRulesService) resolveCondition(index int, input models.ConditionInput) (any, error) {
	tier, ok := ruleConditionTiers[input.Name]
	if !ok {
		known := make([]string, 0, len(ruleConditionTiers))
		for name := range ruleConditionTiers {
			known = append(known, name)
		}
		slices.Sort(known)

		return nil, errors.NewInvalidRuleError(fmt.Sprintf(
			"condition[%d]: unknown condition %q, known conditions: %s",
			index, input.Name, strings.Join(known, ", "),
		))
	}

	switch {
	case input.Expression != "":
		expression, err := pkgfilter.ParseExpression([]byte(input.Expression))
		if err != nil {
			return nil, errors.NewInvalidRuleError(fmt.Sprintf("condition[%d]: invalid expression %q: %v", index, input.Expression, err))
		}

		// Evaluating against an empty VM catches expressions that reference
		// attributes rules cannot match on, or that compare incompatible
		// types.
		if _, err := pkgfilter.Eval(expression, models.ApplianceVirtualMachine{}); err != nil {
			return nil, errors.NewInvalidRuleError(fmt.Sprintf("condition[%d]: expression %q cannot be evaluated: %v", index, input.Expression, err))
		}

		return models.ExpressionCondition{
			Name:       input.Name,
			Tier:       tier,
			Expression: input.Expression,
		}, nil

	case input.Function != nil:
		function := input.Function

		switch function.Name {
		case diskGeometryCondition:
			if function.DiskGeometry == nil {
				return nil, errors.NewInvalidRuleError(fmt.Sprintf(
					"condition[%d]: function %q requires a disk_geometry payload", index, function.Name,
				))
			}

			funcName := "disk_geometry_" + string(function.DiskGeometry.Mode)
			match, ok := s.conditionFuncs[funcName]
			if !ok {
				return nil, errors.NewInvalidRuleError(fmt.Sprintf(
					"condition[%d]: unknown disk geometry mode %q", index, function.DiskGeometry.Mode,
				))
			}

			matchFunc, ok := match.(func(models.DiskGeometry, models.ApplianceVirtualMachine) (bool, error))
			if !ok {
				return nil, fmt.Errorf("condition function %q has unexpected type %T", funcName, match)
			}

			return models.FuncCondition[models.DiskGeometry]{
				Data:     *function.DiskGeometry,
				Name:     input.Name,
				Tier:     tier,
				FuncName: funcName,
				Func:     matchFunc,
			}, nil
		default:
			return nil, errors.NewInvalidRuleError(fmt.Sprintf(
				"condition[%d]: unknown function %q, known functions: disk_geometry", index, function.Name,
			))
		}

	default:
		return nil, errors.NewInvalidRuleError(fmt.Sprintf("condition[%d]: either expression or function must be set", index))
	}
}
