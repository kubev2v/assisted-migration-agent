package services

import (
	"context"
	"errors"
	"github.com/kubev2v/assisted-migration-agent/pkg/vmware"
	"sync"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
	"github.com/kubev2v/assisted-migration-agent/pkg/work"
)

type (
	collectorWorkUnit        = work.WorkUnit[models.CollectorStatus, models.CollectorResult]
	collectorWorkBuilderFunc func(creds models.Credentials) work.WorkBuilder[models.CollectorStatus, models.CollectorResult]
)

type CollectorService struct {
	mu           sync.Mutex
	workSrv      *work.Service[models.CollectorStatus, models.CollectorResult]
	inventorySrv *InventoryService
	buildFn      collectorWorkBuilderFunc
}

func NewCollectorService(inventorySrv *InventoryService, buildFn collectorWorkBuilderFunc) *CollectorService {
	return &CollectorService{
		inventorySrv: inventorySrv,
		buildFn:      buildFn,
	}
}

func (c *CollectorService) GetStatus() models.CollectorStatus {
	inv, err := c.inventorySrv.GetInventory(context.Background())
	if err == nil && inv != nil {
		return models.CollectorStatus{State: models.CollectorStateCollected}
	}

	c.mu.Lock()
	srv := c.workSrv
	c.mu.Unlock()

	if srv != nil {
		state := srv.State()
		if state.Err == nil {
			return state.State
		}
		if !errors.Is(state.Err, work.ErrStopped) {
			return models.CollectorStatus{State: models.CollectorStateError, Error: state.Err}
		}
	}

	return models.CollectorStatus{State: models.CollectorStateReady}
}

func (c *CollectorService) Start(ctx context.Context, creds models.Credentials) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.workSrv != nil && c.workSrv.IsRunning() {
		return srvErrors.NewCollectionInProgressError()
	}

	inv, err := c.inventorySrv.GetInventory(ctx)
	if err == nil && inv != nil {
		return nil
	}

	url, err := vmware.EnsureSdkSuffix(creds.URL)
	if err != nil {
		return err
	}
	creds.URL = url

	srv := work.NewService(models.CollectorStatus{State: models.CollectorStateConnecting}, c.buildFn(creds))
	if err := srv.Start(); err != nil {
		return err
	}

	c.workSrv = srv
	return nil
}

func (c *CollectorService) Stop() {
	c.mu.Lock()
	srv := c.workSrv
	c.mu.Unlock()

	if srv != nil {
		srv.Stop()
	}
}

func (c *CollectorService) WithWorkBuilder(fn collectorWorkBuilderFunc) *CollectorService {
	c.buildFn = fn
	return c
}
