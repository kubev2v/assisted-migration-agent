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
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"golang.org/x/sys/unix"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/pkg/console"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

const MaxImageUpgradeImageSize int64 = 3 << 30

// imageUpgradeDownloadTimeout bounds a stalled download so it cannot hold the upgrade lock forever.
const imageUpgradeDownloadTimeout = 2 * time.Hour

type ImageUpgradeService struct {
	dataFolder string
	version    string
	mode       func(context.Context) (models.AgentMode, error)
	console    *console.Client
	sourceID   uuid.UUID
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

// WithConsole enables connected-mode upgrades through the planner's agent API.
func (s *ImageUpgradeService) WithConsole(client *console.Client, sourceID uuid.UUID) {
	s.console, s.sourceID = client, sourceID
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
	err = s.checkPending()
	if err != nil {
		return image, err
	}
	s.setState("uploading")
	return s.stage(ctx, r)
}

// Latest asks the console whether a newer appliance image is available. It is not cached.
func (s *ImageUpgradeService) Latest(ctx context.Context) (models.ImageUpgradeLatest, error) {
	latest, _, err := s.latest(ctx)
	return latest, err
}

// StartDownload checks the console for an in-place upgrade and, when one is available,
// downloads and stages it in the background. Progress is reported by Status.
func (s *ImageUpgradeService) StartDownload(ctx context.Context) error {
	err := s.ctx.Err()
	if err != nil {
		return err
	}
	select {
	case s.sem <- struct{}{}:
	default:
		return srvErrors.NewImageUpgradeInProgressError()
	}
	started := false
	defer func() {
		if !started {
			<-s.sem
		}
	}()
	err = s.checkPending()
	if err != nil {
		return err
	}
	// Ask again even if the UI just checked: the presigned URL expires.
	latest, imageURL, err := s.latest(ctx)
	if err != nil {
		return err
	}
	if !latest.UpdateAvailable {
		return srvErrors.NewNoImageUpgradeError("no image upgrade available")
	}
	if !latest.Supported {
		reason := "in-place image upgrade not supported"
		if latest.Reason != nil {
			reason += ": " + *latest.Reason
		}
		return srvErrors.NewNoImageUpgradeError(reason)
	}
	if imageURL == nil {
		return srvErrors.NewImageUpgradeConsoleError(errors.New("image is not yet available"))
	}
	// The download outlives the request, so it is bound to the service lifetime instead.
	downloadCtx, cancel := context.WithTimeout(s.ctx, imageUpgradeDownloadTimeout)
	body, err := s.console.DownloadImage(downloadCtx, *imageURL)
	if err != nil {
		cancel()
		return srvErrors.NewImageUpgradeConsoleError(err)
	}
	s.setState("downloading")
	started = true
	go func() {
		defer func() { <-s.sem }()
		defer cancel()
		defer func() { _ = body.Close() }()
		_, err := s.stage(downloadCtx, body)
		if err != nil {
			zap.S().Named("image_upgrade").Errorw("staging downloaded image", "error", err)
		}
	}()
	return nil
}

func (s *ImageUpgradeService) latest(ctx context.Context) (models.ImageUpgradeLatest, *string, error) {
	mode, err := s.mode(ctx)
	if err != nil {
		return models.ImageUpgradeLatest{}, nil, err
	}
	if mode != models.AgentModeConnected {
		return models.ImageUpgradeLatest{}, nil, srvErrors.NewModeConflictError("image upgrade check requires connected mode")
	}
	status, err := s.console.GetImageUpgradeStatus(ctx, s.sourceID)
	if err != nil {
		return models.ImageUpgradeLatest{}, nil, srvErrors.NewImageUpgradeConsoleError(err)
	}
	latest := models.ImageUpgradeLatest{
		UpdateAvailable: status.UpdateAvailable != nil && *status.UpdateAvailable,
		LatestVersion:   status.LatestVersion,
		Supported:       true,
	}
	// The console only describes the image when an update is available.
	image := status.ApplianceImage
	if image == nil {
		return latest, nil, nil
	}
	if image.Supported != nil {
		latest.Supported = *image.Supported
	}
	latest.Reason = image.Reason
	return latest, image.ImageUrl, nil
}

// checkPending rejects new images while the host still holds a published archive.
func (s *ImageUpgradeService) checkPending() error {
	entries, err := os.ReadDir(filepath.Join(s.dataFolder, "updates"))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("checking pending image upgrade: %w", err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tar") {
			return srvErrors.NewImageUpgradeInProgressError()
		}
	}
	return nil
}

func (s *ImageUpgradeService) setState(state string) {
	s.mu.Lock()
	s.state, s.lastError = state, nil
	s.mu.Unlock()
}

// stage writes r to staging, validates it and publishes it for the host.
// The caller holds sem and has set the activity state.
func (s *ImageUpgradeService) stage(ctx context.Context, r io.Reader) (image models.ImageUpgradeImage, err error) {
	updates := filepath.Join(s.dataFolder, "updates")
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
