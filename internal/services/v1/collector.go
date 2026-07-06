package v1

import (
	"context"
	"sync"

	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
	"github.com/kubev2v/assisted-migration-agent/pkg/scheduler"
	"github.com/kubev2v/assisted-migration-agent/pkg/vmware"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/pkg/work"
)

type (
	collectorWorkUnit        = work.WorkUnit[models.CollectorStatus, models.CollectorResult]
	collectorWorkBuilderFunc func(creds models.Credentials) work.WorkBuilder2[models.CollectorStatus, models.CollectorResult]
	postCollectionBuilderFn  func(creds models.Credentials) []collectorWorkUnit
)

type CollectorService struct {
	mu           sync.Mutex
	pipeline     *work.Pipeline2[models.CollectorStatus, models.CollectorResult]
	sched        *scheduler.Scheduler[models.CollectorResult]
	done         chan struct{}
	inventorySrv *InventoryService
	buildFn      collectorWorkBuilderFunc
	credsSvc     *CredentialsService
}

func NewCollectorService(inventorySrv *InventoryService, buildFn collectorWorkBuilderFunc, credsSvc *CredentialsService) *CollectorService {
	return &CollectorService{
		inventorySrv: inventorySrv,
		buildFn:      buildFn,
		credsSvc:     credsSvc,
	}
}

func (c *CollectorService) GetStatus() models.CollectorStatus {
	inv, err := c.inventorySrv.GetInventory(context.Background())
	if err == nil && inv != nil {
		return models.CollectorStatus{State: models.CollectorStateCollected}
	}

	c.mu.Lock()
	p := c.pipeline
	c.mu.Unlock()

	if p != nil {
		result, err := p.Result()
		if err != nil {
			return models.CollectorStatus{State: models.CollectorStateError, Error: err}
		}
		if result.Err != nil {
			return models.CollectorStatus{State: models.CollectorStateError, Error: result.Err}
		}
		return p.State()
	}

	return models.CollectorStatus{State: models.CollectorStateReady}
}

func (c *CollectorService) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.done != nil {
		select {
		case <-c.done:
		default:
			return srvErrors.NewCollectionInProgressError()
		}
	}

	inv, err := c.inventorySrv.GetInventory(ctx)
	if err == nil && inv != nil {
		return nil
	}

	creds, err := c.credsSvc.Resolve(ctx)
	if err != nil {
		return err
	}

	url, err := vmware.NormalizeAndValidateURL(creds.URL)
	if err != nil {
		return err
	}
	creds.URL = url

	sched, err := scheduler.NewScheduler[models.CollectorResult](1, 0)
	if err != nil {
		return err
	}

	p := work.NewPipeline2(sched, c.buildFn(creds))
	ticks, err := p.Start()
	if err != nil {
		sched.Close()
		return err
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range ticks {
		}
	}()

	c.pipeline = p
	c.sched = sched
	c.done = done
	return nil
}

func (c *CollectorService) Stop() {
	c.mu.Lock()
	p := c.pipeline
	s := c.sched
	done := c.done
	c.pipeline = nil
	c.sched = nil
	c.done = nil
	c.mu.Unlock()

	if p != nil {
		p.Stop()
	}
	if done != nil {
		<-done
	}
	if s != nil {
		s.Close()
	}
}

func (c *CollectorService) WithWorkBuilder(fn collectorWorkBuilderFunc) *CollectorService {
	c.buildFn = fn
	return c
}
