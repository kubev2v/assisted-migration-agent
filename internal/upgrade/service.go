package upgrade

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	"github.com/kubev2v/assisted-migration-agent/pkg/work"
)

// Runtime exposes only lifecycle operations; HTTP admission is owned by Service.
type Runtime interface {
	UpgradeBusy() bool
	StopForUpgrade()
}

// Request is a host-only lifecycle transition, authenticated with the local token.
type Request struct {
	Action         string `json:"action"`
	AttemptID      string `json:"attempt_id"`
	PreviousImage  string `json:"previous_image,omitempty"`
	CandidateImage string `json:"candidate_image,omitempty"`
}

type Status struct {
	ContractVersion int    `json:"contract_version"`
	State           string `json:"state"`
	AttemptID       string `json:"attempt_id"`
	ImageID         string `json:"image_id"`
	Error           string `json:"error,omitempty"`
}

type Baseline struct {
	ContractVersion int                   `json:"contract_version"`
	AttemptID       string                `json:"attempt_id"`
	PreviousImage   string                `json:"previous_image"`
	CandidateImage  string                `json:"candidate_image"`
	Snapshot        store.UpgradeSnapshot `json:"snapshot"`
}

type maintenanceResult struct{ Err error }

// Service serializes lifecycle transitions and closes admission before draining.
type Service struct {
	mu            sync.Mutex
	folder        string
	opts          Options
	runtime       Runtime
	token         string
	status        Status
	inflight      int
	requested     Request
	started       bool
	job           *work.Pool[string, maintenanceResult]
	restart       chan Options
	releaseAction string
	quiesced      bool
	timeout       time.Duration
}

func NewService(folder string, opts Options, runtime Runtime, timeout time.Duration) (*Service, error) {
	if err := Authorize(folder, opts, false); err != nil {
		return nil, err
	}
	token, err := LoadToken(folder, opts.AttemptID != "")
	if err != nil {
		return nil, err
	}
	state := "normal"
	if opts.AttemptID != "" {
		state = "probation"
	}
	attemptID := opts.AttemptID
	if attemptID == "" {
		root, err := os.OpenRoot(folder)
		if err != nil {
			return nil, err
		}
		data, readErr := root.ReadFile("updates/authorization.json")
		_ = root.Close()
		if readErr == nil {
			var auth Authorization
			if err := json.Unmarshal(data, &auth); err != nil {
				return nil, err
			}
			attemptID = auth.AttemptID
		}
	}
	return &Service{folder: folder, opts: opts, runtime: runtime, token: token, status: Status{ContractVersion: ContractVersion, State: state, AttemptID: attemptID, ImageID: opts.ImageID}, restart: make(chan Options, 1), timeout: timeout}, nil
}

func (s *Service) Token() string           { return s.token }
func (s *Service) Restart() <-chan Options { return s.restart }
func (s *Service) Status() Status          { s.mu.Lock(); defer s.mu.Unlock(); return s.status }

// BeginRequest and EndRequest cover the full handler lifetime, including uploads.
func (s *Service) BeginRequest() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status.State != "normal" {
		return false
	}
	s.inflight++
	return true
}
func (s *Service) EndRequest() { s.mu.Lock(); defer s.mu.Unlock(); s.inflight-- }

func (s *Service) Transition(request Request) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !attemptPattern.MatchString(request.AttemptID) {
		return fmt.Errorf("invalid attempt ID")
	}
	if s.status.AttemptID == request.AttemptID && (request.Action == "release" || request.Action == "resume") {
		if s.status.State == "normal" {
			return Authorize(s.folder, Options{AttemptID: request.AttemptID, ImageID: s.opts.ImageID}, true)
		}
		if s.status.State == "releasing" && request.Action == s.releaseAction {
			return nil
		}
	}
	if request.Action == "maintenance" {
		if s.status.State != "normal" {
			if s.status.AttemptID == request.AttemptID && (s.status.State == "draining" || s.status.State == "maintenance") && s.requested == request {
				return nil
			}
			return fmt.Errorf("another lifecycle transition is active")
		}
		if !imagePattern.MatchString(request.PreviousImage) || !imagePattern.MatchString(request.CandidateImage) || (s.opts.ImageID != "" && s.opts.ImageID != request.PreviousImage) {
			return fmt.Errorf("invalid or mismatched image IDs")
		}
		if s.job != nil {
			_ = s.job.Stop()
			s.job = nil
		}
		s.started = false
		s.quiesced = false
		s.requested = request
		s.status.ImageID = request.PreviousImage
		s.status.State, s.status.AttemptID = "draining", request.AttemptID
		s.job = work.NewPool(map[string]work.WorkBuilder[string, maintenanceResult]{"maintenance": s}).WithWorkers(1, 0)
		if err := s.job.Start(); err != nil {
			s.status.State = "failed"
			s.status.Error = "could not start maintenance"
			return err
		}
		return nil
	}
	if request.AttemptID != s.status.AttemptID {
		return fmt.Errorf("attempt does not match active lifecycle")
	}
	opts := s.opts
	if request.Action == "resume" && (s.status.State == "maintenance" || s.status.State == "failed") {
		if s.job != nil && s.job.IsRunning() {
			return fmt.Errorf("maintenance is still running")
		}
		opts = Options{AttemptID: request.AttemptID, ImageID: s.requested.PreviousImage, Timeout: s.opts.Timeout}
	} else if request.Action != "release" || s.status.State != "ready" {
		return fmt.Errorf("invalid lifecycle transition")
	}
	if err := Authorize(s.folder, opts, true); err != nil {
		return err
	}
	opts.AttemptID = ""
	if request.Action == "resume" && !s.quiesced {
		// A drain timeout has not closed stores. Reopen admission without restarting
		// or canceling the work that was already running when maintenance was requested.
		if s.job != nil {
			_ = s.job.Stop()
			s.job = nil
		}
		s.opts = opts
		s.status.State, s.status.Error = "normal", ""
		s.status.ImageID = opts.ImageID
		return nil
	}
	s.status.State = "releasing"
	s.releaseAction = request.Action
	s.restart <- opts
	return nil
}

// ValidateProbation performs all checks before any normal bootstrap is invoked.
func (s *Service) ValidateProbation(ctx context.Context) error {
	if s.opts.AttemptID == "" {
		return fmt.Errorf("probation attempt required")
	}
	root, err := os.OpenRoot(s.folder)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	var baseline Baseline
	var manifest migrations.Manifest
	var auth Authorization
	for name, target := range map[string]any{
		"updates/authorization.json":                                         &auth,
		filepath.Join("updates/validation", s.opts.AttemptID+".json"):        &baseline,
		filepath.Join("updates/attempts", s.opts.AttemptID, "manifest.json"): &manifest,
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
	if auth.PreviousImage != baseline.PreviousImage || auth.CandidateImage != baseline.CandidateImage || auth.AttemptID != s.opts.AttemptID || baseline.ContractVersion != ContractVersion || baseline.AttemptID != s.opts.AttemptID || manifest.Version != 1 || manifest.AttemptID != s.opts.AttemptID || manifest.PreviousImage != baseline.PreviousImage || manifest.CandidateImage != baseline.CandidateImage || (s.opts.ImageID != manifest.PreviousImage && s.opts.ImageID != manifest.CandidateImage) {
		return fmt.Errorf("upgrade baseline identity mismatch")
	}
	snapshot, err := store.ReadUpgradeSnapshot(ctx, s.folder)
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
	if err := Authorize(s.folder, s.opts, false); err != nil {
		return err
	}
	s.mu.Lock()
	s.status.State = "ready"
	s.mu.Unlock()
	return nil
}

func (s *Service) Close() {
	s.mu.Lock()
	job := s.job
	s.mu.Unlock()
	if job != nil {
		_ = job.Stop()
	}
}

func (s *Service) Next() (work.WorkUnit[string, maintenanceResult], bool) {
	if s.started {
		return work.WorkUnit[string, maintenanceResult]{}, false
	}
	s.started = true
	return work.WorkUnit[string, maintenanceResult]{Status: s.workStatus, Work: s.maintain}, true
}
func (s *Service) Finalize(_ context.Context, result maintenanceResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if result.Err != nil {
		s.status.State = "failed"
		s.status.Error = result.Err.Error()
	} else {
		s.status.State = "maintenance"
	}
	return nil
}
func (s *Service) workStatus() string { return "draining" }
func (s *Service) maintain(ctx context.Context, _ maintenanceResult) (maintenanceResult, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		s.mu.Lock()
		pending := s.inflight
		s.mu.Unlock()
		if pending == 0 && !s.runtime.UpgradeBusy() {
			break
		}
		select {
		case <-ctx.Done():
			return maintenanceResult{Err: fmt.Errorf("maintenance deadline exceeded; no migration has run")}, nil
		case <-ticker.C:
		}
	}
	s.runtime.StopForUpgrade()
	s.mu.Lock()
	s.quiesced = true
	s.mu.Unlock()
	snapshot, err := store.ReadUpgradeSnapshot(ctx, s.folder)
	if err == nil {
		root, openErr := os.OpenRoot(s.folder)
		if openErr != nil {
			err = openErr
		} else {
			defer func() { _ = root.Close() }()
			err = root.MkdirAll("updates/validation", 0700)
			if err == nil {
				var file *os.File
				file, err = root.OpenFile(filepath.Join("updates/validation", s.requested.AttemptID+".json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
				if err == nil {
					err = json.NewEncoder(file).Encode(Baseline{ContractVersion: ContractVersion, AttemptID: s.requested.AttemptID, PreviousImage: s.requested.PreviousImage, CandidateImage: s.requested.CandidateImage, Snapshot: snapshot})
					err = errors.Join(err, file.Sync(), file.Close())
					for _, name := range []string{"updates/validation", "updates", "."} {
						dir, openErr := root.Open(name)
						if openErr != nil {
							err = errors.Join(err, openErr)
							break
						}
						err = errors.Join(err, dir.Sync(), dir.Close())
					}
				}
			}
		}
	}
	return maintenanceResult{Err: err}, nil
}

func createToken(root *os.Root) error {
	if err := root.MkdirAll("updates", 0700); err != nil {
		return err
	}
	file, err := root.OpenFile("updates/control-token", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	if err != nil {
		return err
	}
	data := make([]byte, 32)
	if _, err := rand.Read(data); err != nil {
		return errors.Join(err, file.Close())
	}
	_, writeErr := file.WriteString(hex.EncodeToString(data))
	return errors.Join(writeErr, file.Sync(), file.Close())
}
