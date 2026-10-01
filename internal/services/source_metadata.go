package services

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/vapi/rest"
	"github.com/vmware/govmomi/vapi/tags"
	"go.uber.org/zap"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
)

// enrichSourceMetadata resolves category names, retaining IDs on lookup failure.
func enrichSourceMetadata(ctx context.Context, db store.QueryInterceptor, client *govmomi.Client, credentials models.Credentials) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	ids, err := tagCategoryIDs(ctx, db)
	if err != nil || len(ids) == 0 {
		if err != nil {
			zap.S().Warnw("reading tag categories failed", "error", err)
		}
		return
	}
	if client == nil {
		zap.S().Warn("tag category names unavailable: no vCenter client")
		return
	}
	rc := rest.NewClient(client.Client)
	if err := rc.Login(ctx, url.UserPassword(credentials.Username, credentials.Password)); err != nil {
		zap.S().Warnw("tag category login failed; retaining category IDs", "error", err)
		return
	}
	defer func() {
		logoutCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := rc.Logout(logoutCtx); err != nil {
			zap.S().Warnw("tag category logout failed", "error", err)
		}
	}()
	manager := tags.NewManager(rc)
	err = resolveTagCategories(ctx, db, ids, func(ctx context.Context, id string) (string, error) {
		category, err := manager.GetCategory(ctx, id)
		if err != nil {
			return "", err
		}
		return category.Name, nil
	})
	if err != nil {
		zap.S().Warnw("tag category enrichment failed; retaining unresolved category IDs", "error", err)
	}
}

func tagCategoryIDs(ctx context.Context, db store.QueryInterceptor) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT DISTINCT e.value->>'key'
		FROM vinfo, json_each(source_metadata) e
		WHERE (e.value->>'kind') = 'tag' AND COALESCE(e.value->>'key', '') != ''`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func resolveTagCategories(ctx context.Context, db store.QueryInterceptor, ids []string, resolve func(context.Context, string) (string, error)) error {
	for _, id := range ids {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		name, err := resolve(ctx, id)
		if err == nil && name == "" {
			err = fmt.Errorf("category has an empty name")
		}
		if err != nil {
			zap.S().Warnw("tag category lookup failed; retaining category ID", "category_id", id, "error", err)
			continue
		}
		_, err = db.ExecContext(ctx, `UPDATE vinfo SET source_metadata = (
			SELECT json_group_array(CASE WHEN (e.value->>'kind') = 'tag' AND (e.value->>'key') = ?
				THEN json_merge_patch(e.value, json_object('key', ?)) ELSE e.value END)
			FROM json_each(source_metadata) e)
			WHERE EXISTS (SELECT 1 FROM json_each(source_metadata) e
				WHERE (e.value->>'kind') = 'tag' AND (e.value->>'key') = ?)`, id, name, id)
		if err != nil {
			return fmt.Errorf("saving tag category %s: %w", id, err)
		}
	}
	return nil
}
