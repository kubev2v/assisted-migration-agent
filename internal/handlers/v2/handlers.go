package v2

import (
	"github.com/kubev2v/assisted-migration-agent/internal/config"
	svc "github.com/kubev2v/assisted-migration-agent/internal/services/v2"
)

type ServiceProvider interface {
	ConsoleService() *svc.Console
	CollectionService() *svc.CollectionService
	CollectorManager() *svc.CollectorManager
	CredentialsService() *svc.CredentialsService

	ApplicationService(collectionID string) (*svc.ApplicationService, error)
	ExportService(collectionID string) (*svc.ExportService, error)
	VirtualMachineService(collectionID string) (*svc.VMService, error)
	GroupService(collectionID string) (*svc.GroupService, error)
	InventoryService(collectionID string) (*svc.InventoryService, error)
	RightsizingService(collectionID string) (*svc.RightsizingService, error)

	LatestVirtualMachineService() (*svc.VMService, error)
	LatestGroupService() (*svc.GroupService, error)
	LatestInventoryService() (*svc.InventoryService, error)
	LatestRightsizingService() (*svc.RightsizingService, error)
}

type Handler struct {
	cfg config.Configuration
	svc ServiceProvider
}

func NewHandler(cfg config.Configuration, svc ServiceProvider) *Handler {
	return &Handler{cfg: cfg, svc: svc}
}
