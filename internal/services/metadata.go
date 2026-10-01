package services

import (
	"context"
	"encoding/json"
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

func saveTagCategoryNames(ctx context.Context, db store.QueryInterceptor, names map[string]string) error {
	if names == nil {
		names = map[string]string{}
	}
	data, err := json.Marshal(names)
	if err != nil {
		return fmt.Errorf("encoding tag category names: %w", err)
	}
	_, err = db.ExecContext(ctx, `CREATE OR REPLACE TEMP TABLE tag_categories AS
		SELECT key AS id, value->>'$' AS name FROM json_each(?)`, string(data))
	if err != nil {
		return fmt.Errorf("saving tag category names: %w", err)
	}
	return nil
}

func dropTagCategoryNames(ctx context.Context, db store.QueryInterceptor) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if _, err := db.ExecContext(ctx, "DROP TABLE IF EXISTS temp.main.tag_categories"); err != nil {
		return fmt.Errorf("dropping tag category names: %w", err)
	}
	return nil
}

func tagCategoryNames(ctx context.Context, client *govmomi.Client, credentials models.Credentials) map[string]string {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if client == nil {
		zap.S().Warn("tag category names unavailable: no vCenter client")
		return nil
	}
	rc := rest.NewClient(client.Client)
	if err := rc.Login(ctx, url.UserPassword(credentials.Username, credentials.Password)); err != nil {
		zap.S().Warnw("tag category login failed; retaining category IDs", "error", err)
		return nil
	}
	defer func() {
		logoutCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := rc.Logout(logoutCtx); err != nil {
			zap.S().Warnw("tag category logout failed", "error", err)
		}
	}()
	manager := tags.NewManager(rc)
	ids, err := manager.ListCategories(ctx)
	if err != nil {
		zap.S().Warnw("listing tag categories failed; retaining category IDs", "error", err)
		return nil
	}
	return resolveTagCategories(ctx, ids, func(ctx context.Context, id string) (string, error) {
		category, err := manager.GetCategory(ctx, id)
		if err != nil {
			return "", err
		}
		return category.Name, nil
	})
}

func resolveTagCategories(ctx context.Context, ids []string, resolve func(context.Context, string) (string, error)) map[string]string {
	names := make(map[string]string)
	for _, id := range ids {
		if ctx.Err() != nil {
			break
		}
		name, err := resolve(ctx, id)
		if err == nil && name == "" {
			err = fmt.Errorf("category has an empty name")
		}
		if err != nil {
			zap.S().Warnw("tag category lookup failed; retaining category ID", "category_id", id, "error", err)
			continue
		}
		names[id] = name
	}
	return names
}
