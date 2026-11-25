package services

import (
	"fmt"
	"regexp"
	"strings"

	"golang.org/x/mod/semver"
)

var (
	productionImageUpgradeVersion = regexp.MustCompile(`^v([0-9]+)\.([0-9]+)\.([0-9]+)$`)
	branchImageUpgradeVersion     = regexp.MustCompile(`^(main|stable)-release-([0-9]+)\.([0-9]+)-([0-9a-f]{7})$`)
)

// Unlike the planner's availability check, upload validation must establish
// compatibility before handing the image to the host updater.
func checkMinimumImageUpgradeVersion(current, minimum string) error {
	if minimum == "" {
		return fmt.Errorf("missing %s label", minimumImageUpgradeLabel)
	}

	minimumChannel, minimumVersion, valid := parseImageUpgradeVersion(minimum)
	if !valid {
		return fmt.Errorf("invalid %s label: %q", minimumImageUpgradeLabel, minimum)
	}

	currentChannel, currentVersion, valid := parseImageUpgradeVersion(current)
	if !valid {
		return fmt.Errorf("invalid running appliance version: %q", current)
	}

	if currentChannel != "production" || minimumChannel != "production" {
		currentVersion = semver.MajorMinor(currentVersion)
		minimumVersion = semver.MajorMinor(minimumVersion)
	}

	if semver.Compare(currentVersion, minimumVersion) < 0 {
		return fmt.Errorf("image requires at least %s; running version is %s", minimum, current)
	}

	return nil
}

func parseImageUpgradeVersion(value string) (channel, version string, valid bool) {
	if value == "v0.0.0" {
		return "", "", false
	}

	parts := productionImageUpgradeVersion.FindStringSubmatch(value)
	if parts != nil {
		channel = "production"
		parts = parts[1:]
	} else {
		parts = branchImageUpgradeVersion.FindStringSubmatch(value)
		if parts == nil {
			return "", "", false
		}

		channel = parts[1]
		parts = []string{parts[2], parts[3], "0"}
	}

	// Match the planner's numeric parsing, including leading zeros, while
	// passing canonical components to semver. Branch SHAs do not affect order.
	for i, part := range parts {
		parts[i] = strings.TrimLeft(part, "0")
		if parts[i] == "" {
			parts[i] = "0"
		}
	}

	return channel, "v" + strings.Join(parts, "."), true
}
