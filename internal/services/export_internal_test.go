package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSanitizeCSVCell(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		{name: "normal", in: "web-server-1", want: "web-server-1"},
		{name: "equals", in: "=1+1", want: "'=1+1"},
		{name: "plus", in: "+cmd", want: "'+cmd"},
		{name: "at", in: "@SUM(A1)", want: "'@SUM(A1)"},
		{name: "tab", in: "\tmalicious", want: "'\tmalicious"},
		{name: "formula minus", in: "-2+3+cmd|", want: "'-2+3+cmd|"},
		{name: "numeric minus", in: "-123.45", want: "-123.45"},
		{name: "zero", in: "0", want: "0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizeCSVCell(tt.in); got != tt.want {
				t.Fatalf("sanitizeCSVCell(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestExportService_sanitizeCSVFile(t *testing.T) {
	svc := &ExportService{}
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.csv")
	if err := os.WriteFile(path, []byte("name,note\n=1+1,normal\n"), 0o644); err != nil {
		t.Fatalf("write sample csv: %v", err)
	}

	if err := svc.sanitizeCSVFile(path); err != nil {
		t.Fatalf("sanitizeCSVFile: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read sanitized csv: %v", err)
	}
	if !strings.Contains(string(data), "'=1+1") {
		t.Fatalf("sanitized csv %q missing escaped formula cell", string(data))
	}
}

func TestExportService_sanitizeExportDir(t *testing.T) {
	svc := &ExportService{}
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.csv")
	if err := os.WriteFile(path, []byte("name\n=1+1\n"), 0o644); err != nil {
		t.Fatalf("write sample csv: %v", err)
	}

	if err := svc.sanitizeExportDir(dir); err != nil {
		t.Fatalf("sanitizeExportDir: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read sanitized csv: %v", err)
	}
	if !strings.Contains(string(data), "'=1+1") {
		t.Fatalf("sanitized csv %q missing escaped formula cell", string(data))
	}
}
