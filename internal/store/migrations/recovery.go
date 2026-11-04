package migrations

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"text/template"

	_ "github.com/duckdb/duckdb-go/v2"
)

type Recovery struct {
	dataFolder string
	registries map[SchemaKind]registry
}

type Request struct {
	Operation      string
	AttemptID      string
	PreviousImage  string
	CandidateImage string
	DryRun         bool
}

type DatabaseResult struct {
	Path     string `json:"path"`
	Versions []int  `json:"versions"`
	Steps    []int  `json:"steps"`
}

type Result struct {
	AttemptID string           `json:"attempt_id"`
	Operation string           `json:"operation"`
	DryRun    bool             `json:"dry_run"`
	Databases []DatabaseResult `json:"databases"`
	Error     string           `json:"error,omitempty"`
}

type databasePlan struct {
	baseline DatabaseState
	db       *sql.DB
	steps    []migration
	current  DatabaseState
}

var (
	attemptName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)
	imageID     = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

func NewRecovery(dataFolder string) (*Recovery, error) {
	main, err := loadRegistry(mainMigrationFiles, "main")
	if err != nil {
		return nil, err
	}
	collection, err := loadRegistry(collectionMigrationFiles, "collection")
	if err != nil {
		return nil, err
	}
	return &Recovery{
		dataFolder: dataFolder,
		registries: map[SchemaKind]registry{MainSchema: main, CollectionSchema: collection},
	}, nil
}

// Run requires the application to be stopped. Directory locking serializes offline
// commands; DuckDB locks additionally exclude processes with open database files.
func (r *Recovery) Run(ctx context.Context, request Request) (Result, error) {
	result := Result{AttemptID: request.AttemptID, Operation: request.Operation, DryRun: request.DryRun, Databases: make([]DatabaseResult, 0)}
	if !attemptName.MatchString(request.AttemptID) {
		return result, fmt.Errorf("invalid attempt ID")
	}
	if request.Operation != "prepare" && request.Operation != "apply" && request.Operation != "rollback" && request.Operation != "verify" {
		return result, fmt.Errorf("invalid migration operation")
	}
	if request.DryRun && request.Operation != "rollback" {
		return result, fmt.Errorf("dry-run is supported only for rollback")
	}
	if r.dataFolder == "" {
		return result, fmt.Errorf("data-folder is required")
	}
	root, err := os.OpenRoot(r.dataFolder)
	if err != nil {
		return result, fmt.Errorf("opening data folder: %w", err)
	}
	defer func() { _ = root.Close() }()
	lock, err := root.Open(".")
	if err != nil {
		return result, err
	}
	defer func() { _ = lock.Close() }()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return result, fmt.Errorf("migration data folder is busy: %w", err)
	}

	if request.Operation == "prepare" {
		return r.prepare(ctx, root, request, result)
	}
	manifest, err := r.readManifest(root, request.AttemptID)
	if err != nil {
		return result, err
	}

	// Keep every connection open until the entire plan has been validated and
	// executed. Releasing a file lock between planning and execution is unsafe.
	plans := make([]databasePlan, 0, len(manifest.Databases))
	defer func() {
		for _, plan := range plans {
			_ = plan.db.Close()
		}
	}()
	readOnly := request.DryRun || request.Operation == "verify"
	for _, baseline := range manifest.Databases {
		db, err := openExisting(ctx, root, baseline.Path, readOnly)
		if err != nil {
			return result, fmt.Errorf("%s: %w", baseline.Path, err)
		}
		plans = append(plans, databasePlan{baseline: baseline, db: db})
		plan := &plans[len(plans)-1]
		plan.current, err = readState(ctx, db, baseline.Path, baseline.Kind)
		if err != nil {
			return result, fmt.Errorf("%s: %w", baseline.Path, err)
		}
		reg := r.registries[baseline.Kind]
		if err := reg.validateHistory(plan.current.Versions); err != nil {
			return result, fmt.Errorf("%s: %w", baseline.Path, err)
		}
		if len(plan.current.Versions) < len(baseline.Versions) {
			return result, fmt.Errorf("%s: history is below baseline", baseline.Path)
		}
		if request.Operation == "verify" || len(plan.current.Versions) == len(baseline.Versions) {
			if err := baseline.verify(plan.current); err != nil {
				return result, err
			}
		}
		switch request.Operation {
		case "apply":
			plan.steps = slices.Clone(reg[len(plan.current.Versions):])
		case "rollback":
			plan.steps = slices.Clone(reg[len(baseline.Versions):len(plan.current.Versions)])
			slices.Reverse(plan.steps)
			for i := range plan.steps {
				content, err := renderDown(plan.steps[i], baseline)
				if err != nil {
					return result, err
				}
				plan.steps[i].Down = content
			}
			if !request.DryRun && len(plan.steps) > 0 {
				if err := plan.validateRollback(ctx); err != nil {
					return result, fmt.Errorf("%s: rollback preflight: %w", baseline.Path, err)
				}
			}
		}
	}

	for _, plan := range plans {
		dbResult := DatabaseResult{Path: plan.baseline.Path, Versions: plan.current.Versions, Steps: make([]int, 0, len(plan.steps))}
		result.Databases = append(result.Databases, dbResult)
		out := &result.Databases[len(result.Databases)-1]
		for _, step := range plan.steps {
			out.Steps = append(out.Steps, step.Version)
			if request.DryRun {
				continue
			}
			down := request.Operation == "rollback"
			content := step.Up
			if down {
				content = step.Down
			}
			if err := executeMigration(ctx, plan.db, historyTable(plan.baseline.Kind), step.Version, content, down); err != nil {
				return result, fmt.Errorf("%s: %w", plan.baseline.Path, err)
			}
			if down {
				out.Versions = out.Versions[:len(out.Versions)-1]
			} else {
				out.Versions = append(out.Versions, step.Version)
			}
		}
		if !request.DryRun && request.Operation == "rollback" {
			current, err := readState(ctx, plan.db, plan.baseline.Path, plan.baseline.Kind)
			if err != nil {
				return result, err
			}
			if err := plan.baseline.verify(current); err != nil {
				return result, err
			}
		}
		if !readOnly {
			if _, err := plan.db.ExecContext(ctx, "CHECKPOINT"); err != nil {
				return result, fmt.Errorf("%s: checkpoint: %w", plan.baseline.Path, err)
			}
		}
		if err := plan.db.Close(); err != nil {
			return result, fmt.Errorf("%s: close: %w", plan.baseline.Path, err)
		}
	}
	return result, nil
}

func (p *databasePlan) validateRollback(ctx context.Context) error {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// Preview the complete down path without committing. This catches SQL errors
	// and schema drift in any database before the first persistent down step.
	for _, step := range p.steps {
		if _, err := tx.ExecContext(ctx, step.Down); err != nil {
			return fmt.Errorf("executing migration %d: %w", step.Version, err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+historyTable(p.baseline.Kind)+` WHERE version = ?`, step.Version); err != nil {
			return err
		}
	}
	current, err := readState(ctx, tx, p.baseline.Path, p.baseline.Kind)
	if err != nil {
		return err
	}
	if err := p.baseline.verify(current); err != nil {
		return err
	}
	return tx.Rollback()
}

func (r *Recovery) prepare(ctx context.Context, root *os.Root, request Request, result Result) (Result, error) {
	if !imageID.MatchString(request.PreviousImage) || !imageID.MatchString(request.CandidateImage) {
		return result, fmt.Errorf("prepare requires immutable previous-image and candidate-image IDs (sha256:<64 hex digits>)")
	}
	paths, err := databaseFiles(root)
	if err != nil {
		return result, err
	}
	manifest := Manifest{
		Version: manifestVersion, AttemptID: request.AttemptID,
		PreviousImage: request.PreviousImage, CandidateImage: request.CandidateImage,
		Registries: make(RegistryChecksums), Databases: make([]DatabaseState, 0, len(paths)),
	}
	for kind, reg := range r.registries {
		manifest.Registries[kind] = reg.checksum()
	}
	for _, name := range paths {
		kind := CollectionSchema
		if name == "agent.duckdb" {
			kind = MainSchema
		}
		db, err := openExisting(ctx, root, name, true)
		if err != nil {
			return result, fmt.Errorf("%s: %w", name, err)
		}
		state, readErr := readState(ctx, db, name, kind)
		closeErr := db.Close()
		if err := errors.Join(readErr, closeErr); err != nil {
			return result, fmt.Errorf("%s: %w", name, err)
		}
		if len(state.Versions) == 0 {
			return result, fmt.Errorf("%s: uninitialized database", name)
		}
		if err := r.registries[kind].validateHistory(state.Versions); err != nil {
			return result, fmt.Errorf("%s: %w", name, err)
		}
		manifest.Databases = append(manifest.Databases, state)
		result.Databases = append(result.Databases, DatabaseResult{Path: name, Versions: state.Versions, Steps: []int{}})
	}
	if err := root.MkdirAll("updates/attempts", 0700); err != nil {
		return result, err
	}
	attemptDir := filepath.Join("updates", "attempts", request.AttemptID)
	// An existing attempt, even one with an interrupted preparation, must never
	// replace a baseline that could already have been used to change databases.
	if err := root.Mkdir(attemptDir, 0700); err != nil {
		return result, fmt.Errorf("creating new attempt: %w", err)
	}
	attempt, err := root.OpenRoot(attemptDir)
	if err != nil {
		return result, err
	}
	defer func() { _ = attempt.Close() }()
	file, err := attempt.OpenFile("manifest.tmp", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return result, err
	}
	writeErr := json.NewEncoder(file).Encode(manifest)
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return result, err
	}
	if err := attempt.Rename("manifest.tmp", "manifest.json"); err != nil {
		return result, err
	}
	// Persist the file rename and every newly created parent directory.
	for _, dir := range []string{attemptDir, "updates/attempts", "updates", "."} {
		file, err := root.Open(dir)
		if err != nil {
			return result, err
		}
		err = errors.Join(file.Sync(), file.Close())
		if err != nil {
			return result, err
		}
	}
	return result, nil
}

func (r *Recovery) readManifest(root *os.Root, attemptID string) (Manifest, error) {
	var manifest Manifest
	file, err := root.Open(filepath.Join("updates", "attempts", attemptID, "manifest.json"))
	if err != nil {
		return manifest, fmt.Errorf("opening baseline manifest: %w", err)
	}
	defer func() { _ = file.Close() }()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return manifest, fmt.Errorf("decoding baseline manifest: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return manifest, fmt.Errorf("trailing content in baseline manifest")
	}
	if manifest.Version != manifestVersion || manifest.AttemptID != attemptID ||
		!imageID.MatchString(manifest.PreviousImage) || !imageID.MatchString(manifest.CandidateImage) {
		return manifest, fmt.Errorf("unsupported or mismatched baseline manifest")
	}
	if len(manifest.Registries) != len(r.registries) {
		return manifest, fmt.Errorf("baseline migration registries are missing")
	}
	for kind, reg := range r.registries {
		if manifest.Registries[kind] != reg.checksum() {
			return manifest, fmt.Errorf("%s: candidate migration registry differs from baseline manifest", kind)
		}
	}
	paths, err := databaseFiles(root)
	if err != nil {
		return manifest, err
	}
	if len(paths) != len(manifest.Databases) {
		return manifest, fmt.Errorf("database inventory differs from baseline")
	}
	for i, baseline := range manifest.Databases {
		kind := CollectionSchema
		if paths[i] == "agent.duckdb" {
			kind = MainSchema
		}
		if baseline.Path != paths[i] || baseline.Kind != kind || len(baseline.Versions) == 0 || len(baseline.Columns) == 0 {
			return manifest, fmt.Errorf("invalid baseline database: %s", baseline.Path)
		}
		if err := r.registries[kind].validateHistory(baseline.Versions); err != nil {
			return manifest, err
		}
	}
	return manifest, nil
}

func databaseFiles(root *os.Root) ([]string, error) {
	file, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	entries, readErr := file.ReadDir(-1)
	if err := errors.Join(readErr, file.Close()); err != nil {
		return nil, err
	}
	paths := make([]string, 0)
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, "collection_") && strings.HasSuffix(name, ".duckdb") {
			paths = append(paths, name)
		}
	}
	slices.Sort(paths)
	// Main is last so it remains available until all collections are restored.
	return append(paths, "agent.duckdb"), nil
}

func openExisting(ctx context.Context, root *os.Root, name string, readOnly bool) (*sql.DB, error) {
	if filepath.Base(name) != name || strings.ContainsAny(name, "?\x00") {
		return nil, fmt.Errorf("invalid database filename")
	}
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("database must be a regular file, not a symlink or directory")
	}
	folder, err := filepath.EvalSymlinks(root.Name())
	if err != nil {
		return nil, err
	}
	folder, err = filepath.Abs(folder)
	if err != nil {
		return nil, err
	}
	if strings.ContainsAny(folder, "?\x00") {
		return nil, fmt.Errorf("invalid database directory")
	}
	mode := "READ_WRITE"
	if readOnly {
		mode = "READ_ONLY"
	}
	db, err := sql.Open("duckdb", filepath.Join(folder, name)+"?access_mode="+mode)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func renderDown(step migration, baseline DatabaseState) (string, error) {
	t, err := template.New(step.Name).Option("missingkey=error").Parse(step.Down)
	if err != nil {
		return "", err
	}
	var content bytes.Buffer
	if err := t.Execute(&content, baseline.objects()); err != nil {
		return "", fmt.Errorf("rendering down migration %d: %w", step.Version, err)
	}
	return content.String(), nil
}
