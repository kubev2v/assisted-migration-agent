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
	store        *store.Store
	rulesService *ApplianceRulesService
}

func NewApplianceService(st *store.Store, rulesService *ApplianceRulesService) (*ApplianceService, error) {
	return &ApplianceService{
		store:        st,
		rulesService: rulesService,
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

	rules, err := s.rulesService.List(ctx)
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

		case models.FuncCondition[models.DiskGeometry]:
			ok, err := c.Func(c.Data, vm)
			if err != nil {
				return models.Evidence{}, errors.NewDetectionError(rule.Name, vm.ID, "disk_geometry", c.Name, "", err.Error())
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

// matchDiskGeometry expects every persisted disk size to be present among the
// VM disks, each one within the tolerance of its configured size. Order is
// ignored. In exact mode the VM must carry the same number of disks; in
// superset mode it may carry more.
func matchDiskGeometry(diskGeometry models.DiskGeometry, vm models.ApplianceVirtualMachine) (bool, error) {
	expected := slices.Clone(diskGeometry.Disks)
	slices.Sort(expected)

	actual := make([]int64, 0, len(vm.Disks))
	for _, disk := range vm.Disks {
		actual = append(actual, disk.Size)
	}

	slices.Sort(actual)

	tolerance := int64(diskGeometry.Tolerance)

	// Exact mode pairs the sizes one to one, so a different number of disks is
	// a mismatch however large the tolerance is.
	if diskGeometry.Mode == models.DiskGeometryExact {
		if len(expected) != len(actual) {
			return false, nil
		}

		for i, size := range expected {
			if max(actual[i]-size, size-actual[i]) > tolerance {
				return false, nil
			}
		}

		return true, nil
	}

	i := 0

	for _, size := range expected {
		for i < len(actual) && actual[i] < size-tolerance {
			i++
		}

		if i == len(actual) || actual[i] > size+tolerance {
			return false, nil
		}

		i++
	}

	return true, nil
}
