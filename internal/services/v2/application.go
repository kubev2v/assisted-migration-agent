package v2

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"go.uber.org/zap"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
)

//go:embed applications.json
var applicationsJSON []byte

type applicationDef struct {
	Name       string   `json:"name"`
	Desc       string   `json:"desc"`
	Processes  []string `json:"processes"`
	MinMatched int      `json:"min_matched"`
}

type ApplicationService struct {
	store *store.Store2
	defs  []applicationDef
}

func NewApplicationService(st *store.Store2) (*ApplicationService, error) {
	var defs []applicationDef
	if err := json.Unmarshal(applicationsJSON, &defs); err != nil {
		return nil, err
	}
	return &ApplicationService{store: st, defs: defs}, nil
}

func (s *ApplicationService) List(ctx context.Context) ([]models.ApplicationOverview, error) {
	return s.store.Application().ListOverviews(ctx)
}

// BuildCollectorWorkUnits returns a postCollectionBuilderFn that precomputes
// application matches and persists them to the database after each collection.
func (s *ApplicationService) MatchApplications(ctx context.Context) error {
	zap.S().Named("application_service").Info("detecting applications from guest apps")

	guestApps, err := s.store.VM().GetGuestApps(ctx)
	if err != nil {
		return fmt.Errorf("fetching guest apps for application detection: %w", err)
	}

	overviews := matchApplications(s.defs, guestApps)

	var records []models.ApplicationVMRecord
	for _, app := range overviews {
		for _, vm := range app.VMs {
			records = append(records, models.ApplicationVMRecord{
				AppName: app.Name,
				AppDesc: app.Description,
				VMID:    vm.ID,
				VMName:  vm.Name,
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

// matchApplications matches application definitions against VM guest apps,
// excludes applications with no matching VMs, and returns results sorted by name.
func matchApplications(defs []applicationDef, guestApps []models.VMGuestApps) []models.ApplicationOverview {
	results := make([]models.ApplicationOverview, 0, len(defs))
	for _, def := range defs {
		minMatched := def.MinMatched
		if minMatched < 1 {
			minMatched = 1
		}

		var vms []models.ApplicationVM
		for _, vm := range guestApps {
			if countMatchedProcesses(vm.AppNames, def.Processes) >= minMatched {
				vms = append(vms, models.ApplicationVM{ID: vm.ID, Name: vm.Name})
			}
		}
		if len(vms) == 0 {
			continue
		}
		results = append(results, models.ApplicationOverview{
			Name:        def.Name,
			Description: def.Desc,
			VMCount:     len(vms),
			VMs:         vms,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Name < results[j].Name
	})

	return results
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
