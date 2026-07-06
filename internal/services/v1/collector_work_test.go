package v1

import (
	"testing"

	"github.com/kubev2v/migration-planner/pkg/inventory"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/pkg/work"
)

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

func TestCollectorWorkFactory_Build(t *testing.T) {
	f := newCollectorWorkFactory(nil, "", "")
	units := drainBuilder(f.Build(models.Credentials{}))

	// Pipeline: connect, verify, collect, ingest, app-detect, rightsizing, inventory, event.
	if len(units) != 8 {
		t.Fatalf("expected 8 units, got %d", len(units))
	}

	last := units[len(units)-1]
	if s := last.Status(); s.State != models.CollectorStateCollected {
		t.Errorf("expected last unit status CollectorStateCollected, got %q", s.State)
	}
}

func TestEmbedClusterUtilization_Mapping(t *testing.T) {
	tests := []struct {
		name     string
		clusters []models.RightsizingClusterUtilization
		want     int
	}{
		{
			name: "single cluster",
			clusters: []models.RightsizingClusterUtilization{
				{
					ClusterID:   "domain-c123",
					ClusterName: "Prod-Cluster",
					CpuAvg:      45.2,
					CpuP95:      67.8,
					CpuMax:      89.3,
					MemAvg:      52.1,
					MemP95:      71.4,
					MemMax:      94.2,
					Confidence:  87.5,
				},
			},
			want: 1,
		},
		{
			name: "multiple clusters",
			clusters: []models.RightsizingClusterUtilization{
				{
					ClusterID:   "domain-c123",
					ClusterName: "Prod-Cluster",
					CpuAvg:      45.2,
					CpuP95:      67.8,
					CpuMax:      89.3,
					MemAvg:      52.1,
					MemP95:      71.4,
					MemMax:      94.2,
					Confidence:  87.5,
				},
				{
					ClusterID:   "domain-c456",
					ClusterName: "Dev-Cluster",
					CpuAvg:      30.5,
					CpuP95:      48.2,
					CpuMax:      65.0,
					MemAvg:      38.7,
					MemP95:      55.9,
					MemMax:      78.1,
					Confidence:  92.3,
				},
			},
			want: 2,
		},
		{
			name:     "no clusters",
			clusters: []models.RightsizingClusterUtilization{},
			want:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inv := &inventory.Inventory{
				VCenterID: "vcenter-1",
				Clusters:  make(map[string]inventory.InventoryData),
			}

			for _, c := range tt.clusters {
				inv.Clusters[c.ClusterID] = inventory.InventoryData{}
			}

			if len(tt.clusters) > 0 {
				utilizationByClusterID := make(map[string]*inventory.ClusterUtilization, len(tt.clusters))
				for _, c := range tt.clusters {
					utilizationByClusterID[c.ClusterID] = &inventory.ClusterUtilization{
						CpuAvg:     sanitizePercentage("cpu_avg", c.CpuAvg),
						CpuP95:     sanitizePercentage("cpu_p95", c.CpuP95),
						CpuMax:     sanitizePercentage("cpu_max", c.CpuMax),
						MemAvg:     sanitizePercentage("mem_avg", c.MemAvg),
						MemP95:     sanitizePercentage("mem_p95", c.MemP95),
						MemMax:     sanitizePercentage("mem_max", c.MemMax),
						Confidence: sanitizePercentage("confidence", c.Confidence),
					}
				}

				for clusterID, clusterData := range inv.Clusters {
					if util, exists := utilizationByClusterID[clusterID]; exists {
						clusterData.ClusterUtilization = util
						inv.Clusters[clusterID] = clusterData
					}
				}
			}

			embeddedCount := 0
			for _, clusterData := range inv.Clusters {
				if clusterData.ClusterUtilization != nil {
					embeddedCount++
				}
			}

			if embeddedCount != tt.want {
				t.Errorf("expected %d clusters with utilization, got %d", tt.want, embeddedCount)
			}

			for _, c := range tt.clusters {
				clusterData, exists := inv.Clusters[c.ClusterID]
				if !exists {
					t.Errorf("cluster %s not found in Clusters map", c.ClusterID)
					continue
				}

				mapped := clusterData.ClusterUtilization
				if mapped == nil {
					t.Errorf("cluster %s has nil ClusterUtilization", c.ClusterID)
					continue
				}

				if mapped.CpuAvg != c.CpuAvg {
					t.Errorf("CpuAvg: expected %f, got %f", c.CpuAvg, mapped.CpuAvg)
				}
				if mapped.CpuP95 != c.CpuP95 {
					t.Errorf("CpuP95: expected %f, got %f", c.CpuP95, mapped.CpuP95)
				}
				if mapped.CpuMax != c.CpuMax {
					t.Errorf("CpuMax: expected %f, got %f", c.CpuMax, mapped.CpuMax)
				}
				if mapped.MemAvg != c.MemAvg {
					t.Errorf("MemAvg: expected %f, got %f", c.MemAvg, mapped.MemAvg)
				}
				if mapped.MemP95 != c.MemP95 {
					t.Errorf("MemP95: expected %f, got %f", c.MemP95, mapped.MemP95)
				}
				if mapped.MemMax != c.MemMax {
					t.Errorf("MemMax: expected %f, got %f", c.MemMax, mapped.MemMax)
				}
				if mapped.Confidence != c.Confidence {
					t.Errorf("Confidence: expected %f, got %f", c.Confidence, mapped.Confidence)
				}
			}
		})
	}
}
