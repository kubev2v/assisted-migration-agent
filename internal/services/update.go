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

const MaxUpdateImageSize int64 = 3 << 30

type preparedUpdate struct {
	models.PreparedImage
	File string `json:"file"`
	Size int64  `json:"size"`
}

type UpdateService struct {
	dataFolder string
	version    string
	mode       func(context.Context) (models.AgentMode, error)
	busy       func() bool
	sem        chan struct{}
	mu         sync.Mutex
	state      string
	prepared   *preparedUpdate
	lastError  *string
	freeSpace  func(string) (uint64, error)
	ctx        context.Context
	cancel     context.CancelFunc
}

func NewUpdateService(dataFolder, version string, mode func(context.Context) (models.AgentMode, error), busy func() bool) (*UpdateService, error) {
	ctx, cancel := context.WithCancel(context.Background())
	s := &UpdateService{dataFolder: dataFolder, version: version, mode: mode, busy: busy,
		sem: make(chan struct{}, 1), state: "idle", ctx: ctx, cancel: cancel,
		freeSpace: func(path string) (uint64, error) {
			var stat unix.Statfs_t
			if err := unix.Statfs(path, &stat); err != nil {
				return 0, err
			}
			return stat.Bavail * uint64(stat.Bsize), nil
		},
	}
	if err := s.recoverPrepared(); err != nil {
		cancel()
		return nil, err
	}
	return s, nil
}

func (s *UpdateService) Prepare(ctx context.Context, r io.Reader, size int64, expectedDigest string) (image models.PreparedImage, err error) {
	if err := s.reserve(); err != nil {
		return image, err
	}
	defer s.release()
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
	if s.busy() {
		return image, srvErrors.NewCollectionInProgressError()
	}
	if err := ctx.Err(); err != nil {
		return image, err
	}
	s.mu.Lock()
	s.state, s.lastError = "uploading", nil
	s.mu.Unlock()
	defer func() {
		if errors.Is(err, syscall.ENOSPC) {
			err = srvErrors.NewInsufficientStorageError()
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		s.state = "idle"
		if s.prepared != nil {
			s.state = "ready"
		}
		if err != nil {
			message := err.Error()
			s.lastError = &message
		}
	}()
	if size > MaxUpdateImageSize {
		return image, srvErrors.NewUpdatePayloadTooLargeError()
	}
	staging := filepath.Join(s.dataFolder, "updates", ".staging")
	if err := os.MkdirAll(staging, 0o750); err != nil {
		return image, err
	}
	available, err := s.freeSpace(s.dataFolder)
	if err != nil {
		return image, err
	}
	if size > 0 && uint64(size) > available/3 {
		return image, srvErrors.NewInsufficientStorageError()
	}
	id := uuid.NewString()
	part := filepath.Join(staging, id+".part")
	candidate := filepath.Join(staging, id+".tar")
	f, err := os.OpenFile(part, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return image, err
	}
	committed := false
	defer func() {
		_ = f.Close()
		_ = os.Remove(part)
		if !committed {
			_ = os.Remove(candidate)
		}
	}()
	w := &updateStorageWriter{file: f, service: s, available: available}
	n, err := io.Copy(w, updateContextReader{ctx: ctx, r: io.LimitReader(r, MaxUpdateImageSize+1)})
	if err != nil {
		return image, err
	}
	if size >= 0 && n != size {
		return image, srvErrors.NewInvalidUpdateImageError("archive size does not match declared size")
	}
	if err := f.Sync(); err != nil {
		return image, err
	}
	if err := f.Close(); err != nil {
		return image, err
	}
	s.mu.Lock()
	s.state = "validating"
	s.mu.Unlock()
	image, err = inspectUpdateArchive(ctx, part, s.version, expectedDigest)
	if err != nil {
		return image, err
	}
	if err := ctx.Err(); err != nil {
		return image, err
	}
	if err := os.Rename(part, candidate); err != nil {
		return image, err
	}
	prepared := &preparedUpdate{PreparedImage: image, File: id + ".tar", Size: n}
	metadata, err := json.Marshal(prepared)
	if err != nil {
		return image, err
	}
	tmp, err := os.CreateTemp(staging, "prepared-*.part")
	if err != nil {
		return image, err
	}
	defer func() { _ = tmp.Close(); _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(metadata); err != nil {
		return image, err
	}
	if err := tmp.Sync(); err != nil {
		return image, err
	}
	if err := tmp.Close(); err != nil {
		return image, err
	}
	// Sync the archive rename before publishing the metadata that references it.
	if err := syncUpdateDirectory(staging); err != nil {
		return image, err
	}
	if err := os.Rename(tmp.Name(), filepath.Join(staging, "prepared.json")); err != nil {
		return image, err
	}
	// Once metadata is published, retain the referenced archive even if directory sync fails.
	committed = true
	s.mu.Lock()
	previous := s.prepared
	s.prepared = prepared
	s.mu.Unlock()
	if err := syncUpdateDirectory(staging); err != nil {
		zap.S().Errorw("syncing prepared update metadata", "error", err)
		return image, err
	}
	if previous != nil {
		if err := os.Remove(filepath.Join(staging, previous.File)); err != nil && !os.IsNotExist(err) {
			zap.S().Warnw("removing obsolete prepared update", "error", err)
		}
	}
	return image, nil
}

func (s *UpdateService) Status(ctx context.Context) (models.UpdateStatus, error) {
	mode, err := s.mode(ctx)
	if err != nil {
		return models.UpdateStatus{}, err
	}
	s.mu.Lock()
	status := models.UpdateStatus{CurrentVersion: s.version, Connected: mode == models.AgentModeConnected, State: s.state}
	if s.lastError != nil {
		message := *s.lastError
		status.LastError = &message
	}
	if s.prepared != nil {
		image := s.prepared.PreparedImage
		status.PreparedImage = &image
	}
	s.mu.Unlock()
	if status.Connected {
		disabled := false
		status.AutoUpdate = &disabled
	}
	var result models.UpdateResult
	err = readUpdateJSON(filepath.Join(s.dataFolder, "updates", "status.json"), &result)
	if os.IsNotExist(err) {
		return status, nil
	}
	if err != nil {
		return status, fmt.Errorf("reading host update result: %w", err)
	}
	if result.File == "" || result.Time.IsZero() {
		return status, fmt.Errorf("invalid host update result: missing file or time")
	}
	switch result.Result {
	case "applied", "rolled-back", "failed", "unchanged":
	default:
		return status, fmt.Errorf("invalid host update result: unknown result")
	}
	status.LastResult = &result
	return status, nil
}

func (s *UpdateService) Stop() {
	s.cancel()
	s.sem <- struct{}{}
	s.release()
}

func (s *UpdateService) reserve() error {
	if err := s.ctx.Err(); err != nil {
		return err
	}
	select {
	case s.sem <- struct{}{}:
		return nil
	default:
		return srvErrors.NewUpdateInProgressError()
	}
}

func (s *UpdateService) release() { <-s.sem }

func (s *UpdateService) recoverPrepared() error {
	staging := filepath.Join(s.dataFolder, "updates", ".staging")
	var prepared preparedUpdate
	err := readUpdateJSON(filepath.Join(staging, "prepared.json"), &prepared)
	if err == nil {
		id, parseErr := uuid.Parse(strings.TrimSuffix(prepared.File, ".tar"))
		if parseErr != nil || prepared.File != id.String()+".tar" || prepared.Size < 0 || prepared.Size > MaxUpdateImageSize {
			err = fmt.Errorf("invalid prepared update metadata")
		} else {
			var info os.FileInfo
			info, err = os.Lstat(filepath.Join(staging, prepared.File))
			if err == nil && (!info.Mode().IsRegular() || info.Size() != prepared.Size) {
				err = fmt.Errorf("invalid prepared archive file")
			}
			if err == nil {
				var image models.PreparedImage
				image, err = inspectUpdateArchive(s.ctx, filepath.Join(staging, prepared.File), s.version, prepared.Digest)
				if err == nil && image != prepared.PreparedImage {
					err = fmt.Errorf("prepared image metadata mismatch")
				}
			}
		}
		if err == nil {
			s.prepared, s.state = &prepared, "ready"
		}
		// A missing referenced archive is a recovery error, not an absent metadata file.
		if err != nil {
			message := "recovering prepared update: " + err.Error()
			s.lastError = &message
		}
	} else if !os.IsNotExist(err) {
		message := "recovering prepared update: " + err.Error()
		s.lastError = &message
	}
	entries, err := os.ReadDir(staging)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if s.prepared != nil && name == s.prepared.File {
			continue
		}
		if strings.HasSuffix(name, ".part") || strings.HasSuffix(name, ".tar") || (name == "prepared.json" && s.prepared == nil) {
			if err := os.Remove(filepath.Join(staging, name)); err != nil {
				return fmt.Errorf("cleaning update staging: %w", err)
			}
		}
	}
	return nil
}

type updateStorageWriter struct {
	file      io.Writer
	service   *UpdateService
	available uint64
	written   int64
}

func (w *updateStorageWriter) Write(p []byte) (int, error) {
	next := w.written + int64(len(p))
	if next > MaxUpdateImageSize {
		return 0, srvErrors.NewUpdatePayloadTooLargeError()
	}
	if uint64(next) > w.available/3 {
		return 0, srvErrors.NewInsufficientStorageError()
	}
	free, err := w.service.freeSpace(w.service.dataFolder)
	if err != nil {
		return 0, err
	}
	if free < uint64(len(p))+2*uint64(next) {
		return 0, srvErrors.NewInsufficientStorageError()
	}
	n, err := w.file.Write(p)
	w.written += int64(n)
	return n, err
}

func readUpdateJSON(filename string, target any) error {
	f, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxUpdateMetadata+1))
	if err != nil {
		return err
	}
	if len(data) > maxUpdateMetadata {
		return fmt.Errorf("update metadata exceeds size limit")
	}
	return json.Unmarshal(data, target)
}

func syncUpdateDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()
	return dir.Sync()
}
