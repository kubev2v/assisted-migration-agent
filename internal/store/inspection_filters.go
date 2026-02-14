package store

import (
	sq "github.com/Masterminds/squirrel"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
)

type InspectionFilterFunc func(sq.SelectBuilder) sq.SelectBuilder

type InspectionQueryFilter struct {
	filters []InspectionFilterFunc
}

func NewInspectionQueryFilter() *InspectionQueryFilter {
	return &InspectionQueryFilter{
		filters: make([]InspectionFilterFunc, 0),
	}
}

func (f *InspectionQueryFilter) Add(filter InspectionFilterFunc) *InspectionQueryFilter {
	f.filters = append(f.filters, filter)
	return f
}

func (f *InspectionQueryFilter) ByVmIDs(vmIDs ...string) *InspectionQueryFilter {
	if len(vmIDs) == 0 {
		return f
	}
	return f.Add(func(b sq.SelectBuilder) sq.SelectBuilder {
		return b.Where(sq.Eq{inspectionColVmID: vmIDs})
	})
}

func (f *InspectionQueryFilter) ByStatus(statuses ...models.InspectionState) *InspectionQueryFilter {
	if len(statuses) == 0 {
		return f
	}
	statusStrings := make([]string, len(statuses))
	for i, s := range statuses {
		statusStrings[i] = s.Value()
	}
	return f.Add(func(b sq.SelectBuilder) sq.SelectBuilder {
		return b.Where(sq.Eq{inspectionColStatus: statusStrings})
	})
}

func (f *InspectionQueryFilter) Limit(limit int) *InspectionQueryFilter {
	return f.Add(func(b sq.SelectBuilder) sq.SelectBuilder {
		return b.Limit(uint64(limit))
	})
}

func (f *InspectionQueryFilter) OrderBySequence() *InspectionQueryFilter {
	return f.Add(func(b sq.SelectBuilder) sq.SelectBuilder {
		return b.OrderBy(inspectionColSequence + " ASC")
	})
}

func (f *InspectionQueryFilter) Apply(builder sq.SelectBuilder) sq.SelectBuilder {
	for _, filter := range f.filters {
		builder = filter(builder)
	}
	return builder
}

type UpdateFilterFunc func(sq.UpdateBuilder) sq.UpdateBuilder

type InspectionUpdateFilter struct {
	filters []UpdateFilterFunc
}

func NewInspectionUpdateFilter() *InspectionUpdateFilter {
	return &InspectionUpdateFilter{
		filters: make([]UpdateFilterFunc, 0),
	}
}

func (f *InspectionUpdateFilter) ByVmIDs(vmIDs ...string) *InspectionUpdateFilter {
	if len(vmIDs) == 0 {
		return f
	}
	f.filters = append(f.filters, func(b sq.UpdateBuilder) sq.UpdateBuilder {
		return b.Where(sq.Eq{inspectionColVmID: vmIDs})
	})
	return f
}

func (f *InspectionUpdateFilter) ByStatus(statuses ...models.InspectionState) *InspectionUpdateFilter {
	if len(statuses) == 0 {
		return f
	}
	statusStrings := make([]string, len(statuses))
	for i, s := range statuses {
		statusStrings[i] = s.Value()
	}
	f.filters = append(f.filters, func(b sq.UpdateBuilder) sq.UpdateBuilder {
		return b.Where(sq.Eq{inspectionColStatus: statusStrings})
	})
	return f
}

func (f *InspectionUpdateFilter) Apply(builder sq.UpdateBuilder) sq.UpdateBuilder {
	for _, filter := range f.filters {
		builder = filter(builder)
	}
	return builder
}
