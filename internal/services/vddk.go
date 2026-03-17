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
	vddkFolder = "vddk"
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
		return nil, fmt.Errorf("vddk filename does not match the expected format: "+
			"VMware-vix-disklib-X.Y.Z-*.tar.gz (got: %s)", filename)
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
	entries, err := os.ReadDir(filepath.Join(extractedFolder, "vmware-vix-disklib-distrib", "lib64"))
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

// extractTarGz extracts all files and directories from a given reader and overrides a specified destination folder
func extractTarGz(r io.Reader, destDir string) error {
	gzr, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("creating gzip reader: %w", err)
	}
	defer func() {
		_ = gzr.Close()
	}()

	tarReader := tar.NewReader(gzr)

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break // end of archive
		}
		if err != nil {
			return err
		}

		targetPath := filepath.Clean(filepath.Join(destDir, header.Name))
		// Ensure the target path is inside destDir
		if !strings.HasPrefix(targetPath, filepath.Clean(destDir)+string(os.PathSeparator)) &&
			targetPath != filepath.Clean(destDir) {
			return fmt.Errorf("illegal file path: %s", targetPath)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			// create directory
			if err := os.MkdirAll(targetPath, os.FileMode(header.Mode)); err != nil {
				return err
			}
		case tar.TypeReg:
			// create file
			outFile, err := os.Create(targetPath)
			if err != nil {
				return err
			}
			if _, err := io.Copy(outFile, tarReader); err != nil {
				_ = outFile.Close()
				return err
			}
			_ = outFile.Close()
			if err := os.Chmod(targetPath, os.FileMode(header.Mode)); err != nil {
				return err
			}
		}
	}

	return nil
}
