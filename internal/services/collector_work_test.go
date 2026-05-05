package services

import (
	"context"
	"testing"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/pkg/work"
)

// drainBuilder returns the full list of units emitted by a WorkBuilder.
func drainBuilder(b work.WorkBuilder[models.CollectorStatus, models.CollectorResult]) []work.WorkUnit[models.CollectorStatus, models.CollectorResult] {
	var units []work.WorkUnit[models.CollectorStatus, models.CollectorResult]
	for {
		u, ok := b.Next()
		if !ok {
			break
		}
		units = append(units, u)
	}
	return units
}

func TestCollectorWorkFactory_NoPostCollectionBuilder(t *testing.T) {
	f := newCollectorWorkFactory(nil, nil, "", "")
	units := drainBuilder(f.Build(models.Credentials{}))

	// Base pipeline: connect, collect, parse, save-inventory, collected-event.
	if len(units) != 5 {
		t.Fatalf("expected 5 units without postCollectionBuilder, got %d", len(units))
	}

	// Verify final unit reports CollectorStateCollected.
	last := units[len(units)-1]
	if s := last.Status(); s.State != models.CollectorStateCollected {
		t.Errorf("expected last unit status CollectorStateCollected, got %q", s.State)
	}
}

func TestCollectorWorkFactory_WithPostCollectionBuilder(t *testing.T) {
	extraUnit := work.WorkUnit[models.CollectorStatus, models.CollectorResult]{
		Status: func() models.CollectorStatus {
			return models.CollectorStatus{State: models.CollectorStateRightsizingConnecting}
		},
		Work: func(ctx context.Context, r models.CollectorResult) (models.CollectorResult, error) {
			return r, nil
		},
	}

	f := newCollectorWorkFactory(nil, nil, "", "")
	f.WithPostCollectionBuilder(func(_ models.Credentials) []collectorWorkUnit {
		return []collectorWorkUnit{extraUnit}
	})

	units := drainBuilder(f.Build(models.Credentials{}))

	// Base 4 + 1 extra + 1 final event = 6 total.
	if len(units) != 6 {
		t.Fatalf("expected 6 units with postCollectionBuilder, got %d", len(units))
	}

	// The injected unit comes before save-inventory and the event unit.
	injected := units[len(units)-3]
	if s := injected.Status(); s.State != models.CollectorStateRightsizingConnecting {
		t.Errorf("expected injected unit status RightsizingConnecting, got %q", s.State)
	}

	// The final unit must still be CollectorStateCollected.
	last := units[len(units)-1]
	if s := last.Status(); s.State != models.CollectorStateCollected {
		t.Errorf("expected last unit status CollectorStateCollected, got %q", s.State)
	}
}
