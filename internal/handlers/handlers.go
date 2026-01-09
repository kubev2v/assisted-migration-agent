package handlers

import (
	"github.com/kubev2v/assisted-migration-agent/internal/services"
)

type Handler struct {
	consoleSrv   *services.Console
	collectorSrv *services.CollectorService
	inventorySrv *services.InventoryService
}

func New(consoleSrv *services.Console, collector *services.CollectorService, invSrv *services.InventoryService) *Handler {
	return &Handler{
		consoleSrv:   consoleSrv,
		collectorSrv: collector,
		inventorySrv: invSrv,
	}
}
