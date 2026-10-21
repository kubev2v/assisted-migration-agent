package services

import (
	"context"
	"fmt"
	"strings"

	"github.com/kubev2v/assisted-migration-agent/internal/store"
)

func ingestVAppProducts(ctx context.Context, db store.QueryInterceptor, sqlitePath string) error {
	path := strings.ReplaceAll(sqlitePath, "'", "''")
	if _, err := db.ExecContext(ctx, fmt.Sprintf("ATTACH '%s' AS forklift_source (TYPE sqlite)", path)); err != nil {
		return fmt.Errorf("attaching forklift inventory: %w", err)
	}

	_, ingestErr := db.ExecContext(ctx, `
		INSERT INTO vm_vapp_products (vm_id, products_json)
		SELECT src."ID", CAST(src."VAppProducts" AS VARCHAR)
		FROM forklift_source.vm src
		JOIN vinfo inv ON inv."VM ID" = src."ID"
		ON CONFLICT (vm_id) DO UPDATE SET products_json = EXCLUDED.products_json
	`)
	_, detachErr := db.ExecContext(ctx, "DETACH forklift_source")
	if ingestErr != nil {
		return fmt.Errorf("copying vApp products from forklift inventory: %w", ingestErr)
	}
	if detachErr != nil {
		return fmt.Errorf("detaching forklift inventory: %w", detachErr)
	}

	return nil
}
