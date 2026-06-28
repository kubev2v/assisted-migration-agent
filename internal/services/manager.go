package services

import (
	"context"
	"errors"

	"github.com/kubev2v/assisted-migration-agent/internal/config"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/pkg/console"
	"github.com/kubev2v/assisted-migration-agent/pkg/crypto"
)

const (
	maxVMsPerCycle = 10
	maxPairsPerRun = 10
)

type ServiceManager struct {
	cfg           *config.Configuration
	store         *store.Store
	consoleClient *console.Client
	keyMgr        *crypto.KeyManager

	console     *Console
	credentials *CredentialsService
	collector   *CollectorService
	inspector   *InspectorService
	forecaster  *ForecasterService
	vddk        *VddkService
	inventory   *InventoryService
	event       *EventService
	vm          *VMService
	group       *GroupService
	rightsizing *RightsizingService
	application *ApplicationService
}

type ServiceManagerOption func(*ServiceManager)

func WithConfig(cfg *config.Configuration) ServiceManagerOption {
	return func(m *ServiceManager) {
		m.cfg = cfg
	}
}

func WithStore(st *store.Store) ServiceManagerOption {
	return func(m *ServiceManager) {
		m.store = st
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
	if m.store == nil {
		return errors.New("store is required")
	}
	if m.consoleClient == nil {
		return errors.New("console client is required")
	}

	m.inventory = NewInventoryService(m.store)
	m.event = NewEventService(m.store)

	m.credentials = NewCredentialsService(m.store)
	if m.keyMgr != nil {
		m.credentials.WithKeyManager(m.keyMgr)
	}

	factory := newCollectorWorkFactory(m.store, m.event, m.cfg.Agent.DataFolder, m.cfg.Agent.OpaPoliciesFolder)
	m.collector = NewCollectorService(m.inventory, factory.Build, m.credentials)

	var err error
	m.inspector, err = NewInspectorService(m.store, maxVMsPerCycle, m.cfg.Agent.DataFolder, m.credentials)
	if err != nil {
		return err
	}

	m.forecaster = NewForecasterService(m.store, maxPairsPerRun, m.credentials)

	m.vddk = NewVddkService(m.cfg.Agent.DataFolder, m.store)

	consoleSrv, err := NewConsoleService(
		m.cfg.Agent,
		m.consoleClient,
		m.collector,
		m.store,
		m.event,
	)
	if err != nil {
		m.collector.Stop()
		_ = m.inspector.Stop()
		return err
	}
	m.console = consoleSrv

	m.vm = NewVMService(m.store)
	m.group = NewGroupService(m.store)
	m.rightsizing = NewRightsizingService(m.store)

	m.application, err = NewApplicationService(m.store)
	if err != nil {
		return err
	}

	factory.WithPostCollectionBuilder(m.rightsizing.BuildCollectorWorkUnits(
		rightsizingDefaultLookbackHours,
		rightsizingDefaultIntervalSeconds,
		rightsizingDefaultBatchSize,
	))
	factory.WithPostCollectionBuilder(m.application.BuildCollectorWorkUnits())

	return nil
}

func (m *ServiceManager) ConsoleService() *Console {
	return m.console
}

func (m *ServiceManager) CollectorService() *CollectorService {
	return m.collector
}

func (m *ServiceManager) InspectorService() *InspectorService {
	return m.inspector
}

func (m *ServiceManager) VddkService() *VddkService {
	return m.vddk
}

func (m *ServiceManager) EventService() *EventService {
	return m.event
}

func (m *ServiceManager) InventoryService() *InventoryService {
	return m.inventory
}

func (m *ServiceManager) VirtualMachineService() *VMService {
	return m.vm
}

func (m *ServiceManager) GroupService() *GroupService {
	return m.group
}

func (m *ServiceManager) RightsizingService() *RightsizingService {
	return m.rightsizing
}

func (m *ServiceManager) ForecasterService() *ForecasterService {
	return m.forecaster
}

func (m *ServiceManager) ApplicationService() *ApplicationService {
	return m.application
}

func (m *ServiceManager) CredentialsService() *CredentialsService {
	return m.credentials
}

func (m *ServiceManager) Stop(ctx context.Context) {
	m.console.Stop()
	m.collector.Stop()
	_ = m.inspector.Stop()
	m.rightsizing.Stop()
	_ = m.forecaster.Stop()
}
