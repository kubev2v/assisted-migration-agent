package services

import (
	"testing"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
)

func TestCountMatchedProcesses(t *testing.T) {
	tests := []struct {
		name     string
		apps     []string
		prefixes []string
		want     int
	}{
		{
			name:     "no matches",
			apps:     []string{"nginx", "sshd"},
			prefixes: []string{"httpd", "tomcat"},
			want:     0,
		},
		{
			name:     "one prefix match",
			apps:     []string{"ora_pmon_ORCL", "sshd"},
			prefixes: []string{"ora_mmon_", "ora_pmon_", "ora_smon_"},
			want:     1,
		},
		{
			name:     "all prefixes match",
			apps:     []string{"ora_mmon_ORCL", "ora_pmon_ORCL", "ora_smon_ORCL"},
			prefixes: []string{"ora_mmon_", "ora_pmon_", "ora_smon_"},
			want:     3,
		},
		{
			name:     "duplicate app matches same prefix only once",
			apps:     []string{"ora_pmon_ORCL", "ora_pmon_DB2"},
			prefixes: []string{"ora_pmon_"},
			want:     1,
		},
		{
			name:     "exact match works as prefix",
			apps:     []string{"smbd", "nmbd"},
			prefixes: []string{"smbd", "nmbd"},
			want:     2,
		},
		{
			name:     "empty apps",
			apps:     []string{},
			prefixes: []string{"httpd"},
			want:     0,
		},
		{
			name:     "empty prefixes",
			apps:     []string{"httpd"},
			prefixes: []string{},
			want:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := countMatchedProcesses(tt.apps, tt.prefixes)
			if got != tt.want {
				t.Errorf("countMatchedProcesses() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestMatchApplications_ExcludesEmptyAndSorts(t *testing.T) {
	defs := []applicationDef{
		{Name: "Zebra", Desc: "Z app", Processes: []string{"zebra_proc"}, MinMatched: 1},
		{Name: "Alpha", Desc: "A app", Processes: []string{"alpha_proc"}, MinMatched: 1},
		{Name: "NoMatch", Desc: "No VMs", Processes: []string{"missing_proc"}, MinMatched: 1},
	}

	guestApps := []models.VMGuestApps{
		{ID: "vm-1", Name: "vm-one", AppNames: []string{"zebra_proc_1", "alpha_proc_1"}},
	}

	results := matchApplications(defs, guestApps)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Name != "Alpha" {
		t.Errorf("expected first app to be Alpha, got %s", results[0].Name)
	}
	if results[1].Name != "Zebra" {
		t.Errorf("expected second app to be Zebra, got %s", results[1].Name)
	}
}

func TestMatchApplications_MinMatchedThreshold(t *testing.T) {
	defs := []applicationDef{
		{Name: "Oracle DB", Desc: "Oracle", Processes: []string{"ora_pmon_", "ora_smon_", "ora_mmon_"}, MinMatched: 2},
	}

	tests := []struct {
		name     string
		apps     []string
		wantApps int
	}{
		{
			name:     "below threshold",
			apps:     []string{"ora_pmon_ORCL"},
			wantApps: 0,
		},
		{
			name:     "meets threshold",
			apps:     []string{"ora_pmon_ORCL", "ora_smon_ORCL"},
			wantApps: 1,
		},
		{
			name:     "exceeds threshold",
			apps:     []string{"ora_pmon_ORCL", "ora_smon_ORCL", "ora_mmon_ORCL"},
			wantApps: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			guestApps := []models.VMGuestApps{
				{ID: "vm-1", Name: "db-01", AppNames: tt.apps},
			}
			results := matchApplications(defs, guestApps)
			if len(results) != tt.wantApps {
				t.Errorf("expected %d apps, got %d", tt.wantApps, len(results))
			}
		})
	}
}

func TestMatchApplications_MinMatchedZeroDefaultsToOne(t *testing.T) {
	defs := []applicationDef{
		{Name: "App", Desc: "Test", Processes: []string{"proc_a"}, MinMatched: 0},
	}
	guestApps := []models.VMGuestApps{
		{ID: "vm-1", Name: "vm", AppNames: []string{"proc_a_1"}},
	}

	results := matchApplications(defs, guestApps)

	if len(results) != 1 {
		t.Fatalf("expected 1 app, got %d", len(results))
	}
	if results[0].VMCount != 1 {
		t.Errorf("expected VMCount 1, got %d", results[0].VMCount)
	}
}

func TestMatchApplications_EmptyInputs(t *testing.T) {
	t.Run("no definitions", func(t *testing.T) {
		results := matchApplications(nil, []models.VMGuestApps{
			{ID: "vm-1", Name: "vm", AppNames: []string{"proc"}},
		})
		if len(results) != 0 {
			t.Errorf("expected 0 apps, got %d", len(results))
		}
	})

	t.Run("no VMs", func(t *testing.T) {
		results := matchApplications([]applicationDef{
			{Name: "App", Desc: "Test", Processes: []string{"proc"}, MinMatched: 1},
		}, nil)
		if len(results) != 0 {
			t.Errorf("expected 0 apps, got %d", len(results))
		}
	})

	t.Run("both empty", func(t *testing.T) {
		results := matchApplications(nil, nil)
		if len(results) != 0 {
			t.Errorf("expected 0 apps, got %d", len(results))
		}
	})
}

func TestMatchApplications_MultipleVMsPerApp(t *testing.T) {
	defs := []applicationDef{
		{Name: "PostgreSQL", Desc: "PG", Processes: []string{"postgres"}, MinMatched: 1},
	}
	guestApps := []models.VMGuestApps{
		{ID: "vm-1", Name: "db-01", AppNames: []string{"postgres"}},
		{ID: "vm-2", Name: "db-02", AppNames: []string{"postgres", "nginx"}},
		{ID: "vm-3", Name: "web-01", AppNames: []string{"nginx"}},
	}

	results := matchApplications(defs, guestApps)

	if len(results) != 1 {
		t.Fatalf("expected 1 app, got %d", len(results))
	}
	if results[0].VMCount != 2 {
		t.Errorf("expected 2 VMs, got %d", results[0].VMCount)
	}
	if results[0].VMs[0].ID != "vm-1" || results[0].VMs[1].ID != "vm-2" {
		t.Errorf("unexpected VM IDs: %v", results[0].VMs)
	}
}

func TestNewApplicationService_ParsesEmbeddedJSON(t *testing.T) {
	svc, err := NewApplicationService(nil)
	if err != nil {
		t.Fatalf("NewApplicationService() failed: %v", err)
	}
	if len(svc.defs) == 0 {
		t.Error("expected application definitions to be loaded from embedded JSON")
	}
	for _, def := range svc.defs {
		if def.Name == "" {
			t.Error("found definition with empty name")
		}
		if len(def.Processes) == 0 {
			t.Errorf("definition %q has no processes", def.Name)
		}
	}
}
