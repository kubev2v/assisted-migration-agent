package v2_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"

	"github.com/kubev2v/migration-planner/pkg/inventory"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	svc "github.com/kubev2v/assisted-migration-agent/internal/services"
)

func TestHandlersV2(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "V2 Handlers Suite")
}

// errServiceNotProvided is returned by the stub for services a spec did not set,
// so a missing stub fails the assertion instead of dereferencing nil.
var errServiceNotProvided = errors.New("the test did not provide this service")

// insertVMForHandler seeds one row of vinfo for the collection database.
func insertVMForHandler(ctx context.Context, db *sql.DB, vmID, vmName string, apps []string) error {
	appsJSON := "[]"
	if len(apps) > 0 {
		entries := make([]map[string]string, len(apps))
		for i, n := range apps {
			entries[i] = map[string]string{"name": n}
		}
		b, err := json.Marshal(entries)
		if err != nil {
			return err
		}
		appsJSON = string(b)
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO vinfo ("VM ID", "VM", "Powerstate", "Connection state", "Cluster", "Datacenter",
			"Host", "Folder ID", "Firmware", "SMBIOS UUID", "Memory", "CPUs",
			"OS according to the configuration file", "DNS Name", "Primary IP Address",
			"In Use MiB", "Template", "FT State", "guest_apps")
		VALUES (?, ?, 'poweredOn', 'connected', 'cluster-1', 'dc-1',
			'host-1', 'folder-1', 'bios', 'uuid-1', 4096, 2,
			'CentOS 7', '', '', 0, false, 'notConfigured', ?)
	`, vmID, vmName, appsJSON)
	return err
}

// mockInventoryBuilder satisfies svc.InventoryBuilder for test GroupService instances.
type mockInventoryBuilder struct{}

func (m *mockInventoryBuilder) BuildInventory(_ context.Context, vmIDs []string) (*inventory.Inventory, error) {
	if len(vmIDs) == 0 {
		return nil, nil
	}
	return &inventory.Inventory{VCenterID: "test-vcenter", VCenterVersion: "7.0.0"}, nil
}

// stubServiceProvider implements handlers.ServiceProvider.
// All methods return nil/no-error except GroupService and LatestGroupService,
// which return the pre-built groupSvc field.
type stubServiceProvider struct {
	groupSvc *svc.GroupService
}

func (s *stubServiceProvider) ConsoleService() *svc.Console                     { return nil }
func (s *stubServiceProvider) CollectionService() *svc.CollectionService        { return nil }
func (s *stubServiceProvider) InspectorService() (*svc.InspectorService, error) { return nil, nil }
func (s *stubServiceProvider) VddkService() *svc.VddkService                    { return nil }
func (s *stubServiceProvider) CredentialsService() *svc.CredentialsService      { return nil }
func (s *stubServiceProvider) ForecasterService() *svc.ForecasterService        { return nil }
func (s *stubServiceProvider) ApplicationService(_ string) (*svc.ApplicationService, error) {
	return nil, nil
}
func (s *stubServiceProvider) ComparisonService(aId, bId string) (*svc.ComparisonService, error) {
	return nil, nil
}
func (s *stubServiceProvider) ExportService(_ string) (*svc.ExportService, error) { return nil, nil }
func (s *stubServiceProvider) VirtualMachineService(_ string) (*svc.VMService, error) {
	return nil, nil
}
func (s *stubServiceProvider) GroupService(_ string) (*svc.GroupService, error) {
	return s.groupSvc, nil
}
func (s *stubServiceProvider) InventoryService(_ string) (*svc.InventoryService, error) {
	return nil, nil
}
func (s *stubServiceProvider) RightsizingService(_ string) (*svc.RightsizingService, error) {
	return nil, nil
}
func (s *stubServiceProvider) LatestVirtualMachineService() (*svc.VMService, error) { return nil, nil }
func (s *stubServiceProvider) LatestGroupService() (*svc.GroupService, error)       { return s.groupSvc, nil }
func (s *stubServiceProvider) LatestInventoryService() (*svc.InventoryService, error) {
	return nil, nil
}
func (s *stubServiceProvider) LatestRightsizingService() (*svc.RightsizingService, error) {
	return nil, nil
}
func (s *stubServiceProvider) LatestApplicationService() (*svc.ApplicationService, error) {
	return nil, nil
}
func (s *stubServiceProvider) ApplianceService(_ string) (*svc.ApplianceService, error) {
	return nil, errServiceNotProvided
}
func (s *stubServiceProvider) ApplianceRulesService() (*svc.ApplianceRulesService, error) {
	return nil, errServiceNotProvided
}
func (s *stubServiceProvider) GetCollectorStatus() models.CollectorStatus {
	return models.CollectorStatus{State: models.CollectorStateReady}
}
func (s *stubServiceProvider) StartCollecting(_ context.Context) (models.CollectorStatus, error) {
	return models.CollectorStatus{State: models.CollectorStateReady}, nil
}
func (s *stubServiceProvider) DeleteData(_ context.Context) error { return nil }
func (s *stubServiceProvider) Stop(_ context.Context)             {}
func (s *stubServiceProvider) StopCollecting() error              { return nil }
