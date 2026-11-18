package services

import (
	"context"
	"fmt"
	"slices"

	sq "github.com/Masterminds/squirrel"

	"github.com/kubev2v/assisted-migration-agent/internal/filter"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/pkg/errors"
	pkgfilter "github.com/kubev2v/assisted-migration-agent/pkg/filter"
)

type ApplianceService struct {
	store          *store.Store
	conditionFuncs models.ConditionFuncs
}

func NewApplianceService(st *store.Store) (*ApplianceService, error) {
	return &ApplianceService{
		store: st,
		conditionFuncs: models.ConditionFuncs{
			"disk_geometry_exact":    matchExactDiskGeometry,
			"disk_geometry_superset": matchSupersetDiskGeometry,
		},
	}, nil
}

// List returns appliances detected from vApp product metadata, optionally
// narrowed by a filter expression.
func (s *ApplianceService) List(ctx context.Context, byExpression string) ([]models.Appliance, error) {
	var expression sq.Sqlizer
	if byExpression != "" {
		parsed, err := filter.ParseWithApplianceMap([]byte(byExpression))
		if err != nil {
			return nil, errors.NewInvalidFilterError(byExpression, err.Error())
		}
		expression = parsed
	}

	return s.store.Appliance().List(ctx, expression)
}

// Detect returns a list of all (rule,virtual machine) combination either detected as an appliance or not.
// The product len(detectec) x len(notDetected) should be equal to the number of (rule, vm) combination.
func (s *ApplianceService) Detect(ctx context.Context, filterExpression string) (detected, notDetected []models.DetectionResult, err error) {
	virtualMachines, err := NewVMService(s.store).ListForApplianceDetection(ctx, filterExpression)
	if err != nil {
		return []models.DetectionResult{}, []models.DetectionResult{}, fmt.Errorf("listing VMs for estimation: %w", err)
	}

	if len(virtualMachines) == 0 {
		return []models.DetectionResult{}, []models.DetectionResult{}, nil
	}

	rules, err := s.store.Appliance().ListRules(ctx, s.conditionFuncs)
	if err != nil {
		return []models.DetectionResult{}, []models.DetectionResult{}, err
	}

	detected = make([]models.DetectionResult, 0, len(virtualMachines))
	notDetected = make([]models.DetectionResult, 0, len(virtualMachines))
	for _, vm := range virtualMachines {
		for _, rule := range rules {
			result, err := evaluate(vm, rule)
			if err != nil {
				return []models.DetectionResult{}, []models.DetectionResult{}, err
			}

			switch result.Confidence {
			case models.NoConfidence:
				notDetected = append(notDetected, result)
			default:
				detected = append(detected, result)
			}
		}
	}

	return
}

// evaluate evaluates a matched rule against vm by computing the confidence.
// In this context, a "matched" rule means the number of matched conditions should be greater than minMatches.
func evaluate(virtualMachine models.ApplianceVirtualMachine, rule models.Rule) (models.DetectionResult, error) {
	evidence, detectionErr := match(virtualMachine, rule)
	if detectionErr != nil {
		return models.DetectionResult{}, detectionErr
	}

	detectionResult := models.DetectionResult{
		VirtualMachineID:  virtualMachine.ID,
		RuleName:          rule.Name,
		MatchedConditions: make([]string, 0, len(evidence.MatchedConditions)),
		Confidence:        models.NoConfidence,
	}

	for n := range evidence.MatchedConditions {
		detectionResult.MatchedConditions = append(detectionResult.MatchedConditions, n)
	}

	if len(evidence.MatchedConditions) >= rule.MinMatches {
		detectionResult.Confidence = computeConfidence(evidence)

		// per design rule's value is acting as a cap
		if rule.Confidence < detectionResult.Confidence {
			detectionResult.Confidence = rule.Confidence
		}
	}

	return detectionResult, nil
}

// match tries to evaluate each rule's condition against a virtual machine.
// It does not compute confidence.
func match(vm models.ApplianceVirtualMachine, rule models.Rule) (models.Evidence, error) {
	matched := make(map[string]int, len(rule.Conditions))

	for _, condition := range rule.Conditions {
		switch c := condition.(type) {
		case models.ExpressionCondition:
			expr, err := pkgfilter.ParseExpression([]byte(c.Expression))
			if err != nil {
				return models.Evidence{}, errors.NewDetectionError(rule.Name, vm.ID, "expression", c.Name, "", fmt.Sprintf("invalid expression %q: %v", c.Expression, err))
			}

			ok, err := pkgfilter.Eval(expr, vm)
			if err != nil {
				return models.Evidence{}, errors.NewDetectionError(rule.Name, vm.ID, "expression", c.Name, "", err.Error())
			}

			if ok {
				matched[c.Name] = c.Tier
			}

		case models.SingleValueFuncCondition[bool]:
			ok, err := c.Func(c.Value, vm)
			if err != nil {
				return models.Evidence{}, errors.NewDetectionError(rule.Name, vm.ID, "bool", c.Name, "", err.Error())
			}

			if ok {
				matched[c.Name] = c.Tier
			}

		case models.ListValueFuncCondition[[]bool, bool]:
			ok, err := c.Func(c.Values, vm)
			if err != nil {
				return models.Evidence{}, errors.NewDetectionError(rule.Name, vm.ID, "bool", c.Name, "", err.Error())
			}

			if ok {
				matched[c.Name] = c.Tier
			}

		case models.SingleValueFuncCondition[string]:
			ok, err := c.Func(c.Value, vm)
			if err != nil {
				return models.Evidence{}, errors.NewDetectionError(rule.Name, vm.ID, "string", c.Name, "", err.Error())
			}

			if ok {
				matched[c.Name] = c.Tier
			}

		case models.ListValueFuncCondition[[]string, string]:
			ok, err := c.Func(c.Values, vm)
			if err != nil {
				return models.Evidence{}, errors.NewDetectionError(rule.Name, vm.ID, "string", c.Name, "", err.Error())
			}

			if ok {
				matched[c.Name] = c.Tier
			}

		case models.SingleValueFuncCondition[int]:
			ok, err := c.Func(c.Value, vm)
			if err != nil {
				return models.Evidence{}, errors.NewDetectionError(rule.Name, vm.ID, "number", c.Name, "", err.Error())
			}

			if ok {
				matched[c.Name] = c.Tier
			}

		case models.ListValueFuncCondition[[]int, int]:
			ok, err := c.Func(c.Values, vm)
			if err != nil {
				return models.Evidence{}, errors.NewDetectionError(rule.Name, vm.ID, "number", c.Name, "", err.Error())
			}

			if ok {
				matched[c.Name] = c.Tier
			}

		default:
			return models.Evidence{}, errors.NewDetectionError(rule.Name, vm.ID, fmt.Sprintf("%T", condition), "", "", "unsupported condition type")
		}
	}

	return models.Evidence{
		RuleName:          rule.Name,
		VirtualMachineID:  vm.ID,
		MatchedConditions: matched,
	}, nil
}

func computeConfidence(ev models.Evidence) models.ConfidenceLevel {
	tiers := make([]int, 0, len(ev.MatchedConditions))

	for _, v := range ev.MatchedConditions {
		tiers = append(tiers, v)
	}

	slices.Sort(tiers)

	switch tiers[0] {
	case 1, 2:
		return models.ConfidenceHighLevel
	case 3, 4:
		return models.ConfidenceMediumLevel
	case 5:
		return models.ConfidenceLowerLevel
	default:
		return models.NoConfidence
	}
}

// matchExactDiskGeometry expects the persisted disk sizes to describe the VM
// disks exactly. Order is ignored.
func matchExactDiskGeometry(sizes []int, vm models.ApplianceVirtualMachine) (bool, error) {
	expected := slices.Clone(sizes)
	slices.Sort(expected)

	return slices.Equal(expected, diskSizes(vm.Disks)), nil
}

// matchSupersetDiskGeometry expects every persisted disk size to be present
// among the VM disks. Extra VM disks are allowed and order is ignored.
func matchSupersetDiskGeometry(sizes []int, vm models.ApplianceVirtualMachine) (bool, error) {
	expected := slices.Clone(sizes)
	slices.Sort(expected)

	actual := diskSizes(vm.Disks)

	i := 0

	for _, size := range expected {
		for i < len(actual) && actual[i] < size {
			i++
		}

		if i == len(actual) || actual[i] != size {
			return false, nil
		}

		i++
	}

	return true, nil
}

func diskSizes(disks []models.ApplianceDisk) []int {
	sizes := make([]int, 0, len(disks))
	for _, disk := range disks {
		sizes = append(sizes, int(disk.Size))
	}

	slices.Sort(sizes)
	return sizes
}
