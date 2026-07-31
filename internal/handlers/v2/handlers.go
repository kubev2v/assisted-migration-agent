package v2

import (
	"context"

	"github.com/kubev2v/assisted-migration-agent/internal/config"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	svc "github.com/kubev2v/assisted-migration-agent/internal/services/v2"
)

type ServiceProvider interface {
	ConsoleService() *svc.Console
	CollectionService() *svc.CollectionService
	InspectorService() (*svc.InspectorService, error)
	VddkService() *svc.VddkService
	CredentialsService() *svc.CredentialsService
	ForecasterService() *svc.ForecasterService

	ApplicationService(collectionID string) (*svc.ApplicationService, error)
	ComparisonService(aId, bId string) (*svc.ComparisonService, error)
	ExportService(collectionID string) (*svc.ExportService, error)
	VirtualMachineService(collectionID string) (*svc.VMService, error)
	GroupService(collectionID string) (*svc.GroupService, error)
	InventoryService(collectionID string) (*svc.InventoryService, error)
	RightsizingService(collectionID string) (*svc.RightsizingService, error)

	LatestVirtualMachineService() (*svc.VMService, error)
	LatestGroupService() (*svc.GroupService, error)
	LatestInventoryService() (*svc.InventoryService, error)
	LatestRightsizingService() (*svc.RightsizingService, error)

	GetCollectorStatus() models.CollectorStatus
	StartCollecting(ctx context.Context) (models.CollectorStatus, error)
	StopCollecting() error
	StartRVToolsCollecting(rvtoolFiles []string) (models.CollectorStatus, error)
}

type Handler struct {
	cfg config.Configuration
	svc ServiceProvider
}

func NewHandler(cfg config.Configuration, svc ServiceProvider) *Handler {
	return &Handler{cfg: cfg, svc: svc}
}
