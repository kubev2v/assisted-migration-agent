package services

import (
	"archive/zip"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kubev2v/assisted-migration-agent/internal/store"
)

// ExportService exports VM inventory and migration planning data as CSV files in a ZIP archive.
type ExportService struct {
	store *store.Store
}

// NewExportService creates a new export service.
func NewExportService(st *store.Store) *ExportService {
	return &ExportService{store: st}
}

// SupportedScopes returns all export scope names accepted by the API and WriteZip.
func (s *ExportService) SupportedScopes() []string {
	return s.store.Export().SupportedScopes()
}

// IsValidScope reports whether scope is a known export scope.
func (s *ExportService) IsValidScope(scope string) bool {
	return s.store.Export().IsValidScope(scope)
}

// WriteZip generates CSV files for the requested scopes and streams a ZIP archive to w.
// Scopes must be pre-validated by the caller (see SupportedScopes and IsValidScope).
func (s *ExportService) WriteZip(ctx context.Context, scopes []string, w io.Writer) error {
	tmpDir, err := os.MkdirTemp("", "export-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	exportStore := s.store.Export()
	for _, scope := range scopes {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.exportScope(ctx, exportStore, scope, tmpDir); err != nil {
			return fmt.Errorf("%s export failed: %w", scope, err)
		}
	}

	if err := s.sanitizeExportDir(tmpDir); err != nil {
		return fmt.Errorf("CSV sanitization failed: %w", err)
	}

	if err := s.writeZIP(ctx, tmpDir, w); err != nil {
		return fmt.Errorf("ZIP creation failed: %w", err)
	}

	return nil
}

func (s *ExportService) exportScope(ctx context.Context, exportStore *store.ExportStore, scope, tmpDir string) error {
	if scope == "utilization" {
		return exportStore.ExportUtilization(ctx, tmpDir)
	}
	filename, ok := exportStore.ScopeFilename(scope)
	if !ok {
		return fmt.Errorf("unknown export scope: %s", scope)
	}
	return exportStore.CopyScope(ctx, scope, filepath.Join(tmpDir, filename))
}

func (s *ExportService) writeZIP(ctx context.Context, tmpDir string, w io.Writer) error {
	zw := zip.NewWriter(w)

	err := filepath.Walk(tmpDir, func(path string, info os.FileInfo, err error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		entry, err := zw.Create(filepath.Base(path))
		if err != nil {
			return err
		}

		f, err := os.Open(path)
		if err != nil {
			return err
		}

		_, copyErr := io.Copy(entry, f)
		closeErr := f.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	if err != nil {
		return err
	}

	return zw.Close()
}

func (s *ExportService) sanitizeExportDir(dir string) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		return s.sanitizeCSVFile(path)
	})
}

func (s *ExportService) sanitizeCSVFile(path string) error {
	in, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	tmpPath := path + ".sanitize"
	out, err := os.Create(tmpPath)
	if err != nil {
		return err
	}

	reader := csv.NewReader(in)
	writer := csv.NewWriter(out)

	for {
		row, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			_ = out.Close()
			_ = os.Remove(tmpPath)
			return err
		}
		for j, cell := range row {
			row[j] = sanitizeCSVCell(cell)
		}
		if err := writer.Write(row); err != nil {
			_ = out.Close()
			_ = os.Remove(tmpPath)
			return err
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		_ = out.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}

	return os.Rename(tmpPath, path)
}

func sanitizeCSVCell(s string) string {
	if s == "" {
		return s
	}

	switch s[0] {
	case '=', '+', '@', '\t', '\r':
		return "'" + s
	case '-':
		if !isNumericCSVCell(s) {
			return "'" + s
		}
	}

	return s
}

func isNumericCSVCell(s string) bool {
	_, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return err == nil
}
