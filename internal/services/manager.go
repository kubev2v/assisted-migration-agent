package services

import (
	"context"
	"errors"

	"github.com/kubev2v/assisted-migration-agent/internal/config"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/pkg/console"
)

const (
	maxVMsPerCycle = 10
)

type ServiceManager struct {
	cfg           *config.Configuration
	store         *store.Store
	consoleClient *console.Client

	console   *Console
	collector *CollectorService
	inspector *InspectorService
	vddk      *VddkService
	inventory *InventoryService
	vm        *VMService
	group     *GroupService
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

	m.collector = NewCollectorService(
		m.store,
		m.cfg.Agent.DataFolder,
		m.cfg.Agent.OpaPoliciesFolder,
	)

	m.inspector = NewInspectorService(m.store, maxVMsPerCycle, m.cfg.Agent.DataFolder)

	m.vddk = NewVddkService(m.cfg.Agent.DataFolder, m.store)

	consoleSrv, err := NewConsoleService(
		m.cfg.Agent,
		m.consoleClient,
		m.collector,
		m.store,
	)
	if err != nil {
		m.collector.Stop()
		_ = m.inspector.Stop()
		return err
	}
	m.console = consoleSrv

	m.inventory = NewInventoryService(m.store)
	m.vm = NewVMService(m.store)
	m.group = NewGroupService(m.store)

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

func (m *ServiceManager) InventoryService() *InventoryService {
	return m.inventory
}

func (m *ServiceManager) VirtualMachineService() *VMService {
	return m.vm
}

func (m *ServiceManager) GroupService() *GroupService {
	return m.group
}

func (m *ServiceManager) Stop(ctx context.Context) {
	m.console.Stop()
	m.collector.Stop()
	_ = m.inspector.Stop()
}
