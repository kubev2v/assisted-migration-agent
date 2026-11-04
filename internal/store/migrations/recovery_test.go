package migrations

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

func TestMigrationRegistry(t *testing.T) {
	for _, kind := range []SchemaKind{MainSchema, CollectionSchema} {
		r, err := NewRecovery(t.TempDir())
		require.NoError(t, err)
		require.NotEmpty(t, r.registries[kind])
	}

	for _, tc := range []struct {
		name  string
		files fstest.MapFS
	}{
		{"missing down", fstest.MapFS{"main/up/001_test.sql": {Data: []byte("SELECT 1;")}}},
		{"missing up", fstest.MapFS{"main/down/001_test.sql": {Data: []byte("SELECT 1;")}}},
		{"empty down", fstest.MapFS{"main/up/001_test.sql": {Data: []byte("SELECT 1;")}, "main/down/001_test.sql": {Data: []byte("-- TODO\n")}}},
		{"duplicate up", fstest.MapFS{"main/up/001_test.sql": {Data: []byte("SELECT 1;")}, "main/up/0001_test.sql": {Data: []byte("SELECT 1;")}, "main/down/001_test.sql": {Data: []byte("SELECT 1;")}}},
		{"gap", fstest.MapFS{"main/up/002_test.sql": {Data: []byte("SELECT 1;")}, "main/down/002_test.sql": {Data: []byte("SELECT 1;")}}},
		{"malformed", fstest.MapFS{"main/up/no_version.sql": {Data: []byte("SELECT 1;")}}},
		{"conflicting name", fstest.MapFS{"main/up/001_test.sql": {Data: []byte("SELECT 1;")}, "main/down/001_other.sql": {Data: []byte("SELECT 1;")}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadRegistry(tc.files, "main")
			require.Error(t, err)
		})
	}
	valid := fstest.MapFS{
		"main/up/001_test.sql":   {Data: []byte("SELECT 1;")},
		"main/down/001_test.sql": {Data: []byte("SELECT 2;")},
		"main/up/002_test.sql":   {Data: []byte("SELECT 3;")},
		"main/down/002_test.sql": {Data: []byte("SELECT 4;")},
	}
	r, err := loadRegistry(valid, "main")
	require.NoError(t, err)
	require.Len(t, r, 2)
	require.Equal(t, "SELECT 1;", r[0].Up)
	require.Equal(t, "SELECT 4;", r[1].Down)
}

func TestRecoveryStorageMetadata(t *testing.T) {
	for _, preexisting := range []bool{false, true} {
		name := "new objects"
		if preexisting {
			name = "preexisting objects"
		}
		t.Run(name, func(t *testing.T) {
			folder := t.TempDir()
			r, err := NewRecovery(folder)
			require.NoError(t, err)
			seedMain(t, folder)
			seedCollection(t, folder, "collection_100.duckdb", preexisting, false)
			seedCollection(t, folder, "collection_200.duckdb", true, true)
			request := prepareRequest()
			_, err = r.Run(context.Background(), request)
			require.NoError(t, err)
			manifestPath := filepath.Join(folder, "updates/attempts", request.AttemptID, "manifest.json")
			original, err := os.ReadFile(manifestPath)
			require.NoError(t, err)
			_, err = r.Run(context.Background(), request)
			require.ErrorContains(t, err, "creating new attempt")
			after, err := os.ReadFile(manifestPath)
			require.NoError(t, err)
			require.Equal(t, original, after)

			request.Operation = "apply"
			result, err := r.Run(context.Background(), request)
			require.NoError(t, err)
			require.Equal(t, []int{2}, result.Databases[0].Steps)
			require.Empty(t, result.Databases[1].Steps)
			request.Operation = "verify"
			_, err = r.Run(context.Background(), request)
			require.Error(t, err)
			before, err := os.ReadFile(filepath.Join(folder, "collection_100.duckdb"))
			require.NoError(t, err)
			request.Operation, request.DryRun = "rollback", true
			result, err = r.Run(context.Background(), request)
			require.NoError(t, err)
			require.Equal(t, []int{2}, result.Databases[0].Steps)
			after, err = os.ReadFile(filepath.Join(folder, "collection_100.duckdb"))
			require.NoError(t, err)
			require.Equal(t, before, after, "dry-run must not change database bytes")
			request.DryRun = false
			_, err = r.Run(context.Background(), request)
			require.NoError(t, err)
			result, err = r.Run(context.Background(), request)
			require.NoError(t, err)
			for _, database := range result.Databases {
				require.Empty(t, database.Steps)
			}
			request.Operation = "verify"
			_, err = r.Run(context.Background(), request)
			require.NoError(t, err)
			if preexisting {
				db := testDatabase(t, filepath.Join(folder, "collection_100.duckdb"))
				var device, vendor, lun, filesystem string
				require.NoError(t, db.QueryRow(`SELECT "Device" FROM vhba`).Scan(&device))
				require.NoError(t, db.QueryRow(`SELECT "LUN Vendor" FROM vhostscsi`).Scan(&vendor))
				require.Equal(t, "keep-device", device)
				require.Equal(t, "keep-vendor", vendor)
				require.NoError(t, db.QueryRow(`SELECT "RDM LUN UUID" FROM vdisk`).Scan(&lun))
				require.NoError(t, db.QueryRow(`SELECT "Filesystem Type" FROM vdatastore`).Scan(&filesystem))
				require.Equal(t, "keep-lun", lun)
				require.Equal(t, "keep-filesystem", filesystem)
				require.NoError(t, db.Close())
			}
			request.Operation = "apply"
			_, err = r.Run(context.Background(), request)
			require.NoError(t, err, "down then up must succeed")
		})
	}
}

func TestRecoveryPartialForwardAndDescendingRollback(t *testing.T) {
	folder := t.TempDir()
	r, err := NewRecovery(folder)
	require.NoError(t, err)
	seedMain(t, folder)
	r.registries[MainSchema] = append(r.registries[MainSchema],
		migration{Version: 2, Name: "second", Up: `CREATE TABLE second (value VARCHAR); INSERT INTO second VALUES ('new');`, Down: `DROP TABLE second;`},
		migration{Version: 3, Name: "third", Up: `CREATE TABLE third (value VARCHAR); INSERT INTO third SELECT value FROM second;`, Down: `DELETE FROM second WHERE value IN (SELECT value FROM third); DROP TABLE third;`},
		migration{Version: 4, Name: "fail", Up: `CREATE TABLE incomplete (value VARCHAR); SELECT * FROM missing_table;`, Down: `DROP TABLE incomplete;`},
	)
	request := prepareRequest()
	_, err = r.Run(context.Background(), request)
	require.NoError(t, err)
	request.Operation = "apply"
	result, err := r.Run(context.Background(), request)
	require.ErrorContains(t, err, "migration 4")
	require.Equal(t, []int{1, 2, 3}, result.Databases[0].Versions)
	request.Operation = "rollback"
	result, err = r.Run(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, []int{3, 2}, result.Databases[0].Steps)
	request.Operation = "verify"
	_, err = r.Run(context.Background(), request)
	require.NoError(t, err)
	db := testDatabase(t, filepath.Join(folder, "agent.duckdb"))
	var count int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM information_schema.tables WHERE table_name = 'incomplete'`).Scan(&count))
	require.Zero(t, count)
	require.NoError(t, db.Close())
}

func TestRecoveryDownFailureIsAtomicAndRetryable(t *testing.T) {
	folder := t.TempDir()
	r, err := NewRecovery(folder)
	require.NoError(t, err)
	seedMain(t, folder)
	r.registries[MainSchema] = append(r.registries[MainSchema], migration{
		Version: 2, Name: "reversible", Up: `CREATE TABLE second (value INTEGER); INSERT INTO second VALUES (5);`,
		Down: `UPDATE second SET value = value + 1; SELECT CASE WHEN (SELECT value FROM second) = 6 THEN error('blocked') ELSE 1 END; DROP TABLE second;`,
	})
	request := prepareRequest()
	_, err = r.Run(context.Background(), request)
	require.NoError(t, err)
	request.Operation = "apply"
	_, err = r.Run(context.Background(), request)
	require.NoError(t, err)
	request.Operation = "rollback"
	_, err = r.Run(context.Background(), request)
	require.ErrorContains(t, err, "migration 2")
	db := testDatabase(t, filepath.Join(folder, "agent.duckdb"))
	var value, version int
	require.NoError(t, db.QueryRow(`SELECT value FROM second`).Scan(&value))
	require.Equal(t, 5, value, "failed down SQL must roll back data changes")
	require.NoError(t, db.QueryRow(`SELECT max(version) FROM schema_migrations`).Scan(&version))
	require.Equal(t, 2, version)
	_, err = db.Exec(`UPDATE second SET value=10`)
	require.NoError(t, err)
	require.NoError(t, db.Close())
	_, err = r.Run(context.Background(), request)
	require.NoError(t, err)
	request.Operation = "verify"
	_, err = r.Run(context.Background(), request)
	require.NoError(t, err)
}

func TestRecoveryPreflightsEveryDatabase(t *testing.T) {
	folder := t.TempDir()
	r, err := NewRecovery(folder)
	require.NoError(t, err)
	seedMain(t, folder)
	seedCollection(t, folder, "collection_100.duckdb", false, false)
	request := prepareRequest()
	_, err = r.Run(context.Background(), request)
	require.NoError(t, err)
	request.Operation = "apply"
	_, err = r.Run(context.Background(), request)
	require.NoError(t, err)
	db := testDatabase(t, filepath.Join(folder, "agent.duckdb"))
	_, err = db.Exec(`INSERT INTO schema_migrations(version) VALUES (99)`)
	require.NoError(t, err)
	require.NoError(t, db.Close())
	request.Operation = "rollback"
	_, err = r.Run(context.Background(), request)
	require.ErrorContains(t, err, "unsupported migration history")
	db = testDatabase(t, filepath.Join(folder, "collection_100.duckdb"))
	var version int
	require.NoError(t, db.QueryRow(`SELECT max(version) FROM collection_schema_migrations`).Scan(&version))
	require.Equal(t, 2, version, "earlier databases must stay unchanged when a later preflight fails")
	require.NoError(t, db.Close())
}

func TestRecoveryRejectsUnsafeState(t *testing.T) {
	for _, scenario := range []string{"missing file", "missing history", "history gap", "unknown version", "empty history", "symlink", "escape", "changed registry", "manifest version", "manifest path", "inventory", "lock"} {
		t.Run(scenario, func(t *testing.T) {
			folder := t.TempDir()
			r, err := NewRecovery(folder)
			require.NoError(t, err)
			seedMain(t, folder)
			request := prepareRequest()
			_, err = r.Run(context.Background(), request)
			require.NoError(t, err)
			manifestPath := filepath.Join(folder, "updates/attempts", request.AttemptID, "manifest.json")
			switch scenario {
			case "missing file":
				require.NoError(t, os.Remove(filepath.Join(folder, "agent.duckdb")))
			case "missing history", "history gap", "unknown version", "empty history":
				db := testDatabase(t, filepath.Join(folder, "agent.duckdb"))
				query := `DROP TABLE schema_migrations`
				if scenario == "history gap" {
					query = `UPDATE schema_migrations SET version=2`
				}
				if scenario == "unknown version" {
					query = `INSERT INTO schema_migrations(version) VALUES (99)`
				}
				if scenario == "empty history" {
					query = `DELETE FROM schema_migrations`
				}
				_, err = db.Exec(query)
				require.NoError(t, err)
				require.NoError(t, db.Close())
			case "symlink":
				require.NoError(t, os.Rename(filepath.Join(folder, "agent.duckdb"), filepath.Join(folder, "other.duckdb")))
				require.NoError(t, os.Symlink("other.duckdb", filepath.Join(folder, "agent.duckdb")))
			case "escape":
				require.NoError(t, os.Rename(filepath.Join(folder, "updates"), filepath.Join(folder, "saved-updates")))
				require.NoError(t, os.Symlink(t.TempDir(), filepath.Join(folder, "updates")))
			case "changed registry":
				r.registries[MainSchema][0].Down += "\nSELECT 1;"
			case "manifest version", "manifest path":
				var manifest Manifest
				data, err := os.ReadFile(manifestPath)
				require.NoError(t, err)
				require.NoError(t, json.Unmarshal(data, &manifest))
				if scenario == "manifest version" {
					manifest.Version++
				} else {
					manifest.Databases[0].Path = "../agent.duckdb"
				}
				data, err = json.Marshal(manifest)
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(manifestPath, data, 0600))
			case "inventory":
				require.NoError(t, os.WriteFile(filepath.Join(folder, "collection_123.duckdb"), nil, 0600))
			case "lock":
				file, err := os.Open(folder)
				require.NoError(t, err)
				defer func() { _ = file.Close() }()
				require.NoError(t, syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB))
			}
			request.Operation = "rollback"
			_, err = r.Run(context.Background(), request)
			require.Error(t, err)
		})
	}
}

func prepareRequest() Request {
	return Request{Operation: "prepare", AttemptID: "test-upgrade", PreviousImage: "sha256:" + strings.Repeat("a", 64), CandidateImage: "sha256:" + strings.Repeat("b", 64)}
}

func testDatabase(t *testing.T, name string) *sql.DB {
	t.Helper()
	db, err := sql.Open("duckdb", name)
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func seedMain(t *testing.T, folder string) {
	t.Helper()
	db := testDatabase(t, filepath.Join(folder, "agent.duckdb"))
	require.NoError(t, RunMain(context.Background(), db))
	_, err := db.Exec(`INSERT INTO credentials (id,url,username,password) VALUES ('saved','url','user','ciphertext')`)
	require.NoError(t, err)
	require.NoError(t, db.Close())
}

func seedCollection(t *testing.T, folder, name string, preexisting, latest bool) {
	t.Helper()
	db := testDatabase(t, filepath.Join(folder, name))
	_, err := db.Exec(`CREATE TABLE vinfo ("VM ID" VARCHAR PRIMARY KEY);
		CREATE TABLE vdisk ("VM ID" VARCHAR);
		CREATE TABLE vdatastore ("Name" VARCHAR);
		CREATE TABLE collection_schema_migrations (version INTEGER PRIMARY KEY, applied_at TIMESTAMP DEFAULT now());`)
	require.NoError(t, err)
	content, err := collectionMigrationFiles.ReadFile("collection/up/001_initial.sql")
	require.NoError(t, err)
	require.NoError(t, executeMigration(context.Background(), db, historyTable(CollectionSchema), 1, string(content), false))
	if preexisting {
		_, err := db.Exec(`CREATE TABLE vhba ("Device" VARCHAR); INSERT INTO vhba VALUES ('keep-device');
			CREATE TABLE vhostscsi ("LUN Vendor" VARCHAR); INSERT INTO vhostscsi VALUES ('keep-vendor');
			ALTER TABLE vdisk ADD COLUMN "RDM LUN UUID" VARCHAR;
			ALTER TABLE vdatastore ADD COLUMN "Filesystem Type" VARCHAR;
			INSERT INTO vdisk ("VM ID", "RDM LUN UUID") VALUES ('vm-1', 'keep-lun');
			INSERT INTO vdatastore ("Name", "Filesystem Type") VALUES ('ds-1', 'keep-filesystem');`)
		require.NoError(t, err)
	}
	if latest {
		require.NoError(t, RunCollection(context.Background(), db, strings.TrimSuffix(name, ".duckdb")))
	}
	require.NoError(t, db.Close())
}

func TestRecoveryResumesBetweenDatabases(t *testing.T) {
	folder := t.TempDir()
	r, err := NewRecovery(folder)
	require.NoError(t, err)
	seedMain(t, folder)
	seedCollection(t, folder, "collection_100.duckdb", false, false)
	seedCollection(t, folder, "collection_200.duckdb", false, false)
	request := prepareRequest()
	_, err = r.Run(context.Background(), request)
	require.NoError(t, err)
	root, err := os.OpenRoot(folder)
	require.NoError(t, err)
	manifest, err := r.readManifest(root, request.AttemptID)
	require.NoError(t, err)
	require.NoError(t, root.Close())
	request.Operation = "apply"
	_, err = r.Run(context.Background(), request)
	require.NoError(t, err)

	// Model a process stopping after the first database's committed down step.
	baseline := manifest.Databases[0]
	db := testDatabase(t, filepath.Join(folder, baseline.Path))
	content, err := renderDown(r.registries[CollectionSchema][1], baseline)
	require.NoError(t, err)
	require.NoError(t, executeMigration(context.Background(), db, historyTable(CollectionSchema), 2, content, true))
	require.NoError(t, db.Close())
	request.Operation = "rollback"
	result, err := r.Run(context.Background(), request)
	require.NoError(t, err)
	require.Empty(t, result.Databases[0].Steps)
	require.Equal(t, []int{2}, result.Databases[1].Steps)
	request.Operation = "verify"
	_, err = r.Run(context.Background(), request)
	require.NoError(t, err)
	db = testDatabase(t, filepath.Join(folder, "agent.duckdb"))
	var password string
	require.NoError(t, db.QueryRow(`SELECT password FROM credentials WHERE id='saved'`).Scan(&password))
	require.Equal(t, "ciphertext", password)
	require.NoError(t, db.Close())
}

func TestRecoveryRejectsDriftBeforeCommittingDown(t *testing.T) {
	folder := t.TempDir()
	r, err := NewRecovery(folder)
	require.NoError(t, err)
	seedMain(t, folder)
	seedCollection(t, folder, "collection_100.duckdb", false, false)
	seedCollection(t, folder, "collection_200.duckdb", false, false)
	request := prepareRequest()
	_, err = r.Run(context.Background(), request)
	require.NoError(t, err)
	request.Operation = "apply"
	_, err = r.Run(context.Background(), request)
	require.NoError(t, err)
	db := testDatabase(t, filepath.Join(folder, "collection_200.duckdb"))
	_, err = db.Exec(`ALTER TABLE vdisk ADD COLUMN unexpected VARCHAR`)
	require.NoError(t, err)
	require.NoError(t, db.Close())
	request.Operation = "rollback"
	_, err = r.Run(context.Background(), request)
	require.ErrorContains(t, err, "schema columns do not match baseline")
	for _, name := range []string{"collection_100.duckdb", "collection_200.duckdb"} {
		db := testDatabase(t, filepath.Join(folder, name))
		var version int
		require.NoError(t, db.QueryRow(`SELECT max(version) FROM collection_schema_migrations`).Scan(&version))
		require.Equal(t, 2, version)
		require.NoError(t, db.Close())
	}
}
