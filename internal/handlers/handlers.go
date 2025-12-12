package handlers

import (
	"github.com/kubev2v/assisted-migration-agent/internal/services"
)

type Handler struct {
	consoleSrv *services.Console
	collector  services.Collector
}

func New(consoleSrv *services.Console, collector services.Collector) *Handler {
	return &Handler{
		consoleSrv: consoleSrv,
		collector:  collector,
	}
}
