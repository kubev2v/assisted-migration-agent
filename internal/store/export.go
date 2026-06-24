package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// ExportStore runs DuckDB COPY queries for CSV export scopes.
type ExportStore struct {
	db QueryInterceptor
}

func NewExportStore(db QueryInterceptor) *ExportStore {
	return &ExportStore{db: db}
}

// SupportedScopes returns all export scope names.
func (s *ExportStore) SupportedScopes() []string {
	scopes := make([]string, 0, len(exportScopes)+1)
	for scope := range exportScopes {
		scopes = append(scopes, scope)
	}
	scopes = append(scopes, "utilization")
	slices.Sort(scopes)
	return scopes
}

// IsValidScope reports whether scope is a known export scope.
func (s *ExportStore) IsValidScope(scope string) bool {
	if scope == "utilization" {
		return true
	}
	_, ok := exportScopes[scope]
	return ok
}

// ExportSupportedScopes returns all export scope names without a database connection.
func ExportSupportedScopes() []string {
	return (&ExportStore{}).SupportedScopes()
}

// IsExportScope reports whether name is a known export scope without a database connection.
func IsExportScope(name string) bool {
	return (&ExportStore{}).IsValidScope(name)
}

// ScopeFilename returns the CSV filename for a COPY scope.
func (s *ExportStore) ScopeFilename(scope string) (string, bool) {
	spec, ok := exportScopes[scope]
	if !ok {
		return "", false
	}
	return spec.filename, true
}

// CopyScope writes the CSV for scope to path using DuckDB COPY.
func (s *ExportStore) CopyScope(ctx context.Context, scope, path string) error {
	spec, ok := exportScopes[scope]
	if !ok {
		return fmt.Errorf("unknown export scope: %s", scope)
	}
	return s.copyQueryToFile(ctx, spec.query, path, spec.filename)
}

// ExportUtilization writes vm_utilization.csv and cluster_utilization.csv into dir.
func (s *ExportStore) ExportUtilization(ctx context.Context, dir string) error {
	reportID, err := s.latestRightsizingReportID(ctx)
	if err != nil {
		return fmt.Errorf("resolve rightsizing report: %w", err)
	}
	if reportID == "" {
		return writeUtilizationHeaderCSVs(dir)
	}

	reportIDLiteral := duckDBStringLiteral(reportID)

	vmQuery := fmt.Sprintf(vmUtilizationCopyQueryTmpl, reportIDLiteral)
	if err := s.copyQueryToFile(ctx, vmQuery, filepath.Join(dir, "vm_utilization.csv"), "vm_utilization.csv"); err != nil {
		return err
	}

	clusterQuery := fmt.Sprintf(clusterUtilizationCopyQueryTmpl, reportIDLiteral)
	return s.copyQueryToFile(ctx, clusterQuery, filepath.Join(dir, "cluster_utilization.csv"), "cluster_utilization.csv")
}

func (s *ExportStore) copyQueryToFile(ctx context.Context, query, path, label string) error {
	if _, err := s.db.ExecContext(ctx, query, path); err != nil {
		return fmt.Errorf("%s CSV generation failed: %w", label, err)
	}
	return nil
}

func (s *ExportStore) latestRightsizingReportID(ctx context.Context) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx, latestRightsizingReportIDQuery).Scan(&id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return id, nil
}

func duckDBStringLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func writeUtilizationHeaderCSVs(dir string) error {
	vmHeader := "vm_name,vm_id,cluster,cpu_avg_pct,cpu_p95_pct,cpu_max_pct,cpu_latest_pct,mem_avg_pct,mem_p95_pct,mem_max_pct,mem_latest_pct,disk_pct,confidence_pct,provisioned_cpus,provisioned_memory_mb,provisioned_disk_kb,report_timestamp\n"
	clusterHeader := "cluster,vm_count,cpu_avg_pct,cpu_p95_pct,cpu_max_pct,mem_avg_pct,mem_p95_pct,mem_max_pct,disk_pct,confidence_pct,total_provisioned_cpus,total_provisioned_memory_mb,total_provisioned_disk_kb,report_timestamp\n"

	if err := os.WriteFile(filepath.Join(dir, "vm_utilization.csv"), []byte(vmHeader), 0o644); err != nil {
		return fmt.Errorf("vm_utilization CSV generation failed: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cluster_utilization.csv"), []byte(clusterHeader), 0o644); err != nil {
		return fmt.Errorf("cluster_utilization CSV generation failed: %w", err)
	}
	return nil
}
