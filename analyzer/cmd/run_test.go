package cmd

import (
	"strings"
	"testing"

	"github.com/kubev2v/assisted-migration-agent/internal/config"
)

func TestAnalyzerCommandHasIndependentFlags(t *testing.T) {
	root := NewRootCommand("test", "test")
	run, _, err := root.Find([]string{"run"})
	if err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{"data-folder", "opa-policies-folder", "server-http-port"} {
		if run.Flags().Lookup(flag) == nil {
			t.Errorf("analyzer run command is missing %q", flag)
		}
	}
	for _, flag := range []string{"rvtools-mode", "agent-id", "source-id", "console-url"} {
		if run.Flags().Lookup(flag) != nil || run.InheritedFlags().Lookup(flag) != nil {
			t.Errorf("analyzer run command exposes Agent flag %q", flag)
		}
	}
}

func TestAnalyzerConfigurationRequiresLocalInputs(t *testing.T) {
	cfg := config.NewConfigurationWithOptionsAndDefaults()
	if err := validateConfiguration(cfg); err == nil || !strings.Contains(err.Error(), "data folder") {
		t.Fatalf("missing data folder: got %v", err)
	}
	cfg.Agent.DataFolder = t.TempDir()
	if err := validateConfiguration(cfg); err == nil || !strings.Contains(err.Error(), "opa policies") {
		t.Fatalf("missing policy folder: got %v", err)
	}
	cfg.Agent.OpaPoliciesFolder = t.TempDir()
	if err := validateConfiguration(cfg); err != nil {
		t.Fatalf("valid analyzer configuration: %v", err)
	}
}
