package services

// SetImageUpgradeFreeSpaceForTest replaces the filesystem seam only on this instance.
func SetImageUpgradeFreeSpaceForTest(s *ImageUpgradeService, space func(string) (uint64, error)) {
	s.freeSpace = space
}
