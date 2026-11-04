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
			if operation == "apply" || operation == "rollback" {
				require.Len(t, result.Databases, 1)
				require.Equal(t, "agent.duckdb", result.Databases[0].Path)
				require.Equal(t, []int{1}, result.Databases[0].Versions)
			}
		})
	}
}

func TestMigrateCLIErrorsAreStructured(t *testing.T) {
	for _, args := range [][]string{
		{"rollback", "--data-folder", t.TempDir(), "--attempt-id", "../escape"},
		{"rollback", "--data-folder", t.TempDir(), "--attempt-id", "missing"},
		{"prepare", "--data-folder", t.TempDir(), "--attempt-id", "no-images"},
		{"verify", "--target", "candidate", "--data-folder", filepath.Join(t.TempDir(), "missing"), "--attempt-id", "invalid-target"},
	} {
		command := NewMigrateCommand()
		var stdout bytes.Buffer
		command.SetOut(&stdout)
		command.SetErr(io.Discard)
		command.SetArgs(args)
		err := command.Execute()
		require.Error(t, err)
		if args[0] == "verify" {
			require.ErrorContains(t, err, "unsupported verification target: candidate")
		}
		var result migrations.Result
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
		require.Equal(t, args[0], result.Operation)
		require.Equal(t, err.Error(), result.Error)
	}
}
