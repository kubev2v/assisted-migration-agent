package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	sq "github.com/Masterminds/squirrel"

	pkgstore "github.com/kubev2v/migration-planner/pkg/store"

	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	"github.com/kubev2v/assisted-migration-agent/pkg/crypto"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

func validateUpgradeDatabase(ctx context.Context, db *sql.DB, root *os.Root, name string, snapshot *UpgradeSnapshot) error {
	st := newStore(name, pkgstore.NewQueryInterceptor(db), pkgstore.NewTransactor(db))
	tables := []string{"inventory", "groups", "group_matches", "vm_inspection_status", "vm_inspection_concerns", "rightsizing_reports", "rightsizing_metrics", "rightsizing_vm_utilization", "vinfo"}
	if name == "agent.duckdb" {
		tables = []string{"configuration", "collections", "forecast_runs", "credentials", "vddk"}
	}
	for _, table := range tables {
		query, args, err := sq.Select("COUNT(*)").From(table).ToSql()
		if err != nil {
			return err
		}
		var count int64
		if err := st.Querier().QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
			return fmt.Errorf("saved %s records are unreadable", table)
		}
		snapshot.Records[name+"/"+table] = count
	}

	if name == "agent.duckdb" {
		if err := migrations.ValidateMain(ctx, db); err != nil {
			return err
		}
		if _, err := st.Configuration().Get(ctx); err != nil && !srvErrors.IsResourceNotFoundError(err) {
			return fmt.Errorf("configuration read failed")
		}
		if _, err := st.Collection().List(ctx); err != nil {
			return fmt.Errorf("collection lifecycle read failed")
		}
		if _, err := st.Forecast().ListRuns(ctx, ""); err != nil {
			return fmt.Errorf("forecast read failed")
		}
		ids, err := st.Credentials().List(ctx)
		if err != nil {
			return fmt.Errorf("credential listing failed")
		}
		for _, id := range ids {
			creds, err := st.Credentials().Get(ctx, id)
			if err != nil {
				return fmt.Errorf("credential read failed")
			}
			// Only the device-key-managed credential can be decrypted unattended.
			// Other records require the user's master password and cannot satisfy this gate.
			if id != "credentials" {
				return fmt.Errorf("credential requires interactive validation")
			}
			key, err := root.ReadFile("credentials.key")
			if err != nil || len(key) != 32 {
				return fmt.Errorf("credential key is missing or invalid")
			}
			if _, err := crypto.NewCrypto().Decrypt(key, creds); err != nil {
				return fmt.Errorf("credential decryption failed")
			}
		}
		return nil
	}
	if err := migrations.ValidateCollection(ctx, db, strings.TrimSuffix(name, ".duckdb")); err != nil {
		return err
	}
	inv, err := st.Inventory().Get(ctx)
	if err != nil && !srvErrors.IsResourceNotFoundError(err) {
		return fmt.Errorf("inventory read failed")
	}
	if inv != nil && !json.Valid(inv.Data) {
		return fmt.Errorf("inventory JSON is invalid")
	}
	if _, err := st.Group().List(ctx, nil, 1, 0); err != nil {
		return fmt.Errorf("group read failed")
	}
	labels, err := st.VM().ListLabels(ctx)
	if err != nil {
		return fmt.Errorf("VM annotation read failed")
	}
	var labelCount int64
	for _, values := range labels {
		labelCount += int64(len(values))
	}
	snapshot.Records[name+"/vm_labels"] = labelCount
	vms, err := st.VM().List(ctx, nil, WithLimit(1))
	if err != nil {
		return fmt.Errorf("VM store read failed")
	}
	if len(vms) > 0 {
		if _, err := st.VM().GetMigrationExcludedStates(ctx, []string{vms[0].ID}); err != nil {
			return fmt.Errorf("VM exclusions read failed")
		}
		if _, err := st.Inspection().ListResults(ctx, vms[0].ID); err != nil {
			return fmt.Errorf("inspection record read failed")
		}
	}
	if _, err := st.Inspection().ListResults(ctx, ""); err != nil {
		return fmt.Errorf("inspection read failed")
	}
	reports, err := st.RightSizing().ListReports(ctx)
	if err != nil {
		return fmt.Errorf("rightsizing read failed")
	}
	if len(reports) > 0 {
		if _, err := st.RightSizing().GetReport(ctx, reports[0].ID); err != nil {
			return fmt.Errorf("rightsizing metrics read failed")
		}
	}
	return nil
}
