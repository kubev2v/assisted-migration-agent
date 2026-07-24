package v2

import (
	"context"
	"errors"
	"sync"

	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"
	"github.com/kubev2v/migration-planner/pkg/opa"

	"github.com/kubev2v/assisted-migration-agent/internal/config"
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

	console      *Console
	collection   *CollectionService
	credentials  *CredentialsService
	collectorMgr *CollectorManager
	mu           sync.Mutex
	inspector    *InspectorService
	vddk         *VddkService
	validator    *opa.Validator
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
		nil,
		mainStore,
		NewEventService(m.pool),
	)
	if err != nil {
		return err
	}

	m.collection = NewCollectionService(m.pool)

	m.credentials = NewCredentialsService(mainStore)
	m.credentials.WithKeyManager(m.keyMgr)

	factory, err := newCollectorWorkFactory(m.pool, m.cfg.Agent.DataFolder, m.validator)
	if err != nil {
		return err
	}
	m.collectorMgr = NewCollectorManager(factory, m.credentials)

	m.vddk = NewVddkService(m.cfg.Agent.DataFolder, m.pool)

	return nil
}

func (m *ServiceManager) ListCollectors() []*CollectorService {
	return m.collectorMgr.List()
}

func (m *ServiceManager) GetCollector(id string) (*CollectorService, error) {
	return m.collectorMgr.Get(id)
}

func (m *ServiceManager) StopCollector(id string) error {
	return m.collectorMgr.Stop(id)
}

func (m *ServiceManager) CreateCollector() (*CollectorService, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.inspector != nil && m.inspector.IsBusy() {
		return nil, srvErrors.NewInspectionInProgressError()
	}

	return m.collectorMgr.Create(), nil
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

	collectors := m.collectorMgr.List()
	for _, c := range collectors {
		if c.GetStatus().State.IsRunning() {
			return nil, srvErrors.NewCollectionInProgressError()
		}
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
	if m.collectorMgr != nil {
		m.collectorMgr.StopAll()
	}

	m.mu.Lock()
	inspector := m.inspector
	m.mu.Unlock()
	if inspector != nil {
		_ = inspector.Stop()
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
