package v2

import (
	"context"
	"errors"
	"sync"

	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"
	"github.com/kubev2v/migration-planner/pkg/opa"

	"github.com/kubev2v/assisted-migration-agent/internal/config"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/pkg/console"
	"github.com/kubev2v/assisted-migration-agent/pkg/crypto"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

type ServiceManager struct {
	cfg           *config.Configuration
	consoleClient *console.Client
	keyMgr        *crypto.KeyManager
	pool          *store.Pool

	console     *Console
	collection  *CollectionService
	credentials *CredentialsService
	mu          sync.Mutex
	inspector   *InspectorService
	vddk        *VddkService
	forecaster  *ForecasterService
	validator   *opa.Validator
	collector   *CollectorService
	workBuilder CollectorWorkBuilder
}

type ServiceManagerOption func(*ServiceManager)

func WithConfig(cfg *config.Configuration) ServiceManagerOption {
	return func(m *ServiceManager) {
		m.cfg = cfg
	}
}

func WithPool(pool *store.Pool) ServiceManagerOption {
	return func(m *ServiceManager) {
		m.pool = pool
	}
}

func WithConsoleClient(c *console.Client) ServiceManagerOption {
	return func(m *ServiceManager) {
		m.consoleClient = c
	}
}

func WithKeyManager(km *crypto.KeyManager) ServiceManagerOption {
	return func(m *ServiceManager) {
		m.keyMgr = km
	}
}

func WithOpaValidatior(v *opa.Validator) ServiceManagerOption {
	return func(m *ServiceManager) {
		m.validator = v
	}
}

func WithCollectorWorkBuilder(fn CollectorWorkBuilder) ServiceManagerOption {
	return func(m *ServiceManager) {
		m.workBuilder = fn
	}
}

func NewServiceManager(opts ...ServiceManagerOption) *ServiceManager {
	m := &ServiceManager{}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

func (m *ServiceManager) Initialize() error {
	if m.cfg == nil {
		return errors.New("config is required")
	}
	if m.pool == nil {
		return errors.New("pool is required")
	}
	if m.keyMgr == nil {
		return errors.New("key manager is required")
	}

	mainDB, err := m.pool.Get(store.MainDatabaseID)
	if err != nil {
		return err
	}
	mainStore, err := mainDB.Store()
	if err != nil {
		return err
	}

	m.console, err = NewConsoleService(
		m.cfg.Agent,
		m.consoleClient,
		m,
		mainStore,
	)
	if err != nil {
		return err
	}

	m.collection = NewCollectionService(m.pool)

	m.credentials = NewCredentialsService(mainStore)
	m.credentials.WithKeyManager(m.keyMgr)

	m.vddk = NewVddkService(m.cfg.Agent.DataFolder, m.pool)

	m.forecaster = NewForecasterService(m.pool, m.credentials)

	return nil
}

func (m *ServiceManager) GetCollectorStatus() models.CollectorStatus {
	m.mu.Lock()
	collector := m.collector
	m.mu.Unlock()

	if collector != nil {
		return collector.GetStatus()
	}

	invSvc, err := m.LatestInventoryService()
	if err == nil {
		inv, err := invSvc.GetInventory(context.Background())
		if err == nil && inv != nil {
			return models.CollectorStatus{State: models.CollectorStateCollected}
		}
	}

	return models.CollectorStatus{State: models.CollectorStateReady}
}

func (m *ServiceManager) StartCollecting(ctx context.Context) (models.CollectorStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.inspector != nil && m.inspector.IsBusy() {
		return models.CollectorStatus{}, srvErrors.NewInspectionInProgressError()
	}

	if m.collector != nil && m.collector.GetStatus().State.IsRunning() {
		return models.CollectorStatus{}, srvErrors.NewCollectionInProgressError()
	}

	if m.workBuilder == nil {
		factory, err := newVCenterCollectorWorkFactory(m.credentials, m.pool, m.cfg.Agent.DataFolder, m.validator)
		if err != nil {
			return models.CollectorStatus{}, err
		}
		m.workBuilder = factory
	}

	m.collector = NewCollectorService(m.workBuilder)

	if err := m.collector.Start(ctx); err != nil {
		m.collector = nil
		return models.CollectorStatus{}, err
	}

	return m.collector.GetStatus(), nil
}

func (m *ServiceManager) StopCollecting() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.collector == nil {
		return nil
	}
	m.collector.Stop()
	m.collector = nil
	return nil
}

func (m *ServiceManager) StartRVToolsCollecting(rvtoolFiles []string) (models.CollectorStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.inspector != nil && m.inspector.IsBusy() {
		return models.CollectorStatus{}, srvErrors.NewInspectionInProgressError()
	}

	if m.collector != nil && m.collector.GetStatus().State.IsRunning() {
		return models.CollectorStatus{}, srvErrors.NewCollectionInProgressError()
	}

	factory, err := newRvtoolWorkFactory(m.pool, rvtoolFiles, m.cfg.Agent.DataFolder, m.validator)
	if err != nil {
		return models.CollectorStatus{}, err
	}

	m.collector = NewCollectorService(factory)

	if err := m.collector.Start(context.Background()); err != nil {
		m.collector = nil
		return models.CollectorStatus{}, err
	}

	return m.collector.GetStatus(), nil
}

// InspectorService must use the latest collection when returning the inspector
// Therefore, this methods return the same inspector as long is busy.
// When the inspector is done, to be sure we use the latest collection
// the methods recreates a new one.
func (m *ServiceManager) InspectorService() (*InspectorService, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.inspector != nil && m.inspector.IsBusy() {
		return m.inspector, nil
	}

	if m.collector != nil && m.collector.GetStatus().State.IsRunning() {
		return nil, srvErrors.NewCollectionInProgressError()
	}

	m.inspector = nil

	db, err := m.pool.Latest()
	if err != nil {
		return nil, err
	}
	store, err := db.Store()
	if err != nil {
		return nil, err
	}

	m.inspector = NewInspectorService(store, 10, m.cfg.Agent.DataFolder, m.credentials)

	return m.inspector, nil
}

func (m *ServiceManager) VddkService() *VddkService {
	return m.vddk
}

func (m *ServiceManager) ForecasterService() *ForecasterService {
	return m.forecaster
}

func (m *ServiceManager) CollectionService() *CollectionService {
	return m.collection
}

func (m *ServiceManager) CredentialsService() *CredentialsService {
	return m.credentials
}

func (m *ServiceManager) ConsoleService() *Console {
	return m.console
}

func (m *ServiceManager) InventoryService(collectionID string) (*InventoryService, error) {
	db, err := m.pool.Get(collectionID)
	if err != nil {
		return nil, err
	}
	return m.inventoryService(db)
}

func (m *ServiceManager) VirtualMachineService(collectionID string) (*VMService, error) {
	db, err := m.pool.Get(collectionID)
	if err != nil {
		return nil, err
	}
	return m.vmService(db)
}

func (m *ServiceManager) GroupService(collectionID string) (*GroupService, error) {
	db, err := m.pool.Get(collectionID)
	if err != nil {
		return nil, err
	}
	return m.groupService(db)
}

func (m *ServiceManager) ApplicationService(collectionID string) (*ApplicationService, error) {
	db, err := m.pool.Get(collectionID)
	if err != nil {
		return nil, err
	}
	st, err := db.Store()
	if err != nil {
		return nil, err
	}
	return NewApplicationService(st)
}

func (m *ServiceManager) RightsizingService(collectionID string) (*RightsizingService, error) {
	db, err := m.pool.Get(collectionID)
	if err != nil {
		return nil, err
	}
	return m.rightsizingService(db)
}

func (m *ServiceManager) ExportService(collectionID string) (*ExportService, error) {
	db, err := m.pool.Get(collectionID)
	if err != nil {
		return nil, err
	}
	return m.exportService(db)
}

func (m *ServiceManager) LatestVirtualMachineService() (*VMService, error) {
	db, err := m.pool.Latest()
	if err != nil {
		return nil, err
	}
	return m.vmService(db)
}

func (m *ServiceManager) LatestGroupService() (*GroupService, error) {
	db, err := m.pool.Latest()
	if err != nil {
		return nil, err
	}
	return m.groupService(db)
}

func (m *ServiceManager) LatestEventService() (*EventService, error) {
	db, err := m.pool.Latest()
	if err != nil {
		return nil, err
	}
	st, err := db.Store()
	if err != nil {
		return nil, err
	}
	return NewEventService(st), nil
}

func (m *ServiceManager) LatestInventoryService() (*InventoryService, error) {
	db, err := m.pool.Latest()
	if err != nil {
		return nil, err
	}
	return m.inventoryService(db)
}

func (m *ServiceManager) LatestRightsizingService() (*RightsizingService, error) {
	db, err := m.pool.Latest()
	if err != nil {
		return nil, err
	}
	return m.rightsizingService(db)
}

func (m *ServiceManager) Stop(ctx context.Context) {
	m.mu.Lock()
	inspector := m.inspector
	m.mu.Unlock()
	if inspector != nil {
		_ = inspector.Stop()
	}

	m.mu.Lock()
	c := m.collector
	m.collector = nil
	m.mu.Unlock()
	if c != nil {
		c.Stop()
	}

	if m.forecaster != nil {
		_ = m.forecaster.Stop()
	}
}

func (m *ServiceManager) vmService(db *store.Database) (*VMService, error) {
	st, err := db.Store()
	if err != nil {
		return nil, err
	}
	return NewVMService(st), nil
}

func (m *ServiceManager) groupService(db *store.Database) (*GroupService, error) {
	st, err := db.Store()
	if err != nil {
		return nil, err
	}
	return NewGroupService(st, duckdb_parser.New(st.Querier(), nil)), nil
}

func (m *ServiceManager) inventoryService(db *store.Database) (*InventoryService, error) {
	st, err := db.Store()
	if err != nil {
		return nil, err
	}
	return NewInventoryService(st), nil
}

func (m *ServiceManager) rightsizingService(db *store.Database) (*RightsizingService, error) {
	st, err := db.Store()
	if err != nil {
		return nil, err
	}
	return NewRightsizingService(st), nil
}

func (m *ServiceManager) exportService(db *store.Database) (*ExportService, error) {
	st, err := db.Store()
	if err != nil {
		return nil, err
	}
	return NewExportService(st), nil
}

func (m *ServiceManager) ComparisonService(aId, bId string) (*ComparisonService, error) {
	dbA, err := m.pool.Get(aId)
	if err != nil {
		return nil, err
	}
	dbB, err := m.pool.Get(bId)
	if err != nil {
		return nil, err
	}
	stA, err := dbA.Store()
	if err != nil {
		return nil, err
	}
	stB, err := dbB.Store()
	if err != nil {
		return nil, err
	}
	return NewComparisonService(stA, stB,
		models.CollectionMeta{ID: dbA.ID, CreatedAt: dbA.CreatedAt},
		models.CollectionMeta{ID: dbB.ID, CreatedAt: dbB.CreatedAt},
	), nil
}
