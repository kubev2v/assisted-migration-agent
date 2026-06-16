package v1_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"testing"
	"time"

	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	handlers "github.com/kubev2v/assisted-migration-agent/internal/handlers/v1"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/services"
)

// generateCACertPEM returns a PEM-encoded self-signed CA certificate for use in handler tests.
func generateCACertPEM() string {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test-ca"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IsCA:         true,
	}
	der, _ := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func TestHandlers(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Handlers Suite")
}

var _ = BeforeSuite(func() {
	if v, ok := binding.Validator.Engine().(*validator.Validate); ok {
		handlers.RegisterValidators(v)
	}
})

// MockCollectorService is a mock implementation of CollectorService.
type MockCollectorService struct {
	StatusResult   models.CollectorStatus
	StartError     error
	StartCallCount int
	StopCallCount  int
	LastCreds      models.Credentials
}

func (m *MockCollectorService) GetStatus() models.CollectorStatus {
	return m.StatusResult
}

func (m *MockCollectorService) Start(ctx context.Context, creds models.Credentials) error {
	m.StartCallCount++
	m.LastCreds = creds
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
	ListResult                     []models.VirtualMachineSummary
	ListTotal                      int
	ListError                      error
	GetResult                      *models.VM
	GetError                       error
	UpdateMigrationExcludedError   error
	LastListParams                 services.VMListParams
	LastUpdateMigrationExcludedID  string
	UpdateMigrationExcludedValue   bool
	UpdateLabelsError              error
	LastUpdateLabelsID             string
	LastUpdateLabelsValue          []string
	GetAllLabelsResult             []string
	GetAllLabelsCountsResult       []int
	GetAllLabelsError              error
	RemoveLabelFromAllVMsResult    int
	RemoveLabelFromAllVMsError     error
	LastRemoveLabelFromAllVMsLabel string
	UpdateLabelVMsError            error
	LastUpdateLabelVMsAdd          []string
	LastUpdateLabelVMsRem          []string
	LastUpdateLabelVMsLabel        string
}

func (m *MockVMService) List(ctx context.Context, params services.VMListParams) ([]models.VirtualMachineSummary, int, error) {
	m.LastListParams = params
	return m.ListResult, m.ListTotal, m.ListError
}

func (m *MockVMService) Get(ctx context.Context, id string) (*models.VM, error) {
	return m.GetResult, m.GetError
}

func (m *MockVMService) GetFilterOptions(ctx context.Context) (models.VMFilterOptions, error) {
	return models.VMFilterOptions{}, nil
}

func (m *MockVMService) UpdateMigrationExcluded(ctx context.Context, id string, excluded bool) error {
	m.LastUpdateMigrationExcludedID = id
	m.UpdateMigrationExcludedValue = excluded
	return m.UpdateMigrationExcludedError
}

func (m *MockVMService) UpdateLabels(ctx context.Context, id string, labels []string) error {
	m.LastUpdateLabelsID = id
	m.LastUpdateLabelsValue = labels
	return m.UpdateLabelsError
}

func (m *MockVMService) GetAllLabels(ctx context.Context) ([]string, []int, error) {
	return m.GetAllLabelsResult, m.GetAllLabelsCountsResult, m.GetAllLabelsError
}

func (m *MockVMService) RemoveLabelFromAllVMs(ctx context.Context, label string) (int, error) {
	m.LastRemoveLabelFromAllVMsLabel = label
	return m.RemoveLabelFromAllVMsResult, m.RemoveLabelFromAllVMsError
}

func (m *MockVMService) UpdateLabelVMs(ctx context.Context, addVMIDs, removeVMIDs []string, label string) error {
	m.LastUpdateLabelVMsAdd = addVMIDs
	m.LastUpdateLabelVMsRem = removeVMIDs
	m.LastUpdateLabelVMsLabel = label
	return m.UpdateLabelVMsError
}

// MockInspectorService is a mock implementation of InspectorService.
type MockInspectorService struct {
	StartError                   error
	CredentialsError             error
	GetStatusResult              models.InspectorStatus
	CancelError                  error
	StopError                    error
	StartCallCount               int
	GetStatusCallCount           int
	CancelVmsInspectionCallCount int
	StopCallCount                int
	IsBusyResult                 bool
	LastCreds                    models.Credentials
}

func (m *MockInspectorService) IsBusy() bool {
	return m.IsBusyResult
}

func (m *MockInspectorService) Start(ctx context.Context, creds models.Credentials, vmIDs []string) error {
	m.StartCallCount++
	m.LastCreds = creds
	return m.StartError
}

func (m *MockInspectorService) Credentials(ctx context.Context, creds models.Credentials) error {
	_, _ = ctx, creds
	m.LastCreds = creds
	return m.CredentialsError
}

func (m *MockInspectorService) GetStatus() models.InspectorStatus {
	m.GetStatusCallCount++
	return m.GetStatusResult
}

func (m *MockInspectorService) Cancel(id string) error {
	m.CancelVmsInspectionCallCount++
	return m.CancelError
}

func (m *MockInspectorService) Stop() error {
	m.StopCallCount++
	return m.StopError
}

// MockVddkService is a mock implementation of VddkService.
type MockVddkService struct {
	UploadResult *models.VddkStatus
	UploadError  error
	StatusResult *models.VddkStatus
	StatusError  error
	UploadCount  int
	StatusCount  int
}

func (m *MockVddkService) Upload(ctx context.Context, filename string, r io.Reader) (*models.VddkStatus, error) {
	m.UploadCount++
	return m.UploadResult, m.UploadError
}

func (m *MockVddkService) Status(ctx context.Context) (*models.VddkStatus, error) {
	m.StatusCount++
	return m.StatusResult, m.StatusError
}

// MockGroupService is a mock implementation of GroupService.
type MockGroupService struct {
	ListResult        []models.Group
	ListTotal         int
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
	LastListParams    services.GroupListParams
	LastListVMsParams services.GroupGetParams
	LastCreateGroup   models.Group
	LastUpdateGroup   models.Group
	LastUpdateID      uuid.UUID
	LastDeleteID      uuid.UUID
}

func (m *MockGroupService) List(ctx context.Context, params services.GroupListParams) ([]models.Group, int, error) {
	m.LastListParams = params
	return m.ListResult, m.ListTotal, m.ListError
}

func (m *MockGroupService) Get(ctx context.Context, id uuid.UUID) (*models.Group, error) {
	return m.GetResult, m.GetError
}

func (m *MockGroupService) ListVirtualMachines(ctx context.Context, id uuid.UUID, params services.GroupGetParams) ([]models.VirtualMachineSummary, int, error) {
	m.LastListVMsParams = params
	return m.ListVMsResult, m.ListVMsTotal, m.ListVMsError
}

func (m *MockGroupService) Create(ctx context.Context, group models.Group) (*models.Group, error) {
	m.LastCreateGroup = group
	return m.CreateResult, m.CreateError
}

func (m *MockGroupService) Update(ctx context.Context, id uuid.UUID, group models.Group) (*models.Group, error) {
	m.LastUpdateID = id
	m.LastUpdateGroup = group
	return m.UpdateResult, m.UpdateError
}

func (m *MockGroupService) Delete(ctx context.Context, id uuid.UUID) error {
	m.LastDeleteID = id
	return m.DeleteError
}

// MockRightsizingService is a mock implementation of RightsizingService.
type MockRightsizingService struct {
	TriggerResult     *models.RightsizingReportSummary
	TriggerError      error
	TriggerCallCount  int
	LastTriggerParams models.RightsizingParams

	ListResult []models.RightsizingReportSummary
	ListError  error

	GetResult *models.RightsizingReport
	GetError  error
	LastGetID string

	GetUtilizationResult *models.VmUtilizationDetails
	GetUtilizationError  error
	LastUtilizationVMID  string

	ClusterUtilizationResult         []models.RightsizingClusterUtilization
	ClusterUtilizationError          error
	LatestClusterUtilizationReportID string
	LatestClusterUtilizationResult   []models.RightsizingClusterUtilization
	LatestClusterUtilizationError    error
	LastLatestClusterFilterExpr      string
}

func (m *MockRightsizingService) TriggerCollection(ctx context.Context, params models.RightsizingParams) (*models.RightsizingReportSummary, error) {
	m.TriggerCallCount++
	m.LastTriggerParams = params
	return m.TriggerResult, m.TriggerError
}

func (m *MockRightsizingService) ListReports(ctx context.Context) ([]models.RightsizingReportSummary, error) {
	return m.ListResult, m.ListError
}

func (m *MockRightsizingService) GetReport(ctx context.Context, id string) (*models.RightsizingReport, error) {
	m.LastGetID = id
	return m.GetResult, m.GetError
}

func (m *MockRightsizingService) GetVMUtilization(ctx context.Context, vmID string) (*models.VmUtilizationDetails, error) {
	m.LastUtilizationVMID = vmID
	return m.GetUtilizationResult, m.GetUtilizationError
}

func (m *MockRightsizingService) ListClusterUtilization(ctx context.Context, reportID, filterExpr string) ([]models.RightsizingClusterUtilization, error) {
	return m.ClusterUtilizationResult, m.ClusterUtilizationError
}

func (m *MockRightsizingService) ListLatestClusterUtilization(ctx context.Context, filterExpr string) (string, []models.RightsizingClusterUtilization, error) {
	m.LastLatestClusterFilterExpr = filterExpr
	return m.LatestClusterUtilizationReportID, m.LatestClusterUtilizationResult, m.LatestClusterUtilizationError
}

type MockApplicationService struct {
	ListResult []models.ApplicationOverview
	ListError  error
}

func (m *MockApplicationService) List(ctx context.Context) ([]models.ApplicationOverview, error) {
	return m.ListResult, m.ListError
}
