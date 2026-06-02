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
