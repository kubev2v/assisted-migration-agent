package services

import (
	"context"
	"sync"

	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"
	"github.com/kubev2v/migration-planner/pkg/opa"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

type ServiceManager struct {
	dataFolder string
	pool       *store.Pool

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

func WithDataFolder(dataFolder string) ServiceManagerOption {
	return func(m *ServiceManager) {
		m.dataFolder = dataFolder
	}
}

func WithPool(pool *store.Pool) ServiceManagerOption {
	return func(m *ServiceManager) {
		m.pool = pool
	}
}

func WithOpaValidator(v *opa.Validator) ServiceManagerOption {
	return func(m *ServiceManager) {
		m.validator = v
	}
}

func WithCollectorWorkBuilder(fn CollectorWorkBuilder) ServiceManagerOption {
	return func(m *ServiceManager) {
		m.workBuilder = fn
	}
}

func WithCollectionService(s *CollectionService) ServiceManagerOption {
	return func(m *ServiceManager) { m.collection = s }
}

func WithCredentialsService(s *CredentialsService) ServiceManagerOption {
	return func(m *ServiceManager) { m.credentials = s }
}

func WithVddkService(s *VddkService) ServiceManagerOption {
	return func(m *ServiceManager) { m.vddk = s }
}

func WithForecasterService(s *ForecasterService) ServiceManagerOption {
	return func(m *ServiceManager) { m.forecaster = s }
}

func NewServiceManager(opts ...ServiceManagerOption) *ServiceManager {
	m := &ServiceManager{}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// SetConsoleService completes Agent wiring after the console receives the manager.
func (m *ServiceManager) SetConsoleService(s *Console) {
	m.console = s
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
		factory, err := newVCenterCollectorWorkFactory(m.credentials, m.pool, m.dataFolder, m.validator)
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

	factory, err := newRvtoolWorkFactory(m.pool, rvtoolFiles, m.dataFolder, m.validator)
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

	m.inspector = NewInspectorService(store, 10, m.dataFolder, m.credentials)

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

func (m *ServiceManager) LatestApplicationService() (*ApplicationService, error) {
	db, err := m.pool.Latest()
	if err != nil {
		return nil, err
	}
	st, err := db.Store()
	if err != nil {
		return nil, err
	}
	return NewApplicationService(st)
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

// DeleteData deletes all the collections.
func (m *ServiceManager) DeleteData(ctx context.Context) error {
	// stop service manager
	m.Stop(ctx)

	// wipe out data
	if err := m.collection.DeleteAll(ctx); err != nil {
		return err
	}

	// switch agent in disconnected mode
	if err := m.console.SetMode(ctx, models.AgentModeDisconnected); err != nil {
		return err
	}

	return nil
}

// DeleteCollections stops local work and removes all collection databases.
func (m *ServiceManager) DeleteCollections(ctx context.Context) error {
	m.Stop(ctx)
	return m.collection.DeleteAll(ctx)
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

	// Get main database for storage-forecast data
	mainDB, err := m.pool.Get(store.MainDatabaseID)
	if err != nil {
		// If main DB not available, create service without it (will write empty forecast CSV)
		return NewExportService(st), nil
	}
	mainStore, err := mainDB.Store()
	if err != nil {
		return NewExportService(st), nil
	}

	return NewExportServiceWithMain(st, mainStore), nil
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
