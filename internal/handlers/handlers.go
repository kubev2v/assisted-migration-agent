package handlers

import (
	"context"

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
	List(ctx context.Context, params services.VMListParams) ([]models.VMSummary, int, error)
	Get(ctx context.Context, id string) (*models.VM, error)
}

type Handler struct {
	consoleSrv   ConsoleService
	collectorSrv CollectorService
	inventorySrv InventoryService
	vmSrv        VMService
	dataDir      string
}

func New(dataDir string, consoleSrv ConsoleService, collectorSrv CollectorService, invSrv InventoryService, vmSrv VMService) *Handler {
	return &Handler{
		consoleSrv:   consoleSrv,
		collectorSrv: collectorSrv,
		inventorySrv: invSrv,
		vmSrv:        vmSrv,
		dataDir:      dataDir,
	}
}
