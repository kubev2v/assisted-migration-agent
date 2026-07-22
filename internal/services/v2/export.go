package v2

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

	"github.com/xuri/excelize/v2"

	"github.com/kubev2v/assisted-migration-agent/internal/store"
)

type ExportService struct {
	store *store.Store2
}

func NewExportService(st *store.Store2) *ExportService {
	return &ExportService{store: st}
}

func (s *ExportService) SupportedScopes() []string {
	return s.store.Export().SupportedScopes()
}

func (s *ExportService) IsValidScope(scope string) bool {
	return s.store.Export().IsValidScope(scope)
}

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
		if err := exportScope(ctx, exportStore, scope, tmpDir); err != nil {
			return fmt.Errorf("%s export failed: %w", scope, err)
		}
	}

	if err := sanitizeExportDir(tmpDir); err != nil {
		return fmt.Errorf("CSV sanitization failed: %w", err)
	}

	return writeZIP(ctx, tmpDir, w)
}

// WriteExcel generates an Excel workbook with one sheet per requested scope and writes it to w.
func (s *ExportService) WriteExcel(ctx context.Context, scopes []string, w io.Writer) error {
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
		if err := exportScope(ctx, exportStore, scope, tmpDir); err != nil {
			return fmt.Errorf("%s export failed: %w", scope, err)
		}
	}

	if err := sanitizeExportDir(tmpDir); err != nil {
		return fmt.Errorf("CSV sanitization failed: %w", err)
	}

	return writeXLSX(scopes, exportStore, tmpDir, w)
}

func writeXLSX(scopes []string, es *store.ExportStore, tmpDir string, w io.Writer) error {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	for _, scope := range scopes {
		csvFiles := scopeCSVFiles(es, scope)
		for _, cf := range csvFiles {
			if err := addCSVAsSheet(f, cf.sheet, filepath.Join(tmpDir, cf.filename)); err != nil {
				return fmt.Errorf("sheet %s: %w", cf.sheet, err)
			}
		}
	}

	_ = f.DeleteSheet("Sheet1")

	_, err := f.WriteTo(w)
	return err
}

type csvFileEntry struct {
	sheet    string
	filename string
}

func scopeCSVFiles(es *store.ExportStore, scope string) []csvFileEntry {
	if scope == "utilization" {
		return []csvFileEntry{
			{sheet: "VM Utilization", filename: "vm_utilization.csv"},
			{sheet: "Cluster Utilization", filename: "cluster_utilization.csv"},
		}
	}
	filename, ok := es.ScopeFilename(scope)
	if !ok {
		return nil
	}
	return []csvFileEntry{{sheet: scopeSheetName(scope), filename: filename}}
}

var sheetNames = map[string]string{
	"overview":         "Overview",
	"hosts":            "Hosts",
	"clusters":         "Clusters",
	"datastores":       "Datastores",
	"vms":              "VMs",
	"network":          "Networks",
	"applications":     "Applications",
	"groups":           "Groups",
	"inspection":       "Inspection",
	"storage-forecast": "Storage Forecast",
}

func scopeSheetName(scope string) string {
	if name, ok := sheetNames[scope]; ok {
		return name
	}
	return scope
}

func addCSVAsSheet(f *excelize.File, sheet, csvPath string) error {
	file, err := os.Open(csvPath)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	if _, err := f.NewSheet(sheet); err != nil {
		return err
	}

	reader := csv.NewReader(file)
	row := 1
	for {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		for col, val := range record {
			cell, _ := excelize.CoordinatesToCellName(col+1, row)
			_ = f.SetCellValue(sheet, cell, val)
		}
		row++
	}
	return nil
}

func exportScope(ctx context.Context, exportStore *store.ExportStore, scope, tmpDir string) error {
	if scope == "utilization" {
		return exportStore.ExportUtilization(ctx, tmpDir)
	}
	filename, ok := exportStore.ScopeFilename(scope)
	if !ok {
		return fmt.Errorf("unknown export scope: %s", scope)
	}
	return exportStore.CopyScope(ctx, scope, filepath.Join(tmpDir, filename))
}

func writeZIP(ctx context.Context, tmpDir string, w io.Writer) error {
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

func sanitizeExportDir(dir string) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		return sanitizeCSVFile(path)
	})
}

func sanitizeCSVFile(path string) error {
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
