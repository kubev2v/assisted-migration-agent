package models

import "time"

type ImageUpgradeImage struct {
	Digest  string `json:"digest"`
	Version string `json:"version"`
}

type SuccessfulImageUpgrade struct {
	Digest  string    `json:"digest"`
	Version string    `json:"version"`
	Time    time.Time `json:"time"`
}

type ImageUpgradeResult struct {
	File                 string                  `json:"file"`
	Result               string                  `json:"result"`
	Time                 time.Time               `json:"time"`
	Error                string                  `json:"error"`
	ErrorStage           string                  `json:"error_stage"`
	RunningImage         *ImageUpgradeImage      `json:"running_image"`
	LastSuccessfulUpdate *SuccessfulImageUpgrade `json:"last_successful_update"`
}

type ImageUploadStatus struct {
	State     string  `json:"state"`
	LastError *string `json:"lastError"`
}

type ImageUpgradeStatus struct {
	CurrentVersion string              `json:"currentVersion"`
	Upload         ImageUploadStatus   `json:"upload"`
	Upgrade        *ImageUpgradeResult `json:"upgrade"`
	UpgradeError   *string             `json:"upgradeError"`
}
