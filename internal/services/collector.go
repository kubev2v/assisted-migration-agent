package services

import (
	"io"
	"strings"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/pkg/scheduler"
)

type CollectorService struct {
	scheduler *scheduler.Scheduler
}

func NewCollectorService(s *scheduler.Scheduler) *CollectorService {
	return &CollectorService{scheduler: s}
}

func (c *CollectorService) Status() models.CollectorStatusType {
	return models.CollectorStatusWaitingForCredentials
}

func (c *CollectorService) Inventory() (io.Reader, error) {
	return strings.NewReader("{}"), nil
}
