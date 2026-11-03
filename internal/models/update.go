package models

import "time"

type UpdateImage struct {
	Digest  string `json:"digest"`
	Version string `json:"version"`
}

type SuccessfulUpdate struct {
	Digest  string    `json:"digest"`
	Version string    `json:"version"`
	Time    time.Time `json:"time"`
}

type UpdateResult struct {
	File                 string            `json:"file"`
	Result               string            `json:"result"`
	Time                 time.Time         `json:"time"`
	Error                string            `json:"error"`
	ErrorStage           string            `json:"error_stage"`
	RunningImage         *UpdateImage      `json:"running_image"`
	LastSuccessfulUpdate *SuccessfulUpdate `json:"last_successful_update"`
}

type UpdateStatus struct {
	CurrentVersion string        `json:"currentVersion"`
	State          string        `json:"state"`
	LastResult     *UpdateResult `json:"lastResult"`
	LastError      *string       `json:"lastError"`
}
