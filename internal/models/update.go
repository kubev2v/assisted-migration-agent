package models

import "time"

type PreparedImage struct {
	Version string `json:"version"`
	Digest  string `json:"digest"`
}

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
	CurrentVersion  string         `json:"currentVersion"`
	Connected       bool           `json:"connected"`
	AutoUpdate      *bool          `json:"autoUpdate"`
	UpdateAvailable *bool          `json:"updateAvailable"`
	LatestVersion   *string        `json:"latestVersion"`
	State           string         `json:"state"`
	PreparedImage   *PreparedImage `json:"preparedImage"`
	LastResult      *UpdateResult  `json:"lastResult"`
	LastError       *string        `json:"lastError"`
}
