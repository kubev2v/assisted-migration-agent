package upgrade

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
)

// Validate exercises this image's stores against the source baseline without
// starting application services, loading extensions, or writing persistent data.
// The host must stop every data consumer before invoking it in an offline container.
func Validate(ctx context.Context, folder, attemptID, imageID string) error {
	if !attemptPattern.MatchString(attemptID) || !imagePattern.MatchString(imageID) {
		return fmt.Errorf("valid attempt and immutable image IDs are required")
	}
	root, err := os.OpenRoot(folder)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	var baseline Baseline
	var manifest migrations.Manifest
	for name, target := range map[string]any{
		filepath.Join("updates/validation", attemptID+".json"):        &baseline,
		filepath.Join("updates/attempts", attemptID, "manifest.json"): &manifest,
	} {
		file, err := root.Open(name)
		if err != nil {
			return fmt.Errorf("opening upgrade baseline: %w", err)
		}
		decoder := json.NewDecoder(file)
		decoder.DisallowUnknownFields()
		err = decoder.Decode(target)
		if err == nil && decoder.Decode(new(any)) != io.EOF {
			err = fmt.Errorf("trailing upgrade baseline content")
		}
		closeErr := file.Close()
		if err := errors.Join(err, closeErr); err != nil {
			return fmt.Errorf("reading upgrade baseline: %w", err)
		}
	}
	if baseline.ContractVersion != ContractVersion || baseline.AttemptID != attemptID || manifest.Version != 1 || manifest.AttemptID != attemptID || manifest.PreviousImage != baseline.PreviousImage || manifest.CandidateImage != baseline.CandidateImage || (imageID != manifest.PreviousImage && imageID != manifest.CandidateImage) {
		return fmt.Errorf("upgrade baseline identity mismatch")
	}
	snapshot, err := store.ReadUpgradeSnapshot(ctx, folder)
	if err != nil {
		return err
	}
	inventory := make([]string, 0, len(manifest.Databases))
	for _, database := range manifest.Databases {
		inventory = append(inventory, database.Path)
	}
	slices.Sort(inventory)
	if !slices.Equal(snapshot.Databases, baseline.Snapshot.Databases) || !slices.Equal(snapshot.Databases, inventory) || !maps.Equal(snapshot.Assets, baseline.Snapshot.Assets) {
		return fmt.Errorf("database inventory or retained assets differ from baseline")
	}
	if baseline.Snapshot.Records == nil {
		return fmt.Errorf("baseline record inventory is missing")
	}
	for key, count := range baseline.Snapshot.Records {
		current, exists := snapshot.Records[key]
		if !exists || current < count {
			return fmt.Errorf("saved records differ from baseline: %s", key)
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}
