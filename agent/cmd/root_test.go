package cmd

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestRootCommandUsesParsedLoggingFlags(t *testing.T) {
	previousLogger := zap.L()
	t.Cleanup(func() { zap.ReplaceGlobals(previousLogger) })

	root := NewRootCommand("v1.2.3", "agent-commit", "ui-commit")
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetErr(io.Discard)
	root.SetArgs([]string{"version", "--log-level", "error"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if zap.L().Core().Enabled(zapcore.DebugLevel) || !zap.L().Core().Enabled(zapcore.ErrorLevel) {
		t.Fatal("logging level did not use the parsed flag")
	}
	for _, value := range []string{"v1.2.3", "agent-commit", "ui-commit"} {
		if !strings.Contains(output.String(), value) {
			t.Fatalf("version output %q does not contain %q", output.String(), value)
		}
	}
}

func TestRootCommandRejectsInvalidLoggingFlags(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{name: "format", args: []string{"version", "--log-format", "invalid"}, want: "invalid log-format"},
		{name: "level", args: []string{"version", "--log-level", "invalid"}, want: "invalid log level"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := NewRootCommand("test", "test", "test")
			root.SetOut(io.Discard)
			root.SetErr(io.Discard)
			root.SetArgs(tc.args)
			if err := root.Execute(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q, got %v", tc.want, err)
			}
		})
	}
}
