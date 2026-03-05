package handlers

import (
	"context"

	"github.com/kubev2v/assisted-migration-agent/internal/config"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/services"
)

// CollectorService defines the interface for collector operations.
type CollectorService interface {
	GetStatus() models.CollectorStatus
	Start(ctx context.Context, creds *models.Credentials) error
	Stop()
}

// InventoryService defines the interface for inventory operations.
type InventoryService interface {
	GetInventory(ctx context.Context) (*models.Inventory, error)
}

// ConsoleService defines the interface for console/agent operations.
type ConsoleService interface {
	Status() models.ConsoleStatus
	SetMode(ctx context.Context, mode models.AgentMode) error
}

// VMService defines the interface for VM operations.
type VMService interface {
	List(ctx context.Context, params services.VMListParams) ([]models.VirtualMachineSummary, int, error)
	Get(ctx context.Context, id string) (*models.VM, error)
}

// InspectorService defines the interface for deep inspector operations.
type InspectorService interface {
	Start(ctx context.Context, vmIDs []string, cred *models.Credentials) error
	Add(ctx context.Context, vmIDs []string) error
	GetStatus() models.InspectorStatus
	GetVmStatus(ctx context.Context, id string) (models.InspectionStatus, error)
	CancelVmsInspection(ctx context.Context, vmIDs ...string) error
	Stop(ctx context.Context) error
}

// GroupService defines the interface for group operations.
type GroupService interface {
	List(ctx context.Context, params services.GroupListParams) ([]models.Group, int, error)
	ListVirtualMachines(ctx context.Context, id int, params services.GroupGetParams) ([]models.VirtualMachineSummary, int, error)
	Get(ctx context.Context, id int) (*models.Group, error)
	Create(ctx context.Context, group models.Group) (*models.Group, error)
	Update(ctx context.Context, id int, group models.Group) (*models.Group, error)
	Delete(ctx context.Context, id int) error
}

type Handler struct {
	cfg          config.Configuration
	consoleSrv   ConsoleService
	collectorSrv CollectorService
	inventorySrv InventoryService
	inspectorSrv InspectorService
	vmSrv        VMService
	groupSrv     GroupService
}

func NewHandler(cfg config.Configuration) *Handler {
	return &Handler{cfg: cfg}
}

func (h *Handler) WithConsoleService(srv ConsoleService) *Handler {
	h.consoleSrv = srv
	return h
}

func (h *Handler) WithCollectorService(srv CollectorService) *Handler {
	h.collectorSrv = srv
	return h
}

func (h *Handler) WithInventoryService(srv InventoryService) *Handler {
	h.inventorySrv = srv
	return h
}

func (h *Handler) WithVMService(srv VMService) *Handler {
	h.vmSrv = srv
	return h
}

func (h *Handler) WithInspectorService(srv InspectorService) *Handler {
	h.inspectorSrv = srv
	return h
}

func (h *Handler) WithGroupService(srv GroupService) *Handler {
	h.groupSrv = srv
	return h
}
