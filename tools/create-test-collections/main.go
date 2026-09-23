// create-test-collections creates DuckDB collection files from a YAML scenario
// for manual and QE testing of the collection comparison API.
//
// Usage: (from the create-test-collections directory)
//
//	go run . --data-folder=. testdata/sample-scenario.yaml
package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"

	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
)

// ── DSL types ─────────────────────────────────────────────────────────────────

type Scenario struct {
	Collections []Collection `yaml:"collections"`
}

type Collection struct {
	Label     string `yaml:"label"`
	Timestamp int64  `yaml:"timestamp"` // unix seconds → filename + createdAt
	VMs       []VM   `yaml:"vms"`
}

type VM struct {
	ID         string   `yaml:"id"`
	Cluster    string   `yaml:"cluster"`
	Migratable *bool    `yaml:"migratable"` // nil = true (default)
	Excluded   bool     `yaml:"excluded"`
	Labels     []string `yaml:"labels"`
}

func (v VM) isMigratable() bool {
	return v.Migratable == nil || *v.Migratable
}

// ── main ──────────────────────────────────────────────────────────────────────

func main() {
	dataFolder := flag.String("data-folder", ".", "directory to write collection files (default: current directory)")
	flag.Parse()

	if *dataFolder == "" {
		*dataFolder = "."
	}
	if flag.NArg() != 1 {
		log.Fatal("usage: create-test-collections --data-folder <dir> <scenario.yaml>")
	}

	raw, err := os.ReadFile(flag.Arg(0))
	if err != nil {
		log.Fatalf("reading scenario file: %v", err)
	}

	var scenario Scenario
	if err := yaml.Unmarshal(raw, &scenario); err != nil {
		log.Fatalf("parsing scenario YAML: %v", err)
	}

	if err := os.MkdirAll(*dataFolder, 0o755); err != nil {
		log.Fatalf("creating data folder: %v", err)
	}

	fmt.Printf("Creating %d collection(s) in %s\n\n", len(scenario.Collections), *dataFolder)

	for _, col := range scenario.Collections {
		id, err := createCollection(*dataFolder, col)
		if err != nil {
			log.Fatalf("creating collection %q: %v", col.Label, err)
		}
		ts := time.Unix(col.Timestamp, 0).UTC()
		fmt.Printf("  %-20s  id=%-8s  createdAt=%s  vms=%d\n",
			col.Label, id, ts.Format(time.RFC3339), len(col.VMs))
	}

	fmt.Println()
	fmt.Println("Done. Start the agent pointing at the data folder, then run:")
	fmt.Println("  curl -s http://localhost:8000/api/v2/collections | jq .")
	fmt.Println("  curl -s http://localhost:8000/api/v2/collections/compare/<A>/<B> | jq .")
}

// ── collection creation ────────────────────────────────────────────────────────

func createCollection(dataFolder string, col Collection) (string, error) {
	filename := fmt.Sprintf("collection_%s.duckdb", strconv.FormatInt(col.Timestamp, 10))
	path := filepath.Join(dataFolder, filename)
	name := fmt.Sprintf("collection_%s", strconv.FormatInt(col.Timestamp, 10))

	// Derive the same ID the agent would assign (SHA256 of full path, first 6 hex chars).
	hash := sha256.Sum256([]byte(path))
	id := hex.EncodeToString(hash[:])[:6]

	pool := store.NewPool(5 * time.Minute)
	db, err := pool.NewDatabase(id, path, time.Unix(col.Timestamp, 0),
		store.EagerConnectionInitilization, 256, store.ReadWriteDatabase)
	if err != nil {
		return "", fmt.Errorf("opening database: %w", err)
	}

	st, err := db.Store()
	if err != nil {
		return "", fmt.Errorf("getting store: %w", err)
	}

	if err := duckdb_parser.New(st.Querier(), nil).Init(); err != nil {
		return "", fmt.Errorf("initialising schema: %w", err)
	}

	ctx := context.Background()
	if err := db.Migrate(ctx, func(ctx context.Context, sqlDB *sql.DB) error {
		return migrations.RunCollection(ctx, sqlDB, name)
	}); err != nil {
		return "", fmt.Errorf("running migrations: %w", err)
	}

	for i, vm := range col.VMs {
		if err := insertVM(ctx, st, vm, i); err != nil {
			return "", fmt.Errorf("inserting VM %q: %w", vm.ID, err)
		}
	}

	return id, nil
}

func insertVM(ctx context.Context, st *store.Store, vm VM, idx int) error {
	labelsJSON := "[]"
	if len(vm.Labels) > 0 {
		b, _ := json.Marshal(vm.Labels)
		labelsJSON = string(b)
	}

	_, err := st.Querier().ExecContext(ctx, `
		INSERT INTO vinfo (
			"VM ID", "VM", "Powerstate", "Connection state",
			"Cluster", "Datacenter", "Host", "Folder ID",
			"Firmware", "SMBIOS UUID", "Memory", "CPUs",
			"Template", "migration_excluded", "labels"
		) VALUES (?, ?, 'poweredOn', 'connected', ?, 'DC1', 'esxi-01.local', '/vms',
		          'bios', ?, 4096, 2, false, ?, ?)
	`, vm.ID, vm.ID, vm.Cluster, fmt.Sprintf("uuid-%d", idx), vm.Excluded, labelsJSON)
	if err != nil {
		return fmt.Errorf("inserting vinfo row: %w", err)
	}

	if !vm.isMigratable() {
		_, err = st.Querier().ExecContext(ctx, `
			INSERT INTO concerns ("VM_ID", "Concern_ID", "Label", "Category", "Assessment")
			VALUES (?, ?, 'Non-migratable', 'Critical', 'This VM cannot be migrated as configured')
		`, vm.ID, "concern-"+vm.ID)
		if err != nil {
			return fmt.Errorf("inserting concern row: %w", err)
		}
	}

	return nil
}
