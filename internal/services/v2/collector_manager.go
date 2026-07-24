package v2

import (
	"sync"

	"github.com/google/uuid"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

type CollectorManager struct {
	mu         sync.Mutex
	collectors map[string]*CollectorService
	factory    *collectorWorkFactory
	credsSvc   *CredentialsService
}

func NewCollectorManager(factory *collectorWorkFactory, credsSvc *CredentialsService) *CollectorManager {
	return &CollectorManager{
		collectors: make(map[string]*CollectorService),
		factory:    factory,
		credsSvc:   credsSvc,
	}
}

func (m *CollectorManager) Create() *CollectorService {
	m.mu.Lock()
	defer m.mu.Unlock()

	id := uuid.New().String()
	svc := NewCollectorService(m.factory.Build, m.credsSvc)
	svc.ID = id
	m.collectors[id] = svc
	return svc
}

func (m *CollectorManager) Get(id string) (*CollectorService, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.sweep()

	svc, ok := m.collectors[id]
	if !ok {
		return nil, srvErrors.NewResourceNotFoundError("collector", id)
	}
	return svc, nil
}

func (m *CollectorManager) List() []*CollectorService {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.sweep()

	result := make([]*CollectorService, 0, len(m.collectors))
	for _, svc := range m.collectors {
		result = append(result, svc)
	}
	return result
}

func (m *CollectorManager) Stop(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	svc, ok := m.collectors[id]
	if !ok {
		return srvErrors.NewResourceNotFoundError("collector", id)
	}
	svc.Stop()
	delete(m.collectors, id)
	return nil
}

func (m *CollectorManager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for id, svc := range m.collectors {
		svc.Stop()
		delete(m.collectors, id)
	}
}

func (m *CollectorManager) sweep() {
	for id, svc := range m.collectors {
		if svc.GetStatus().State == models.CollectorStateCollected {
			delete(m.collectors, id)
		}
	}
}
