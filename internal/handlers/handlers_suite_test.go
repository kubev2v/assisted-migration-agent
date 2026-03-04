package handlers_test

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/services"
)

func TestHandlers(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Handlers Suite")
}

// MockCollectorService is a mock implementation of CollectorService.
type MockCollectorService struct {
	StatusResult   models.CollectorStatus
	StartError     error
	StartCallCount int
	StopCallCount  int
}

func (m *MockCollectorService) GetStatus() models.CollectorStatus {
	return m.StatusResult
}

func (m *MockCollectorService) Start(ctx context.Context, creds *models.Credentials) error {
	m.StartCallCount++
	return m.StartError
}

func (m *MockCollectorService) Stop() {
	m.StopCallCount++
}

// MockInventoryService is a mock implementation of InventoryService.
type MockInventoryService struct {
	InventoryResult *models.Inventory
	InventoryError  error
}

func (m *MockInventoryService) GetInventory(ctx context.Context) (*models.Inventory, error) {
	return m.InventoryResult, m.InventoryError
}

// MockConsoleService is a mock implementation of ConsoleService.
type MockConsoleService struct {
	StatusResult     models.ConsoleStatus
	SetModeError     error
	SetModeCallCount int
	LastModeSet      models.AgentMode
}

func (m *MockConsoleService) Status() models.ConsoleStatus {
	return m.StatusResult
}

func (m *MockConsoleService) SetMode(ctx context.Context, mode models.AgentMode) error {
	m.SetModeCallCount++
	m.LastModeSet = mode
	return m.SetModeError
}

// MockVMService is a mock implementation of VMService.
type MockVMService struct {
	ListResult     []models.VirtualMachineSummary
	ListTotal      int
	ListError      error
	GetResult      *models.VM
	GetError       error
	LastListParams services.VMListParams
}

func (m *MockVMService) List(ctx context.Context, params services.VMListParams) ([]models.VirtualMachineSummary, int, error) {
	m.LastListParams = params
	return m.ListResult, m.ListTotal, m.ListError
}

func (m *MockVMService) Get(ctx context.Context, id string) (*models.VM, error) {
	return m.GetResult, m.GetError
}

// MockInspectorService is a mock implementation of InspectorService.
type MockInspectorService struct {
	StartError                   error
	AddError                     error
	GetStatusResult              models.InspectorStatus
	GetVmStatusResult            models.InspectionStatus
	GetVmStatusError             error
	CancelVmsInspectionError     error
	StopError                    error
	StartCallCount               int
	AddCallCount                 int
	GetStatusCallCount           int
	GetVmStatusCallCount         int
	CancelVmsInspectionCallCount int
	StopCallCount                int
}

func (m *MockInspectorService) Start(ctx context.Context, vmIDs []string, cred *models.Credentials) error {
	m.StartCallCount++
	return m.StartError
}

func (m *MockInspectorService) Add(ctx context.Context, vmIDs []string) error {
	m.AddCallCount++
	return m.AddError
}

func (m *MockInspectorService) GetStatus() models.InspectorStatus {
	m.GetStatusCallCount++
	return m.GetStatusResult
}

func (m *MockInspectorService) GetVmStatus(ctx context.Context, id string) (models.InspectionStatus, error) {
	m.GetVmStatusCallCount++
	return m.GetVmStatusResult, m.GetVmStatusError
}

func (m *MockInspectorService) CancelVmsInspection(ctx context.Context, vmIDs ...string) error {
	m.CancelVmsInspectionCallCount++
	return m.CancelVmsInspectionError
}

func (m *MockInspectorService) Stop(ctx context.Context) error {
	m.StopCallCount++
	return m.StopError
}

// MockGroupService is a mock implementation of GroupService.
type MockGroupService struct {
	ListResult        []models.Group
	ListError         error
	GetResult         *models.Group
	GetError          error
	ListVMsResult     []models.VirtualMachineSummary
	ListVMsTotal      int
	ListVMsError      error
	CreateResult      *models.Group
	CreateError       error
	UpdateResult      *models.Group
	UpdateError       error
	DeleteError       error
	LastListVMsParams services.GroupGetParams
	LastCreateGroup   models.Group
	LastUpdateGroup   models.Group
	LastUpdateID      int
	LastDeleteID      int
}

func (m *MockGroupService) List(ctx context.Context) ([]models.Group, error) {
	return m.ListResult, m.ListError
}

func (m *MockGroupService) Get(ctx context.Context, id int) (*models.Group, error) {
	return m.GetResult, m.GetError
}

func (m *MockGroupService) ListVirtualMachines(ctx context.Context, id int, params services.GroupGetParams) ([]models.VirtualMachineSummary, int, error) {
	m.LastListVMsParams = params
	return m.ListVMsResult, m.ListVMsTotal, m.ListVMsError
}

func (m *MockGroupService) Create(ctx context.Context, group models.Group) (*models.Group, error) {
	m.LastCreateGroup = group
	return m.CreateResult, m.CreateError
}

func (m *MockGroupService) Update(ctx context.Context, id int, group models.Group) (*models.Group, error) {
	m.LastUpdateID = id
	m.LastUpdateGroup = group
	return m.UpdateResult, m.UpdateError
}

func (m *MockGroupService) Delete(ctx context.Context, id int) error {
	m.LastDeleteID = id
	return m.DeleteError
}
