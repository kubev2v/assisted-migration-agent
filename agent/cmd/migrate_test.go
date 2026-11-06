package cmd

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
)

func TestMigrateCLIWithoutApplicationConfiguration(t *testing.T) {
	folder := t.TempDir()
	db, err := sql.Open("duckdb", filepath.Join(folder, "agent.duckdb"))
	require.NoError(t, err)
	require.NoError(t, migrations.RunMain(context.Background(), db))
	require.NoError(t, db.Close())

	for _, operation := range []string{"prepare", "apply", "rollback", "verify"} {
		t.Run(operation, func(t *testing.T) {
			root := NewRootCommand("test", "test", "test")
			var stdout, stderr bytes.Buffer
			root.SetOut(&stdout)
			root.SetErr(&stderr)
			args := []string{"migrate", operation, "--data-folder", folder, "--attempt-id", "cli-test"}
			if operation == "prepare" {
				args = append(args, "--previous-image", "sha256:"+strings.Repeat("a", 64), "--candidate-image", "sha256:"+strings.Repeat("b", 64))
			}
			root.SetArgs(args)
			require.NoError(t, root.Execute(), stderr.String())
			var result migrations.Result
			decoder := json.NewDecoder(&stdout)
			require.NoError(t, decoder.Decode(&result))
			require.ErrorIs(t, decoder.Decode(new(any)), io.EOF)
			require.Equal(t, operation, result.Operation)
			require.Empty(t, result.Error)
		})
	}
}

func TestMigrateCLIErrorsAreStructured(t *testing.T) {
	for _, args := range [][]string{
		{"rollback", "--data-folder", t.TempDir(), "--attempt-id", "../escape"},
		{"rollback", "--data-folder", t.TempDir(), "--attempt-id", "missing"},
		{"prepare", "--data-folder", t.TempDir(), "--attempt-id", "no-images"},
	} {
		command := NewMigrateCommand()
		var stdout bytes.Buffer
		command.SetOut(&stdout)
		command.SetErr(io.Discard)
		command.SetArgs(args)
		require.Error(t, command.Execute())
		var result migrations.Result
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
		require.NotEmpty(t, result.Error)
	}
}
