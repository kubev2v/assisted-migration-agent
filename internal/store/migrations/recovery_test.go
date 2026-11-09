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
		{"missing down", fstest.MapFS{
			"main/up/001_test.sql": {Data: []byte("SELECT 1;")},
			"main/up/002_test.sql": {Data: []byte("SELECT 2;")},
		}},
		{"missing up", fstest.MapFS{"main/down/001_test.sql": {Data: []byte("SELECT 1;")}}},
		{"empty up", fstest.MapFS{"main/up/001_test.sql": {Data: []byte("-- TODO\n")}}},
		{"empty down", fstest.MapFS{
			"main/up/001_test.sql":   {Data: []byte("SELECT 1;")},
			"main/up/002_test.sql":   {Data: []byte("SELECT 2;")},
			"main/down/002_test.sql": {Data: []byte("-- TODO\n")},
		}},
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
		"main/up/002_test.sql":   {Data: []byte("SELECT 3;")},
		"main/down/002_test.sql": {Data: []byte("SELECT 4;")},
	}
	r, err := loadRegistry(valid, "main")
	require.NoError(t, err)
	require.Len(t, r, 2)
	require.Equal(t, "SELECT 1;", r[0].Up)
	require.Empty(t, r[0].Down)
	require.Equal(t, "SELECT 4;", r[1].Down)
	valid["main/down/001_test.sql"] = &fstest.MapFile{Data: []byte("-- Permanent baseline: never reversed.\n")}
	_, err = loadRegistry(valid, "main")
	require.NoError(t, err)

	collection := fstest.MapFS{
		"collection/up/001_initial.sql":   {Data: []byte("SELECT 1;")},
		"collection/up/002_storage.sql":   {Data: []byte("SELECT 2;")},
		"collection/down/001_initial.sql": {Data: []byte("-- Permanent baseline: never reversed.\n")},
		"collection/down/002_storage.sql": {Data: []byte("-- Permanent baseline: never reversed.\n")},
	}
	r, err = loadRegistry(collection, "collection")
	require.NoError(t, err)
	require.Contains(t, r[0].Down, "Permanent baseline")
	require.Contains(t, r[1].Down, "Permanent baseline")
	collection["collection/up/003_new.sql"] = &fstest.MapFile{Data: []byte("SELECT 3;")}
	_, err = loadRegistry(collection, "collection")
	require.ErrorContains(t, err, "requires an up/down pair")
	collection["collection/down/003_new.sql"] = &fstest.MapFile{Data: []byte("-- TODO\n")}
	_, err = loadRegistry(collection, "collection")
	require.ErrorContains(t, err, "empty migration")
	collection["collection/down/003_new.sql"] = &fstest.MapFile{Data: []byte("SELECT 4;")}
	_, err = loadRegistry(collection, "collection")
	require.NoError(t, err)
}

func TestRecoveryRejectsMalformedCollectionNames(t *testing.T) {
	for _, filename := range []string{"collection_backup.duckdb", "collection_.duckdb", "collection_9223372036854775808.duckdb"} {
		for _, operation := range []string{"prepare", "apply", "rollback", "verify"} {
			t.Run(filename+"/"+operation, func(t *testing.T) {
				folder := t.TempDir()
				seedMain(t, folder)
				seedCollection(t, folder, "collection_100.duckdb", false, true)
				r, err := NewRecovery(folder)
				require.NoError(t, err)
				addCollectionUpgrade(r)
				request := prepareRequest()
				if operation != "prepare" {
					_, err = r.Run(context.Background(), request)
					require.NoError(t, err)
				}
				malformed := filepath.Join(folder, filename)
				contents := []byte("leave this file alone")
				require.NoError(t, os.WriteFile(malformed, contents, 0600))
				collection := filepath.Join(folder, "collection_100.duckdb")
				before, err := os.ReadFile(collection)
				require.NoError(t, err)

				request.Operation = operation
				_, err = r.Run(context.Background(), request)
				require.ErrorContains(t, err, "failed to validate collection filename "+filename)
				after, err := os.ReadFile(collection)
				require.NoError(t, err)
				require.Equal(t, before, after)
				actual, err := os.ReadFile(malformed)
				require.NoError(t, err)
				require.Equal(t, contents, actual)
				if operation == "prepare" {
					_, err = os.Stat(filepath.Join(folder, "updates/attempts", request.AttemptID, "manifest.json"))
					require.ErrorIs(t, err, os.ErrNotExist)
				}
			})
		}
	}
}

func TestRecoveryPreservesBaselineData(t *testing.T) {
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
			seedCollection(t, folder, "collection_100.duckdb", preexisting, true)
			seedCollection(t, folder, "collection_200.duckdb", true, true)
			addCollectionUpgrade(r)
			baselineColumns := collectionColumns(t, folder, "collection_100.duckdb")
			request := prepareRequest()
			_, err = r.Run(context.Background(), request)
			require.NoError(t, err)
			manifestPath := filepath.Join(folder, "updates/attempts", request.AttemptID, "manifest.json")
			original, err := os.ReadFile(manifestPath)
			require.NoError(t, err)
			var manifest Manifest
			require.NoError(t, json.Unmarshal(original, &manifest))
			require.Equal(t, []int{1, 2, 3, 4}, manifest.Databases[0].Versions)
			require.Equal(t, []int{1, 2, 3, 4}, manifest.Databases[1].Versions)
			require.Equal(t, []int{1}, manifest.Databases[2].Versions)
			_, err = r.Run(context.Background(), request)
			require.ErrorContains(t, err, "creating new attempt")
			after, err := os.ReadFile(manifestPath)
			require.NoError(t, err)
			require.Equal(t, original, after)

			request.Operation = "apply"
			result, err := r.Run(context.Background(), request)
			require.NoError(t, err)
			require.Equal(t, []int{5}, result.Databases[0].Steps)
			require.Equal(t, []int{5}, result.Databases[1].Steps)
			request.Operation = "verify"
			_, err = r.Run(context.Background(), request)
			require.Error(t, err)
			before, err := os.ReadFile(filepath.Join(folder, "collection_100.duckdb"))
			require.NoError(t, err)
			request.Operation, request.DryRun = "rollback", true
			result, err = r.Run(context.Background(), request)
			require.NoError(t, err)
			require.Equal(t, []int{5}, result.Databases[0].Steps)
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
			require.Equal(t, baselineColumns, collectionColumns(t, folder, "collection_100.duckdb"))
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
	seedCollection(t, folder, "collection_100.duckdb", false, true)
	addCollectionUpgrade(r)
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
	require.Equal(t, 5, version, "earlier databases must stay unchanged when a later preflight fails")
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
			if scenario == "history gap" {
				require.ErrorContains(t, err, "unsupported migration history")
			}
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
	seedCollection(t, folder, "collection_100.duckdb", false, true)
	seedCollection(t, folder, "collection_200.duckdb", false, true)
	addCollectionUpgrade(r)
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
	require.NoError(t, executeMigration(context.Background(), db, historyTable(CollectionSchema), 5, r.registries[CollectionSchema][4].Down, true))
	require.NoError(t, db.Close())
	request.Operation = "rollback"
	result, err := r.Run(context.Background(), request)
	require.NoError(t, err)
	require.Empty(t, result.Databases[0].Steps)
	require.Equal(t, []int{5}, result.Databases[1].Steps)
	request.Operation = "verify"
	_, err = r.Run(context.Background(), request)
	require.NoError(t, err)
	db = testDatabase(t, filepath.Join(folder, "agent.duckdb"))
	var password string
	require.NoError(t, db.QueryRow(`SELECT password FROM credentials WHERE id='saved'`).Scan(&password))
	require.Equal(t, "ciphertext", password)
	require.NoError(t, db.Close())
}

func TestRecoveryIgnoresUnrelatedSchemaChanges(t *testing.T) {
	folder := t.TempDir()
	r, err := NewRecovery(folder)
	require.NoError(t, err)
	seedMain(t, folder)
	seedCollection(t, folder, "collection_100.duckdb", false, true)
	seedCollection(t, folder, "collection_200.duckdb", false, true)
	addCollectionUpgrade(r)
	request := prepareRequest()
	_, err = r.Run(context.Background(), request)
	require.NoError(t, err)
	request.Operation = "apply"
	_, err = r.Run(context.Background(), request)
	require.NoError(t, err)
	db := testDatabase(t, filepath.Join(folder, "collection_200.duckdb"))
	_, err = db.Exec(`ALTER TABLE vdisk ADD COLUMN unexpected VARCHAR;
		INSERT INTO vdisk ("VM ID", unexpected) VALUES ('vm-1', 'retained')`)
	require.NoError(t, err)
	require.NoError(t, db.Close())
	request.Operation = "rollback"
	_, err = r.Run(context.Background(), request)
	require.NoError(t, err)
	request.Operation = "verify"
	_, err = r.Run(context.Background(), request)
	require.NoError(t, err)
	for _, name := range []string{"collection_100.duckdb", "collection_200.duckdb"} {
		db := testDatabase(t, filepath.Join(folder, name))
		var version int
		require.NoError(t, db.QueryRow(`SELECT max(version) FROM collection_schema_migrations`).Scan(&version))
		require.Equal(t, 4, version)
		require.NoError(t, db.Close())
	}

	db = testDatabase(t, filepath.Join(folder, "collection_200.duckdb"))
	var retained string
	require.NoError(t, db.QueryRow(`SELECT unexpected FROM vdisk WHERE "VM ID"='vm-1'`).Scan(&retained))
	require.Equal(t, "retained", retained)
	require.NoError(t, db.Close())
}

func TestRecoveryRejectsOlderCollectionsBeforePreparing(t *testing.T) {
	folder := t.TempDir()
	r, err := NewRecovery(folder)
	require.NoError(t, err)
	seedMain(t, folder)
	seedCollection(t, folder, "collection_100.duckdb", false, true)
	seedCollection(t, folder, "collection_200.duckdb", true, false)
	path := filepath.Join(folder, "collection_200.duckdb")
	before, err := os.ReadFile(path)
	require.NoError(t, err)

	request := prepareRequest()
	_, err = r.Run(context.Background(), request)
	require.ErrorContains(t, err, "collection_200.duckdb")
	require.ErrorContains(t, err, "requires migration version 2 or newer")
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, before, after)
	_, err = os.Stat(filepath.Join(folder, "updates/attempts", request.AttemptID, "manifest.json"))
	require.True(t, os.IsNotExist(err))
}

func TestRecoveryRejectsManifestBelowPermanentBaseline(t *testing.T) {
	folder := t.TempDir()
	r, err := NewRecovery(folder)
	require.NoError(t, err)
	seedMain(t, folder)
	seedCollection(t, folder, "collection_100.duckdb", true, true)
	addCollectionUpgrade(r)
	request := prepareRequest()
	_, err = r.Run(context.Background(), request)
	require.NoError(t, err)
	request.Operation = "apply"
	_, err = r.Run(context.Background(), request)
	require.NoError(t, err)

	path := filepath.Join(folder, "updates/attempts", request.AttemptID, "manifest.json")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var manifest Manifest
	require.NoError(t, json.Unmarshal(data, &manifest))
	manifest.Databases[0].Versions = []int{1}
	data, err = json.Marshal(manifest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0600))

	for _, operation := range []string{"apply", "rollback", "verify"} {
		request.Operation = operation
		_, err = r.Run(context.Background(), request)
		require.ErrorContains(t, err, "requires migration version 2 or newer")
	}
	db := testDatabase(t, filepath.Join(folder, "collection_100.duckdb"))
	var version int
	require.NoError(t, db.QueryRow(`SELECT max(version) FROM collection_schema_migrations`).Scan(&version))
	require.Equal(t, 5, version)
	require.NoError(t, db.Close())
}

func TestRecoveryResumesAfterLaterDownSQLFails(t *testing.T) {
	folder := t.TempDir()
	r, err := NewRecovery(folder)
	require.NoError(t, err)
	seedMain(t, folder)
	seedCollection(t, folder, "collection_100.duckdb", false, true)
	seedCollection(t, folder, "collection_200.duckdb", false, true)
	r.registries[CollectionSchema] = append(r.registries[CollectionSchema], migration{
		Version: 5,
		Name:    "rollback_gate",
		Up:      `CREATE TABLE rollback_gate (allow_down BOOLEAN); INSERT INTO rollback_gate VALUES (false);`,
		Down:    `SELECT CASE WHEN (SELECT allow_down FROM rollback_gate) THEN 1 ELSE error('blocked') END; DROP TABLE rollback_gate;`,
	})
	request := prepareRequest()
	_, err = r.Run(context.Background(), request)
	require.NoError(t, err)
	request.Operation = "apply"
	_, err = r.Run(context.Background(), request)
	require.NoError(t, err)
	db := testDatabase(t, filepath.Join(folder, "collection_100.duckdb"))
	_, err = db.Exec(`UPDATE rollback_gate SET allow_down=true`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	request.Operation = "rollback"
	_, err = r.Run(context.Background(), request)
	require.ErrorContains(t, err, "collection_200.duckdb")
	require.ErrorContains(t, err, "migration 5")
	for _, name := range []string{"collection_100.duckdb", "collection_200.duckdb"} {
		db := testDatabase(t, filepath.Join(folder, name))
		var version int
		require.NoError(t, db.QueryRow(`SELECT max(version) FROM collection_schema_migrations`).Scan(&version))
		if name == "collection_100.duckdb" {
			require.Equal(t, 4, version, "earlier down steps commit without previewing later SQL")
		} else {
			require.Equal(t, 5, version, "the failed step retains its migration history")
		}
		require.NoError(t, db.Close())
	}

	db = testDatabase(t, filepath.Join(folder, "collection_200.duckdb"))
	_, err = db.Exec(`UPDATE rollback_gate SET allow_down=true`)
	require.NoError(t, err)
	require.NoError(t, db.Close())
	_, err = r.Run(context.Background(), request)
	require.NoError(t, err)
	request.Operation = "verify"
	_, err = r.Run(context.Background(), request)
	require.NoError(t, err)
}

func collectionColumns(t *testing.T, folder, name string) []string {
	t.Helper()
	db := testDatabase(t, filepath.Join(folder, name))
	rows, err := db.Query(`SELECT table_name, column_name, data_type FROM information_schema.columns
		WHERE table_catalog = current_database() AND table_schema = 'main'
		ORDER BY table_name, ordinal_position`)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var columns []string
	for rows.Next() {
		var table, column, dataType string
		require.NoError(t, rows.Scan(&table, &column, &dataType))
		columns = append(columns, table+"."+column+":"+dataType)
	}
	require.NoError(t, rows.Err())
	require.NoError(t, db.Close())
	return columns
}

func TestRecoveryStopsAtRecordedStartingVersion(t *testing.T) {
	folder := t.TempDir()
	r, err := NewRecovery(folder)
	require.NoError(t, err)
	seedMain(t, folder)
	seedCollection(t, folder, "collection_100.duckdb", true, true)
	addCollectionUpgrade(r)
	db := testDatabase(t, filepath.Join(folder, "collection_100.duckdb"))
	require.NoError(t, executeMigration(context.Background(), db, historyTable(CollectionSchema), 5, r.registries[CollectionSchema][4].Up, false))
	require.NoError(t, db.Close())
	r.registries[CollectionSchema] = append(r.registries[CollectionSchema], migration{
		Version: 6,
		Name:    "next_upgrade",
		Up:      `ALTER TABLE upgrade_notes ADD COLUMN extra VARCHAR;`,
		Down:    `ALTER TABLE upgrade_notes DROP COLUMN extra;`,
	})
	request := prepareRequest()
	_, err = r.Run(context.Background(), request)
	require.NoError(t, err)
	request.Operation = "apply"
	_, err = r.Run(context.Background(), request)
	require.NoError(t, err)
	request.Operation = "rollback"
	result, err := r.Run(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, []int{6}, result.Databases[0].Steps)
	require.Equal(t, []int{1, 2, 3, 4, 5}, result.Databases[0].Versions)
	request.Operation = "verify"
	_, err = r.Run(context.Background(), request)
	require.NoError(t, err)
	db = testDatabase(t, filepath.Join(folder, "collection_100.duckdb"))
	var value string
	require.NoError(t, db.QueryRow(`SELECT value FROM upgrade_notes`).Scan(&value))
	require.Equal(t, "candidate", value)
	require.NoError(t, db.Close())
}

func addCollectionUpgrade(r *Recovery) {
	r.registries[CollectionSchema] = append(r.registries[CollectionSchema], migration{
		Version: 5,
		Name:    "upgrade_notes",
		Up:      `CREATE TABLE upgrade_notes (value VARCHAR); INSERT INTO upgrade_notes VALUES ('candidate');`,
		Down:    `DROP TABLE upgrade_notes;`,
	})
}
