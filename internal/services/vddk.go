package services

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

const (
	vddkFolder              = "vddk"
	vddkLibPath             = "vmware-vix-disklib-distrib/lib64"
	maxInflatedBytes  int64 = 1 << 30 // 1 GiB. real VDDK inflates to ~200 MiB
	maxArchiveEntries       = 1000    // real VDDK archive has ~300 entries
)

var (
	versionRegex    = regexp.MustCompile(`\d+\.\d+\.\d+`)
	libVersionRegex = regexp.MustCompile(`libvixDiskLib\.so\.(\d+\.\d+\.\d+)`)
)

type VddkService struct {
	parentFolder    string
	store           *store.Store
	uploadSemaphore chan struct{}
}

func NewVddkService(parentFolder string, st *store.Store) *VddkService {
	return &VddkService{
		parentFolder:    parentFolder,
		store:           st,
		uploadSemaphore: make(chan struct{}, 1), // allow single concurrent upload
	}
}

func (v *VddkService) Upload(ctx context.Context, filename string, r io.Reader) (*models.VddkStatus, error) {
	if !v.acquireUpload() {
		return nil, srvErrors.NewVddkUploadInProgressError()
	}
	defer v.releaseUpload()

	tmpDir := filepath.Join(v.parentFolder, fmt.Sprintf("%s_%s", vddkFolder, uuid.New()))
	defer func() {
		_ = os.RemoveAll(tmpDir)
	}()

	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating temp dir: %w", err)
	}

	hash := md5.New()
	if err := extractTarGz(io.TeeReader(r, hash), tmpDir); err != nil {
		return nil, fmt.Errorf("extracting vddk: %w", err)
	}

	version, err := v.extractVersion(filename, tmpDir)
	if err != nil {
		return nil, fmt.Errorf("extracting version: %w", err)
	}

	expectedVersion, err := v.store.Parser().VCenterApiVersion(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting expected version from duckdb: %w", err)
	}

	// Take major.minor (x.y) only
	parts := strings.Split(expectedVersion, ".")
	if len(parts) > 2 {
		expectedVersion = strings.Join(parts[:2], ".")
	}

	if !strings.HasPrefix(version, expectedVersion) {
		return nil, srvErrors.NewVddkInvalidVersionError(expectedVersion, version)
	}

	// Replace existing VDDK folder
	destinationPath := filepath.Join(v.parentFolder, vddkFolder)
	_ = os.RemoveAll(destinationPath)
	if err := os.Rename(tmpDir, destinationPath); err != nil {
		return nil, fmt.Errorf("error replacing vddk folder: %w", err)
	}

	status := &models.VddkStatus{
		Version: version,
		Md5:     hex.EncodeToString(hash.Sum(nil)),
	}

	if err := v.store.Vddk().Save(ctx, status); err != nil {
		return nil, fmt.Errorf("error saving vddk status: %w", err)
	}

	return status, nil
}

func (v *VddkService) Status(ctx context.Context) (*models.VddkStatus, error) {
	return v.store.Vddk().Get(ctx)
}

func (v *VddkService) acquireUpload() bool {
	select {
	case v.uploadSemaphore <- struct{}{}:
		return true
	default:
		return false
	}
}

func (v *VddkService) releaseUpload() {
	<-v.uploadSemaphore
}

func (v *VddkService) extractVersion(filename, extractedFolder string) (string, error) {
	// Valid name example: VMware-vix-disklib-8.0.3-23950268.x86_64.tar.gz

	// by filename
	parts := strings.Split(filename, "-")
	for _, part := range parts {
		if versionRegex.MatchString(part) {
			return versionRegex.FindString(part), nil
		}
	}

	// fallback: by extracted content
	entries, err := os.ReadDir(filepath.Join(extractedFolder, vddkLibPath))
	if err != nil {
		return "", fmt.Errorf("cannot read lib64 directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if libVersionRegex.MatchString(entry.Name()) {
			return versionRegex.FindString(entry.Name()), nil
		}
	}

	return "", fmt.Errorf("no version found in filename '%s' or tar content", filename)
}

// extractTarGz extracts all files, directories, hard and symbolic links from a given reader and overrides a specified destination folder
func extractTarGz(r io.Reader, destDir string) error {
	gzr, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("creating gzip reader: %w", err)
	}
	defer func() {
		_ = gzr.Close()
	}()

	tarReader := tar.NewReader(gzr)
	var totalWritten int64
	var entryCount int

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break // end of archive
		}
		if err != nil {
			return err
		}

		entryCount++
		if entryCount > maxArchiveEntries {
			return fmt.Errorf("archive contains too many entries (max %d)", maxArchiveEntries)
		}

		targetPath := filepath.Clean(filepath.Join(destDir, header.Name))

		if !pathInsideDest(destDir, targetPath) {
			return fmt.Errorf("illegal file path: %s", targetPath)
		}

		// ECOPROJECT-4719 | Before creating files/directories, resolve symlinks in the parent path
		// to prevent chained symlink attacks (e.g., a/x -> .., then a/x/evil)
		if header.Typeflag == tar.TypeDir || header.Typeflag == tar.TypeReg || header.Typeflag == tar.TypeSymlink {
			parentDir := filepath.Dir(targetPath)
			if parentDir != destDir {
				resolvedParent, err := filepath.EvalSymlinks(parentDir)
				if err == nil {
					// Resolve symlinks in destDir as well for proper comparison
					// (e.g., on macOS /var is a symlink to /private/var)
					resolvedDest := destDir
					if rd, err := filepath.EvalSymlinks(destDir); err == nil {
						resolvedDest = rd
					}

					// Parent exists - verify resolved path is still strictly inside destDir.
					// If it resolves to destDir itself, that means a symlink has traversed
					// back to the root, which indicates an escape attempt.
					if filepath.Clean(resolvedParent) == filepath.Clean(resolvedDest) {
						return fmt.Errorf("symlink escape detected: %s resolves to %s (root)", parentDir, resolvedParent)
					}
					if !pathInsideDest(resolvedDest, resolvedParent) {
						return fmt.Errorf("symlink escape detected: %s resolves to %s", parentDir, resolvedParent)
					}
				}
			}
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, os.FileMode(header.Mode)&0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			remaining := maxInflatedBytes - totalWritten
			if remaining <= 0 {
				return fmt.Errorf("extracted archive exceeds maximum allowed size (%d bytes)", maxInflatedBytes)
			}
			outFile, err := os.Create(targetPath)
			if err != nil {
				return err
			}
			n, err := io.Copy(outFile, io.LimitReader(tarReader, remaining+1))
			_ = outFile.Close()
			if err != nil {
				return err
			}
			if n > remaining {
				return fmt.Errorf("extracted archive exceeds maximum allowed size (%d bytes)", maxInflatedBytes)
			}
			totalWritten += n
			if err := os.Chmod(targetPath, os.FileMode(header.Mode)&0o755); err != nil {
				return err
			}
		case tar.TypeSymlink:
			symlinkResolvedPath := filepath.Clean(header.Linkname)
			if !filepath.IsAbs(header.Linkname) {
				symlinkResolvedPath = filepath.Clean(filepath.Join(filepath.Dir(targetPath), header.Linkname))
			}

			if !pathInsideDest(destDir, symlinkResolvedPath) {
				return fmt.Errorf("illegal symlink target %q -> %q", targetPath, header.Linkname)
			}

			_ = os.Remove(targetPath)
			if err := os.Symlink(header.Linkname, targetPath); err != nil {
				return fmt.Errorf("symlink %s: %w", targetPath, err)
			}
		case tar.TypeLink:
			existingPath := filepath.Clean(filepath.Join(destDir, header.Linkname))
			if !pathInsideDest(destDir, existingPath) {
				return fmt.Errorf("illegal hard link target path: %s", existingPath)
			}
			_ = os.Remove(targetPath)
			if err := os.Link(existingPath, targetPath); err != nil {
				return fmt.Errorf("hard link %s -> %s: %w", targetPath, existingPath, err)
			}
		}
	}

	return nil
}

func pathInsideDest(destDir, candidate string) bool {
	destClean := filepath.Clean(destDir)
	candClean := filepath.Clean(candidate)
	if candClean == destClean {
		return true
	}
	sep := string(os.PathSeparator)
	return strings.HasPrefix(candClean, destClean+sep)
}
