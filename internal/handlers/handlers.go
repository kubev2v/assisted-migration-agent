package handlers

import (
	"github.com/kubev2v/assisted-migration-agent/internal/services"
)

type Handler struct {
	consoleSrv *services.Console
	collector  *services.CollectorService
}

func New(consoleSrv *services.Console, collector *services.CollectorService) *Handler {
	return &Handler{
		consoleSrv: consoleSrv,
		collector:  collector,
	}
}
