package services

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/vapi/rest"
	"github.com/vmware/govmomi/vapi/tags"
	"go.uber.org/zap"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
)

func tagCategoryNames(ctx context.Context, client *govmomi.Client, credentials models.Credentials) map[string]string {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if client == nil {
		zap.S().Warn("tag category names unavailable: no vCenter client; tags without category names will be skipped")
		return nil
	}
	rc := rest.NewClient(client.Client)
	if err := rc.Login(ctx, url.UserPassword(credentials.Username, credentials.Password)); err != nil {
		zap.S().Warnw("tag category login failed; tags without category names will be skipped", "error", err)
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
		zap.S().Warnw("listing tag categories failed; tags without category names will be skipped", "error", err)
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
		if err == nil && strings.TrimSpace(name) == "" {
			err = fmt.Errorf("category has an empty name")
		}
		if err != nil {
			zap.S().Warnw("tag category lookup failed; tags in this category will be skipped", "category_id", id, "error", err)
			continue
		}
		names[id] = name
	}
	return names
}
