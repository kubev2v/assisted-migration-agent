package services

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"go.uber.org/zap"
	"gopkg.in/yaml.v2"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/util"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

const (
	vddkFolder = "vddk"
	vddkConfig = "vddkConfig.yaml"
)

var versionRegex = regexp.MustCompile(`\d+\.\d+\.\d+`)

type VddkService struct {
	parentFolder    string
	uploadSemaphore chan struct{}
}

func NewVddkService(parentFolder string) *VddkService {
	return &VddkService{
		parentFolder:    parentFolder,
		uploadSemaphore: make(chan struct{}, 1), // allow single concurrent upload
	}
}

func (v *VddkService) Upload(h *multipart.FileHeader) (*models.VddkStatus, error) {
	if !v.acquireUpload() {
		return nil, srvErrors.NewVddkUploadInProgressError()
	}
	defer v.releaseUpload()

	file, err := h.Open()
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = file.Close()
	}()

	tmpDir := filepath.Join(v.parentFolder, vddkFolder+fmt.Sprintf("_%s", uuid.New()))
	defer func() {
		_ = os.RemoveAll(tmpDir)
	}()

	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return nil, fmt.Errorf("creating temp dir: %w", err)
	}

	hash := md5.New()
	if err := util.ExtractTarGz(io.TeeReader(file, hash), tmpDir); err != nil {
		return nil, fmt.Errorf("extracting vddk: %w", err)
	}

	// Replace existing VDDK folder
	_ = os.RemoveAll(v.DstPath())
	if err := os.Rename(tmpDir, v.DstPath()); err != nil {
		return nil, fmt.Errorf("error replacing vddk folder: %w", err)
	}

	version, err := v.extractVersion(h.Filename)
	if err != nil {
		version = "unknown"
		zap.S().Errorw("error getting Vddk version", "error", err)
	}

	status := &models.VddkStatus{
		Version: version,
		Bytes:   h.Size,
		Md5:     hex.EncodeToString(hash.Sum(nil)),
	}

	if err := v.saveVddkStatus(status); err != nil {
		return nil, fmt.Errorf("error saving vddk status: %w", err)
	}

	return status, nil
}

func (v *VddkService) Status() (*models.VddkStatus, error) {
	if _, err := os.Stat(v.configPath()); err != nil {
		return nil, srvErrors.NewVddkNotFoundError()
	}

	data, err := os.ReadFile(v.configPath())
	if err != nil {
		return nil, fmt.Errorf("error reading vddk config: %w", err)
	}

	var status models.VddkStatus
	if err := yaml.Unmarshal(data, &status); err != nil {
		return nil, fmt.Errorf("error parsing vddk config: %w", err)
	}

	return &status, nil
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

func (v *VddkService) extractVersion(filename string) (string, error) {
	// Valid name example: VMware-vix-disklib-8.0.3-23950268.x86_64.tar.gz

	parts := strings.Split(filename, "-")
	for _, part := range parts {
		if versionRegex.MatchString(part) {
			return versionRegex.FindString(part), nil
		}
	}
	return "", fmt.Errorf("no version found in filename '%s'", filename)
}

func (v *VddkService) saveVddkStatus(s *models.VddkStatus) error {
	data, err := yaml.Marshal(s)
	if err != nil {
		return err
	}

	return os.WriteFile(
		v.configPath(),
		data,
		0644,
	)
}

func (v *VddkService) configPath() string {
	return filepath.Join(v.parentFolder, vddkConfig)
}

func (v *VddkService) DstPath() string {
	return filepath.Join(v.parentFolder, vddkFolder)
}
