// Package upgrade implements the agent side of host-coordinated upgrades.
package upgrade

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"time"
)

const ContractVersion = 1

var attemptPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)
var imagePattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// Options bind a host-managed process to its immutable image and attempt.
type Options struct {
	AttemptID string
	ImageID   string
	Timeout   time.Duration
}

// Authorization is published durably by the host, never by the agent.
// The file is the agent-facing start/release decision, not the migration manifest.
type Authorization struct {
	ContractVersion int    `json:"contract_version"`
	AttemptID       string `json:"attempt_id"`
	Phase           string `json:"phase"`
	PreviousImage   string `json:"previous_image"`
	CandidateImage  string `json:"candidate_image"`
}

func (o Options) Validate() error {
	if o.Timeout < 0 {
		return fmt.Errorf("upgrade timeout must be positive")
	}
	if o.ImageID != "" && !imagePattern.MatchString(o.ImageID) {
		return fmt.Errorf("invalid upgrade image ID")
	}
	if o.AttemptID != "" && (!attemptPattern.MatchString(o.AttemptID) || !imagePattern.MatchString(o.ImageID)) {
		return fmt.Errorf("probation requires a valid attempt ID and immutable image ID")
	}
	return nil
}

// Authorize checks normal startup, probation startup, or release against the host decision.
func Authorize(folder string, opts Options, release bool) error {
	if err := opts.Validate(); err != nil {
		return err
	}
	root, err := os.OpenRoot(folder)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	file, err := root.Open("updates/authorization.json")
	if errors.Is(err, os.ErrNotExist) && opts.AttemptID == "" && opts.ImageID == "" {
		return nil
	}
	if err != nil {
		return fmt.Errorf("host upgrade authorization required: %w", err)
	}
	defer func() { _ = file.Close() }()
	var auth Authorization
	decoder := json.NewDecoder(io.LimitReader(file, 16385))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&auth); err != nil {
		return fmt.Errorf("invalid host authorization: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("trailing host authorization content")
	}
	if auth.ContractVersion != ContractVersion || !attemptPattern.MatchString(auth.AttemptID) || !imagePattern.MatchString(auth.PreviousImage) || !imagePattern.MatchString(auth.CandidateImage) {
		return fmt.Errorf("unsupported host authorization")
	}
	if opts.AttemptID != "" && opts.AttemptID != auth.AttemptID {
		return fmt.Errorf("host authorization belongs to another attempt")
	}
	expected := ""
	switch auth.Phase {
	case "validating":
		if opts.AttemptID != "" && !release {
			expected = auth.CandidateImage
		}
	case "restoring":
		if opts.AttemptID != "" && !release {
			expected = auth.PreviousImage
		}
	case "committed":
		expected = auth.CandidateImage
	case "rolled-back", "aborted":
		expected = auth.PreviousImage
	}
	if expected == "" || opts.ImageID != expected {
		return fmt.Errorf("host phase %q does not authorize this image/start", auth.Phase)
	}
	if opts.AttemptID != "" && !release && auth.Phase != "validating" && auth.Phase != "restoring" {
		return fmt.Errorf("probation start requires validating or restoring authorization")
	}
	return nil
}

// LoadToken creates a local control secret only during ordinary startup.
func LoadToken(folder string, probation bool) (string, error) {
	root, err := os.OpenRoot(folder)
	if err != nil {
		return "", err
	}
	defer func() { _ = root.Close() }()
	if !probation {
		if err := createToken(root); err != nil {
			return "", err
		}
	}
	data, err := root.ReadFile("updates/control-token")
	if err != nil {
		return "", err
	}
	if len(data) != 64 {
		return "", fmt.Errorf("invalid upgrade control token")
	}
	return string(data), nil
}
