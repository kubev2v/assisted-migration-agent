package analyzer

import (
	"context"

	"github.com/kubev2v/assisted-migration-agent/internal/config"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/services"
)

type ServiceProvider interface {
	CollectionService() *services.CollectionService
	ExportService(string) (*services.ExportService, error)
	VirtualMachineService(string) (*services.VMService, error)
	GroupService(string) (*services.GroupService, error)
	InventoryService(string) (*services.InventoryService, error)
	RightsizingService(string) (*services.RightsizingService, error)
	LatestVirtualMachineService() (*services.VMService, error)
	LatestGroupService() (*services.GroupService, error)
	LatestInventoryService() (*services.InventoryService, error)
	LatestRightsizingService() (*services.RightsizingService, error)
	LatestApplicationService() (*services.ApplicationService, error)
	GetCollectorStatus() models.CollectorStatus
	StopCollecting() error
	StartRVToolsCollecting([]string) (models.CollectorStatus, error)
	DeleteCollections(context.Context) error
}

type Handler struct {
	cfg config.Configuration
	svc ServiceProvider
}

func NewHandler(cfg config.Configuration, svc ServiceProvider) *Handler {
	return &Handler{cfg: cfg, svc: svc}
}
