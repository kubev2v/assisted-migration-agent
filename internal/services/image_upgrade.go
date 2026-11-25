package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"golang.org/x/sys/unix"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

const MaxImageUpgradeImageSize int64 = 3 << 30

type ImageUpgradeService struct {
	dataFolder string
	version    string
	mode       func(context.Context) (models.AgentMode, error)
	sem        chan struct{}
	mu         sync.Mutex
	state      string
	lastError  *string
	freeSpace  func(string) (uint64, error)
	ctx        context.Context
	cancel     context.CancelFunc
}

func NewImageUpgradeService(dataFolder, version string, mode func(context.Context) (models.AgentMode, error)) (*ImageUpgradeService, error) {
	ctx, cancel := context.WithCancel(context.Background())
	s := &ImageUpgradeService{dataFolder: dataFolder, version: version, mode: mode,
		sem: make(chan struct{}, 1), state: "idle", ctx: ctx, cancel: cancel,
		freeSpace: func(path string) (uint64, error) {
			var stat unix.Statfs_t
			err := unix.Statfs(path, &stat)
			if err != nil {
				return 0, err
			}
			return stat.Bavail * uint64(stat.Bsize), nil
		},
	}
	staging := filepath.Join(dataFolder, "updates", ".staging")
	entries, err := os.ReadDir(staging)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		cancel()
		return nil, err
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".part") {
			continue
		}
		err = os.Remove(filepath.Join(staging, entry.Name()))
		if err != nil {
			cancel()
			return nil, fmt.Errorf("cleaning partial upload: %w", err)
		}
	}
	return s, nil
}

func (s *ImageUpgradeService) Upload(ctx context.Context, r io.Reader) (image models.ImageUpgradeImage, err error) {
	err = s.ctx.Err()
	if err != nil {
		return image, err
	}
	// Serialize uploads only; host upgrade execution is independent of this service.
	select {
	case s.sem <- struct{}{}:
	default:
		return image, srvErrors.NewImageUpgradeInProgressError()
	}
	defer func() { <-s.sem }()
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.ctx, cancel)
	defer func() { stop(); cancel() }()
	mode, err := s.mode(ctx)
	if err != nil {
		return image, err
	}
	if mode != models.AgentModeDisconnected {
		return image, srvErrors.NewModeConflictError("image upload requires disconnected mode")
	}
	err = ctx.Err()
	if err != nil {
		return image, err
	}

	updates := filepath.Join(s.dataFolder, "updates")
	// The host retains published archives until their upgrades are resolved.
	entries, err := os.ReadDir(updates)
	if err != nil && !os.IsNotExist(err) {
		return image, fmt.Errorf("checking pending image upgrade: %w", err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tar") {
			return image, srvErrors.NewImageUpgradeInProgressError()
		}
	}

	s.mu.Lock()
	s.state, s.lastError = "uploading", nil
	s.mu.Unlock()
	defer func() {
		if errors.Is(err, syscall.ENOSPC) {
			err = srvErrors.NewImageUpgradeInsufficientStorageError()
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		s.state = "idle"
		if err != nil {
			message := err.Error()
			s.lastError = &message
		}
	}()
	staging := filepath.Join(updates, ".staging")
	err = os.MkdirAll(staging, 0o750)
	if err != nil {
		return image, err
	}
	available, err := s.freeSpace(s.dataFolder)
	if err != nil {
		return image, err
	}
	id := uuid.NewString()
	part := filepath.Join(staging, id+".part")
	f, err := os.OpenFile(part, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return image, err
	}
	defer func() { _ = f.Close(); _ = os.Remove(part) }()
	w := &imageUpgradeStorageWriter{file: f, service: s, available: available}
	_, err = io.Copy(w, imageUpgradeContextReader{ctx: ctx, r: io.LimitReader(r, MaxImageUpgradeImageSize+1)})
	if err != nil {
		return image, err
	}
	err = f.Sync()
	if err != nil {
		return image, err
	}
	err = f.Close()
	if err != nil {
		return image, err
	}
	s.mu.Lock()
	s.state = "validating"
	s.mu.Unlock()
	image, err = inspectImageUpgradeArchive(ctx, part, s.version)
	if err != nil {
		return image, err
	}
	err = ctx.Err()
	if err != nil {
		return image, err
	}
	target := filepath.Join(updates, id+".tar")
	_, err = os.Lstat(target)
	if err == nil {
		return image, srvErrors.NewImageUpgradeInProgressError()
	}
	if !os.IsNotExist(err) {
		return image, err
	}
	err = os.Rename(part, target)
	if err != nil {
		return image, fmt.Errorf("publishing upgrade image: %w", err)
	}
	// Publication cannot be undone: the host may consume the archive immediately.
	for _, dir := range []string{updates, staging} {
		err = syncImageUpgradeDirectory(dir)
		if err != nil {
			zap.S().Errorw("syncing uploaded image", "directory", dir, "error", err)
		}
	}
	return image, nil
}

func (s *ImageUpgradeService) Status(ctx context.Context) (models.ImageUpgradeStatus, error) {
	err := ctx.Err()
	if err != nil {
		return models.ImageUpgradeStatus{}, err
	}
	s.mu.Lock()
	status := models.ImageUpgradeStatus{CurrentVersion: s.version, Upload: models.ImageUploadStatus{State: s.state}}
	if s.lastError != nil {
		message := *s.lastError
		status.Upload.LastError = &message
	}
	s.mu.Unlock()
	f, err := os.Open(filepath.Join(s.dataFolder, "updates", "status.json"))
	if os.IsNotExist(err) {
		return status, nil
	}
	if err != nil {
		return status, fmt.Errorf("reading host image upgrade result: %w", err)
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxImageUpgradeMetadata+1))
	if err != nil {
		return status, err
	}
	if len(data) > maxImageUpgradeMetadata {
		return status, fmt.Errorf("host image upgrade result exceeds size limit")
	}
	var result models.ImageUpgradeResult
	err = json.Unmarshal(data, &result)
	if err != nil {
		return status, fmt.Errorf("reading host image upgrade result: %w", err)
	}
	if result.File == "" || result.Time.IsZero() {
		return status, fmt.Errorf("invalid host image upgrade result: missing file or time")
	}
	switch result.Result {
	case "applied", "rolled-back", "failed", "unchanged":
	default:
		return status, fmt.Errorf("invalid host image upgrade result: unknown result")
	}
	status.Upgrade = &result
	return status, nil
}

func (s *ImageUpgradeService) Stop() {
	s.cancel()
	s.sem <- struct{}{}
	<-s.sem
}

type imageUpgradeStorageWriter struct {
	file      io.Writer
	service   *ImageUpgradeService
	available uint64
	written   int64
}

func (w *imageUpgradeStorageWriter) Write(p []byte) (int, error) {
	next := w.written + int64(len(p))
	if next > MaxImageUpgradeImageSize {
		return 0, srvErrors.NewImageUpgradePayloadTooLargeError()
	}
	if uint64(next) > w.available/3 {
		return 0, srvErrors.NewImageUpgradeInsufficientStorageError()
	}
	free, err := w.service.freeSpace(w.service.dataFolder)
	if err != nil {
		return 0, err
	}
	if free < uint64(len(p))+2*uint64(next) {
		return 0, srvErrors.NewImageUpgradeInsufficientStorageError()
	}
	n, err := w.file.Write(p)
	w.written += int64(n)
	return n, err
}

func syncImageUpgradeDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()
	return dir.Sync()
}
