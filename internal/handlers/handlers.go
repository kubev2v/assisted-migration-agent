package handlers

import (
	"github.com/kubev2v/assisted-migration-agent/internal/services"
)

type Handler struct {
	consoleSrv   *services.Console
	collectorSrv *services.CollectorService
	inventorySrv *services.InventoryService
	vmSrv        *services.VMService
	inspectorSrv *services.InspectorService
}

func New(
	consoleSrv *services.Console,
	collector *services.CollectorService,
	invSrv *services.InventoryService,
	vmSrv *services.VMService,
	inspectorSrv *services.InspectorService,
) *Handler {
	return &Handler{
		consoleSrv:   consoleSrv,
		collectorSrv: collector,
		inventorySrv: invSrv,
		vmSrv:        vmSrv,
		inspectorSrv: inspectorSrv,
	}
}
