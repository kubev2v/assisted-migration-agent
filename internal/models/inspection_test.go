package models_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
)

// TerminalStatus runs condenseInspectionError on the failure error, so these
// cases exercise the condensing behavior end to end.
func TestTerminalStatus_CondensesError(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		wantState   models.InspectionState
		wantErr     string // exact, when non-empty
		wantValid   bool   // result must be valid UTF-8
		wantContain string // substring the result must contain
	}{
		{
			name:      "single-line marker is not duplicated",
			err:       errors.New("v2v inspection failed: libguestfs: error: cannot open disk"),
			wantState: models.InspectionStateError,
			wantErr:   "v2v inspection failed: libguestfs: error: cannot open disk",
			wantValid: true,
		},
		{
			name:      "marker at start of message, no context prefix",
			err:       errors.New("libguestfs: error: cannot open disk\nlaunch trace line 1\nlaunch trace line 2"),
			wantState: models.InspectionStateError,
			wantErr:   "libguestfs: error: cannot open disk",
			wantValid: true,
		},
		{
			name:      "multi-line wrapped error keeps context plus cause only",
			err:       errors.New("v2v inspection failed: detect: libguestfs: error: no bootable device\nverbose trace...\nmore trace"),
			wantState: models.InspectionStateError,
			wantErr:   "v2v inspection failed: detect: libguestfs: error: no bootable device",
			wantValid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := models.TerminalStatus(models.InspectionResult{Err: tt.err})
			if status.State != tt.wantState {
				t.Fatalf("state = %q, want %q", status.State, tt.wantState)
			}
			got := status.Error.Error()
			if tt.wantErr != "" && got != tt.wantErr {
				t.Errorf("error = %q, want %q", got, tt.wantErr)
			}
			if tt.wantValid && !utf8.ValidString(got) {
				t.Errorf("error string is not valid UTF-8: %q", got)
			}
			if strings.Count(got, "libguestfs: error:") > 1 {
				t.Errorf("libguestfs marker duplicated: %q", got)
			}
			if tt.wantContain != "" && !strings.Contains(got, tt.wantContain) {
				t.Errorf("error = %q, want to contain %q", got, tt.wantContain)
			}
		})
	}
}

// A long error whose truncation boundary splits a multibyte rune must not
// produce invalid UTF-8 (DuckDB VARCHAR would reject it, stranding the run).
func TestTerminalStatus_TruncatesOnRuneBoundary(t *testing.T) {
	// "€" (3 bytes: E2 82 AC) straddles byte index 600 so a raw byte slice splits it.
	raw := strings.Repeat("a", 598) + "€" + strings.Repeat("b", 200)

	// Sanity: the naive byte cut the fix replaced would indeed be invalid.
	if utf8.ValidString(raw[:600]) {
		t.Fatalf("test setup wrong: raw[:600] should be invalid UTF-8")
	}

	status := models.TerminalStatus(models.InspectionResult{Err: errors.New(raw)})
	got := status.Error.Error()

	if !utf8.ValidString(got) {
		t.Errorf("truncated error is not valid UTF-8: %q", got)
	}
	if !strings.Contains(got, "truncated") {
		t.Errorf("expected truncation marker, got %q", got)
	}
}

func TestTerminalStatus_CanceledHasNoError(t *testing.T) {
	status := models.TerminalStatus(models.InspectionResult{Err: context.Canceled})
	if status.State != models.InspectionStateCanceled {
		t.Fatalf("state = %q, want canceled", status.State)
	}
	if status.Error != nil {
		t.Errorf("canceled status should have no error, got %v", status.Error)
	}
}
