package services

import (
	"context"
	"io"
)

var InspectImageUpgradeArchiveForTest = inspectImageUpgradeArchive

var InspectImageUpgradeArchiveForPlatformForTest = inspectImageUpgradeArchiveForPlatform

func ValidateImageUpgradeLayersForTest(ctx context.Context, layers []io.Reader, mediaType string, maxBytes int64, maxEntries int) error {
	budget := imageUpgradeLayerBudget{bytes: maxBytes, entries: maxEntries}
	for _, layer := range layers {
		err := validateImageUpgradeLayer(ctx, layer, mediaType, &budget)
		if err != nil {
			return err
		}
	}
	return nil
}
