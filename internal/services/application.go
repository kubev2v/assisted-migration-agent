package services

import (
	"context"
	_ "embed"
	"fmt"
	"sort"
	"strings"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"

	"github.com/kubev2v/assisted-migration-agent/internal/filter"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

//go:embed applications.yaml
var applicationsYAML []byte

type applicationDef struct {
	Name                      string             `yaml:"name"`
	Desc                      string             `yaml:"desc"`
	Vendor                    string             `yaml:"vendor"`
	Category                  string             `yaml:"category"`
	Certified                 string             `yaml:"certified"`
	CertificationComment      string             `yaml:"certification_comment"`
	ContainerizationCandidate bool               `yaml:"containerization_candidate"`
	Match                     string             `yaml:"match"`
	Confidence                *int               `yaml:"confidence"`
	Processes                 processSignature   `yaml:"processes"`
	Products                  []productSignature `yaml:"products"`
}

type processSignature struct {
	Names      []string `yaml:"names"`
	MinMatched int      `yaml:"min_matched"`
}

type productSignature struct {
	Names      []string `yaml:"name_contains"`
	ExactNames []string `yaml:"name_equals"`
	Vendors    []string `yaml:"vendor_contains"`
}

type ApplicationService struct {
	store *store.Store
	defs  []applicationDef
}

func NewApplicationService(st *store.Store) (*ApplicationService, error) {
	var defs []applicationDef
	if err := yaml.Unmarshal(applicationsYAML, &defs); err != nil {
		return nil, err
	}
	for _, def := range defs {
		if strings.TrimSpace(def.Vendor) == "" {
			return nil, fmt.Errorf("missing vendor for application %q", def.Name)
		}
		if strings.TrimSpace(def.Category) == "" {
			return nil, fmt.Errorf("missing category for application %q", def.Name)
		}
		certified := strings.ToLower(strings.TrimSpace(def.Certified))
		if certified != "yes" && certified != "no" && certified != "maybe" {
			return nil, fmt.Errorf("invalid certification status %q for application %q: expected yes, no, or maybe", def.Certified, def.Name)
		}
		if strings.TrimSpace(def.CertificationComment) == "" {
			return nil, fmt.Errorf("missing certification comment for application %q", def.Name)
		}
		match := strings.ToLower(strings.TrimSpace(def.Match))
		if match != "" && match != "any" && match != "all" {
			return nil, fmt.Errorf("invalid match mode %q for application %q: expected any or all", def.Match, def.Name)
		}
		if def.Confidence != nil && (*def.Confidence < 0 || *def.Confidence > 100) {
			return nil, fmt.Errorf("invalid confidence %d for application %q: expected 0-100", *def.Confidence, def.Name)
		}
	}
	return &ApplicationService{store: st, defs: defs}, nil
}

func (s *ApplicationService) List(ctx context.Context, byExpression string) ([]models.ApplicationOverview, error) {
	var overviews []models.ApplicationOverview
	var err error
	if byExpression == "" {
		overviews, err = s.store.Application().ListOverviews(ctx, nil)
	} else {
		f, parseErr := filter.ParseWithApplicationMap([]byte(byExpression))
		if parseErr != nil {
			return nil, errors.NewInvalidFilterError(byExpression, parseErr.Error())
		}
		overviews, err = s.store.Application().ListOverviews(ctx, f)
	}
	if err != nil {
		return nil, err
	}

	definitionsByName := make(map[string]applicationDef, len(s.defs))
	for _, def := range s.defs {
		definitionsByName[def.Name] = def
	}
	for i := range overviews {
		if def, ok := definitionsByName[overviews[i].Name]; ok {
			overviews[i].ContainerizationCandidate = def.ContainerizationCandidate
			overviews[i].Confidence = confidenceForApplication(def)
		}
	}
	return overviews, nil
}

// BuildCollectorWorkUnits returns a postCollectionBuilderFn that precomputes
// application matches and persists them to the database after each collection.
func (s *ApplicationService) MatchApplications(ctx context.Context) error {
	zap.S().Named("application_service").Info("detecting applications from guest apps and vApp products")

	guestApps, err := s.store.VM().GetGuestApps(ctx)
	if err != nil {
		return fmt.Errorf("fetching guest apps for application detection: %w", err)
	}

	overviews := matchApplications(s.defs, guestApps)

	var records []models.ApplicationVMRecord
	for _, app := range overviews {
		for _, vm := range app.VMs {
			records = append(records, models.ApplicationVMRecord{
				AppName:     app.Name,
				AppDesc:     app.Description,
				AppVendor:   app.Vendor,
				AppCategory: app.Category,
				Confidence:  app.Confidence,
				VMID:        vm.ID,
				VMName:      vm.Name,
			})
		}
	}

	if err := s.store.WithTx(ctx, func(txCtx context.Context) error {
		return s.store.Application().ReplaceAll(txCtx, records)
	}); err != nil {
		return fmt.Errorf("persisting application matches: %w", err)
	}

	zap.S().Named("application_service").Infof("detected %d applications across %d VMs", len(overviews), len(records))
	return nil
}

// matchApplications matches application definitions against VM guest apps and
// vApp products, excludes unmatched definitions, and returns results sorted by name.
func matchApplications(defs []applicationDef, guestApps []models.VMGuestApps) []models.ApplicationOverview {
	results := make([]models.ApplicationOverview, 0, len(defs))
	for _, def := range defs {
		minMatched := def.Processes.MinMatched
		if minMatched < 1 {
			minMatched = 1
		}
		processConfigured := len(def.Processes.Names) > 0
		productConfigured := hasProductSignatures(def.Products)
		match := strings.ToLower(strings.TrimSpace(def.Match))
		if match == "" {
			match = "any"
			if processConfigured && productConfigured {
				match = "all"
			}
		}

		var vms []models.ApplicationVM
		for _, vm := range guestApps {
			processMatch := processConfigured && countMatchedProcesses(vm.AppNames, def.Processes.Names) >= minMatched
			productMatch := matchesProducts(vm.Products, def.Products)
			matched := processMatch || productMatch
			if match == "all" {
				matched = (processConfigured || productConfigured) &&
					(!processConfigured || processMatch) &&
					(!productConfigured || productMatch)
			}
			if matched {
				vms = append(vms, models.ApplicationVM{ID: vm.ID, Name: vm.Name})
			}
		}
		if len(vms) == 0 {
			continue
		}
		confidence := confidenceForApplication(def)
		results = append(results, models.ApplicationOverview{
			Name:        def.Name,
			Description: def.Desc,
			Vendor:      def.Vendor,
			Category:    def.Category,
			Confidence:  confidence,
			VMCount:     len(vms),
			VMs:         vms,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Name < results[j].Name
	})

	return results
}

func hasProductSignatures(signatures []productSignature) bool {
	for _, signature := range signatures {
		if len(signature.Names) > 0 || len(signature.ExactNames) > 0 || len(signature.Vendors) > 0 {
			return true
		}
	}
	return false
}

func confidenceForApplication(def applicationDef) *int {
	if def.Confidence != nil {
		return def.Confidence
	}

	defaultConfidence := 50
	if len(def.Processes.Names) > 0 && !hasProductSignatures(def.Products) {
		defaultConfidence = 75
	}
	return &defaultConfidence
}

// matchesProducts returns true when any configured signature matches one
// product record. All configured fields in a signature must match the same
// product; multiple values within a field are alternatives.
func matchesProducts(products []models.VAppProduct, signatures []productSignature) bool {
	for _, signature := range signatures {
		if len(signature.Names) == 0 && len(signature.ExactNames) == 0 && len(signature.Vendors) == 0 {
			continue
		}
		nameConfigured := len(signature.Names) > 0 || len(signature.ExactNames) > 0

		for _, product := range products {
			nameMatched := containsAnyFold(product.Name, signature.Names) || equalsAnyFold(product.Name, signature.ExactNames)
			if nameConfigured && !nameMatched {
				continue
			}
			if len(signature.Vendors) > 0 && !containsAnyFold(product.Vendor, signature.Vendors) {
				continue
			}
			return true
		}
	}
	return false
}

func containsAnyFold(value string, candidates []string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, candidate := range candidates {
		candidate = strings.ToLower(strings.TrimSpace(candidate))
		if candidate != "" && strings.Contains(value, candidate) {
			return true
		}
	}
	return false
}

func equalsAnyFold(value string, candidates []string) bool {
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate != "" && strings.EqualFold(strings.TrimSpace(value), candidate) {
			return true
		}
	}
	return false
}

// countMatchedProcesses counts how many distinct process prefixes are matched
// by the VM's guest app names. Each prefix is counted at most once.
func countMatchedProcesses(appNames []string, processPrefixes []string) int {
	count := 0
	for _, prefix := range processPrefixes {
		for _, name := range appNames {
			if strings.HasPrefix(name, prefix) {
				count++
				break
			}
		}
	}
	return count
}
