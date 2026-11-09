package upgrade

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/kubev2v/assisted-migration-agent/internal/store"
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
	ServicesClosed  bool   `json:"services_closed"`
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
	mu        sync.Mutex
	folder    string
	runtime   Runtime
	token     string
	status    Status
	inflight  int
	requested Request
	started   bool
	job       *work.Pool[string, maintenanceResult]
	timeout   time.Duration
}

func NewService(folder string, runtime Runtime, timeout time.Duration) (*Service, error) {
	if runtime == nil || timeout <= 0 {
		return nil, fmt.Errorf("runtime and positive maintenance timeout are required")
	}
	token, err := loadControlToken(folder)
	if err != nil {
		return nil, err
	}
	return &Service{folder: folder, runtime: runtime, token: token, status: Status{ContractVersion: ContractVersion, State: "normal"}, timeout: timeout}, nil
}

func (s *Service) Token() string  { return s.token }
func (s *Service) Status() Status { s.mu.Lock(); defer s.mu.Unlock(); return s.status }

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
	if request.Action == "resume" && s.status.State == "normal" && s.status.AttemptID == request.AttemptID {
		return nil
	}
	if request.Action == "maintenance" {
		if s.status.State != "normal" {
			if s.status.AttemptID == request.AttemptID && (s.status.State == "draining" || s.status.State == "maintenance") && s.requested == request {
				return nil
			}
			return fmt.Errorf("another lifecycle transition is active")
		}
		if !imagePattern.MatchString(request.PreviousImage) || !imagePattern.MatchString(request.CandidateImage) {
			return fmt.Errorf("invalid or mismatched image IDs")
		}
		if s.job != nil {
			_ = s.job.Stop()
			s.job = nil
		}
		s.started = false
		s.status.ServicesClosed = false
		s.requested = request
		s.status.ImageID = request.PreviousImage
		s.status.State, s.status.AttemptID, s.status.Error = "draining", request.AttemptID, ""
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
	if request.Action != "resume" || s.status.State != "failed" || s.status.ServicesClosed {
		return fmt.Errorf("invalid transition; closed services require a host-controlled container restart")
	}
	if s.job != nil && s.job.IsRunning() {
		return fmt.Errorf("maintenance is still running")
	}
	// Failed draining has not closed services or canceled existing work.
	s.status.State, s.status.Error = "normal", ""
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
	s.status.ServicesClosed = true
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
