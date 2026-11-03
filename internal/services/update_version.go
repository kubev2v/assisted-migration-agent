package services

import (
	"fmt"
	"regexp"

	"golang.org/x/mod/semver"
)

var (
	productionUpgradeVersion = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	branchUpgradeVersion     = regexp.MustCompile(`^(main|stable)-release-(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)-([0-9a-f]{7,40})$`)
)

// checkMinimumUpgradeVersion uses the Containerfile's version formats. Dev and
// stable compatibility is defined at release granularity; commit SHAs do not
// affect the comparison.
func checkMinimumUpgradeVersion(current, minimum string) error {
	if minimum == "" {
		return nil // Older images may omit the optional compatibility label.
	}
	if productionUpgradeVersion.MatchString(minimum) {
		if !productionUpgradeVersion.MatchString(current) {
			return fmt.Errorf("cannot compare running version %q with minimum %q: expected a production vX.Y.Z version", current, minimum)
		}
		if semver.Compare(current, minimum) < 0 {
			return fmt.Errorf("image requires at least %s; running version is %s", minimum, current)
		}
		return nil
	}
	boundary := branchUpgradeVersion.FindStringSubmatch(minimum)
	if boundary == nil {
		return fmt.Errorf("invalid min-upgrade-version label: %q", minimum)
	}
	installed := branchUpgradeVersion.FindStringSubmatch(current)
	if installed == nil || installed[1] != boundary[1] {
		return fmt.Errorf("cannot compare running version %q with minimum %q: expected the same release channel", current, minimum)
	}
	// Use semver's numeric comparison rather than lexical ordering (0.10 > 0.9).
	comparison := semver.Compare("v"+installed[2]+"."+installed[3]+".0", "v"+boundary[2]+"."+boundary[3]+".0")
	if comparison < 0 {
		return fmt.Errorf("image requires at least %s; running version is %s", minimum, current)
	}
	return nil
}
